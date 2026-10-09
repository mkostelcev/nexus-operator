package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	opmetrics "github.com/mkostelcev/nexus-operator/pkg/metrics"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	privilegeFinalizer    = "finalizer.nexus.kostoed.ru"
	privilegeRequeueDelay = 30 * time.Second
)

type PrivilegeReconciler struct {
	client.Client
	ExternalClients
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=privileges,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=privileges/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=privileges/finalizers,verbs=update

func (r *PrivilegeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("privilege", req.NamespacedName)
	log.V(1).Info("Начало обработки привелегии")

	var privilegeCR nexusv1alpha1.Privilege
	if err := r.Get(ctx, req.NamespacedName, &privilegeCR); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка получения привелегии: %w", err)
	}

	if !privilegeCR.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.finalizePrivilege(ctx, &privilegeCR, log)
	}

	if !utils.ContainsString(privilegeCR.Finalizers, privilegeFinalizer) {
		privilegeCR.Finalizers = append(privilegeCR.Finalizers, privilegeFinalizer)
		if err := r.Update(ctx, &privilegeCR); err != nil {
			return ctrl.Result{}, fmt.Errorf("ошибка при добавлении финализатора: %w", err)
		}
	}

	return r.syncPrivilege(ctx, &privilegeCR, log)
}

func (r *PrivilegeReconciler) syncPrivilege(
	ctx context.Context,
	privilege *nexusv1alpha1.Privilege,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := r.nexusClient()
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("privilege", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("privilege").Inc()
		return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка подключения к Nexus: %w", err))
	}

	start := time.Now()
	exists, err := nexusClient.PrivilegeExists(ctx, privilege.Spec.Name)
	opmetrics.NexusAPIDuration.WithLabelValues("PrivilegeExists").Observe(time.Since(start).Seconds())
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("privilege", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("privilege").Inc()
		return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка проверки привелегии: %w", err))
	}

	desiredConfig, err := nexus.BuildPrivilegeConfig(privilege.Spec)
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("privilege", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("privilege").Inc()
		return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка формирования конфигурации: %w", err))
	}

	if !exists {
		start = time.Now()
		if err := nexusClient.CreatePrivilege(ctx, desiredConfig); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("CreatePrivilege").Observe(time.Since(start).Seconds())
			opmetrics.SyncTotal.WithLabelValues("privilege", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("privilege").Inc()
			return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка создания привелегии: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("CreatePrivilege").Observe(time.Since(start).Seconds())
		log.Info("Привелегия успешно создана", "name", privilege.Spec.Name, "type", privilege.Spec.Type)
		opmetrics.SyncTotal.WithLabelValues("privilege", "success").Inc()
		return r.updateStatus(ctx, privilege, true, nil)
	}

	start = time.Now()
	currentConfig, err := nexusClient.GetPrivilege(ctx, privilege.Spec.Name)
	opmetrics.NexusAPIDuration.WithLabelValues("GetPrivilege").Observe(time.Since(start).Seconds())
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("privilege", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("privilege").Inc()
		return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка получения привелегии: %w", err))
	}

	if diff := r.configDiff(currentConfig, desiredConfig); len(diff) > 0 {
		start = time.Now()
		if err := nexusClient.UpdatePrivilege(ctx, privilege.Spec.Name, desiredConfig); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("UpdatePrivilege").Observe(time.Since(start).Seconds())
			opmetrics.SyncTotal.WithLabelValues("privilege", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("privilege").Inc()
			return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка обновления привелегии: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("UpdatePrivilege").Observe(time.Since(start).Seconds())
		log.Info("Привелегия успешно обновлена", "name", privilege.Spec.Name, "changed", diff)
	}

	opmetrics.SyncTotal.WithLabelValues("privilege", "success").Inc()
	return r.updateStatus(ctx, privilege, true, nil)
}

// configDiff возвращает пути расхождений между current и desired конфигурациями
// (без значений — безопасно для логирования). Пустой список = обновление не нужно.
func (r *PrivilegeReconciler) configDiff(current, desired map[string]interface{}) []string {
	ignoreFields := []string{"readOnly", "type", "id"}

	desiredCopy := make(map[string]interface{})
	for k, v := range desired {
		desiredCopy[k] = v
	}
	for _, field := range ignoreFields {
		delete(desiredCopy, field)
	}

	filtered := make(map[string]interface{}, len(desiredCopy))
	for key := range desiredCopy {
		if srcVal, ok := current[key]; ok {
			filtered[key] = srcVal
		}
	}

	return diffPaths(desiredCopy, filtered)
}

func (r *PrivilegeReconciler) finalizePrivilege(
	ctx context.Context,
	privilege *nexusv1alpha1.Privilege,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := r.nexusClient()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка подключения к Nexus: %w", err)
	}

	if err := nexusClient.DeletePrivilege(ctx, privilege.Spec.Name); err != nil {
		if errors.Is(err, nexus.ErrPrivilegeNotFound) {
			log.Info("Привелегия уже удалена в Nexus")
		} else {
			return ctrl.Result{}, fmt.Errorf("ошибка удаления привелегии в Nexus: %w", err)
		}
	}

	privilege.Finalizers = utils.RemoveString(privilege.Finalizers, privilegeFinalizer)
	opmetrics.DeleteResourceReady("privilege", privilege.Namespace, privilege.Name)
	if err := r.Update(ctx, privilege); err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка удаления финализатора: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *PrivilegeReconciler) updateStatus(
	ctx context.Context,
	privilege *nexusv1alpha1.Privilege,
	ready bool,
	cause error,
) (ctrl.Result, error) {
	opmetrics.SetResourceReady("privilege", privilege.Namespace, privilege.Name, ready)
	newCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: privilege.Generation,
	}

	if ready {
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = successReason
		newCondition.Message = "Привелегия успешно синхронизирована"
	} else {
		newCondition.Status = metav1.ConditionFalse
		newCondition.Reason = errorReason
		newCondition.Message = cause.Error()
	}

	currentCondition := meta.FindStatusCondition(privilege.Status.Conditions, "Ready")
	if currentCondition != nil &&
		currentCondition.Status == newCondition.Status &&
		currentCondition.Reason == newCondition.Reason &&
		currentCondition.Message == newCondition.Message &&
		currentCondition.ObservedGeneration == privilege.Generation {
		if ready {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: privilegeRequeueDelay}, nil
	}

	meta.SetStatusCondition(&privilege.Status.Conditions, newCondition)

	if ready {
		now := metav1.Now()
		privilege.Status.LastSyncTime = &now
		privilege.Status.SyncErrors = 0
	} else {
		privilege.Status.SyncErrors++
	}

	if err := r.Status().Update(ctx, privilege); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка обновления статуса: %w", err)
	}

	if ready {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: privilegeRequeueDelay}, nil
}

func (r *PrivilegeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.Privilege{}).
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)).
		Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}
