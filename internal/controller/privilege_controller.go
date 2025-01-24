package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	privilegeFinalizer    = "finalizer.nexus.operators.dev.kostoed.ru"
	privilegeRequeueDelay = 30 * time.Second
)

type PrivilegeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=privileges,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=privileges/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=privileges/finalizers,verbs=update

func (r *PrivilegeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("privilege", req.NamespacedName)
	log.Info("Начало обработки привелегии")

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
	nexusClient, err := nexus.GetClient()
	if err != nil {
		return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка подключения к Nexus: %w", err))
	}

	exists, err := nexusClient.PrivilegeExists(ctx, privilege.Spec.Name)
	if err != nil {
		return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка проверки привелегии: %w", err))
	}

	desiredConfig, err := nexus.BuildPrivilegeConfig(privilege.Spec)
	if err != nil {
		return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка формирования конфигурации: %w", err))
	}

	if !exists {
		if err := nexusClient.CreatePrivilege(ctx, desiredConfig); err != nil {
			return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка создания привелегии: %w", err))
		}
		log.Info("Привелегия успешно создана")
		return r.updateStatus(ctx, privilege, true, nil)
	}

	currentConfig, err := nexusClient.GetPrivilege(ctx, privilege.Spec.Name)
	if err != nil {
		return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка получения привелегии: %w", err))
	}

	if r.needsUpdate(currentConfig, desiredConfig) {
		if err := nexusClient.UpdatePrivilege(ctx, privilege.Spec.Name, desiredConfig); err != nil {
			return r.updateStatus(ctx, privilege, false, fmt.Errorf("ошибка обновления привелегии: %w", err))
		}
		log.Info("Привелегия успешно обновлена")
	}

	return r.updateStatus(ctx, privilege, true, nil)
}

func (r *PrivilegeReconciler) needsUpdate(current, desired map[string]interface{}) bool {
	ignoreFields := []string{"readOnly", "type", "id"}

	currentCopy := make(map[string]interface{})
	for k, v := range current {
		currentCopy[k] = v
	}
	desiredCopy := make(map[string]interface{})
	for k, v := range desired {
		desiredCopy[k] = v
	}

	for _, field := range ignoreFields {
		delete(currentCopy, field)
		delete(desiredCopy, field)
	}
	return !cmp.Equal(currentCopy, desiredCopy)
}

func (r *PrivilegeReconciler) finalizePrivilege(
	ctx context.Context,
	privilege *nexusv1alpha1.Privilege,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := nexus.GetClient()
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

	meta.SetStatusCondition(&privilege.Status.Conditions, newCondition)
	if err := r.Status().Update(ctx, privilege); err != nil {
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
		Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}
