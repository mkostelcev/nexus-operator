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
	"github.com/mkostelcev/nexus-operator/pkg/keycloak"
	opmetrics "github.com/mkostelcev/nexus-operator/pkg/metrics"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	roleFinalizer    = "finalizer.nexus.kostoed.ru"
	roleRequeueDelay = 30 * time.Second
)

type RoleReconciler struct {
	client.Client
	ExternalClients
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nexus.kostoed.ru,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nexus.kostoed.ru,resources=roles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nexus.kostoed.ru,resources=roles/finalizers,verbs=update

func (r *RoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("Role", req.NamespacedName)
	log.V(1).Info("Начало обработки роли")

	var roleCR nexusv1alpha1.Role
	if err := r.Get(ctx, req.NamespacedName, &roleCR); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Ресурс роли не найден, возможно был удален")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка получения роли: %w", err)
	}

	if !roleCR.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.finalizeRole(ctx, &roleCR, log)
	}

	if !utils.ContainsString(roleCR.Finalizers, roleFinalizer) {
		log.Info("Добавление финализатора")
		roleCR.Finalizers = append(roleCR.Finalizers, roleFinalizer)
		if err := r.Update(ctx, &roleCR); err != nil {
			return ctrl.Result{}, fmt.Errorf("ошибка добавления финализатора: %w", err)
		}
	}

	return r.syncRole(ctx, &roleCR, log)
}

func (r *RoleReconciler) syncRole(
	ctx context.Context,
	role *nexusv1alpha1.Role,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := r.nexusClient()
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("role", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("role").Inc()
		return r.updateStatus(ctx, role, false, fmt.Errorf("ошибка подключения к Nexus: %w", err))
	}

	desiredRole := nexus.BuildRoleConfig(role.Spec)

	start := time.Now()
	exists, err := nexusClient.RoleExists(ctx, role.Spec.RoleID)
	opmetrics.NexusAPIDuration.WithLabelValues("RoleExists").Observe(time.Since(start).Seconds())
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("role", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("role").Inc()
		return r.updateStatus(ctx, role, false, fmt.Errorf("ошибка проверки существования роли: %w", err))
	}

	if !exists {
		start = time.Now()
		if err := nexusClient.CreateRole(ctx, desiredRole); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("CreateRole").Observe(time.Since(start).Seconds())
			opmetrics.SyncTotal.WithLabelValues("role", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("role").Inc()
			return r.updateStatus(ctx, role, false, fmt.Errorf("ошибка создания роли: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("CreateRole").Observe(time.Since(start).Seconds())
		log.Info("Роль успешно создана", "roleID", role.Spec.RoleID)
		opmetrics.SyncTotal.WithLabelValues("role", "success").Inc()
		return r.updateStatus(ctx, role, true, nil)
	}

	start = time.Now()
	currentRole, err := nexusClient.GetRole(ctx, role.Spec.RoleID)
	opmetrics.NexusAPIDuration.WithLabelValues("GetRole").Observe(time.Since(start).Seconds())
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("role", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("role").Inc()
		return r.updateStatus(ctx, role, false, fmt.Errorf("ошибка получения роли из Nexus: %w", err))
	}

	if diff := r.roleDiff(currentRole, &desiredRole); len(diff) > 0 {
		start = time.Now()
		if err := nexusClient.UpdateRole(ctx, role.Spec.RoleID, desiredRole); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("UpdateRole").Observe(time.Since(start).Seconds())
			opmetrics.SyncTotal.WithLabelValues("role", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("role").Inc()
			return r.updateStatus(ctx, role, false, fmt.Errorf("ошибка обновления роли: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("UpdateRole").Observe(time.Since(start).Seconds())
		log.Info("Роль успешно обновлена", "roleID", role.Spec.RoleID, "changed", diff)
	}

	if role.Spec.KeycloakSync {
		if err := r.syncKeycloak(ctx, role, log); err != nil {
			meta.SetStatusCondition(&role.Status.Conditions, metav1.Condition{
				Type:               "KeycloakSynced",
				Status:             metav1.ConditionFalse,
				Reason:             errorReason,
				Message:            err.Error(),
				ObservedGeneration: role.Generation,
			})
			opmetrics.SyncTotal.WithLabelValues("role", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("role").Inc()
			return r.updateStatus(ctx, role, false, err)
		}
		meta.SetStatusCondition(&role.Status.Conditions, metav1.Condition{
			Type:               "KeycloakSynced",
			Status:             metav1.ConditionTrue,
			Reason:             successReason,
			Message:            fmt.Sprintf("KC роль %q синхронизирована", role.Spec.RoleID),
			ObservedGeneration: role.Generation,
		})
	} else {
		meta.RemoveStatusCondition(&role.Status.Conditions, "KeycloakSynced")
	}

	opmetrics.SyncTotal.WithLabelValues("role", "success").Inc()
	return r.updateStatus(ctx, role, true, nil)
}

const keycloakManagedDesc = "Managed by nexus-operator"

func (r *RoleReconciler) syncKeycloak(
	ctx context.Context,
	role *nexusv1alpha1.Role,
	log logr.Logger,
) error {
	kcClient, err := r.keycloakClient()
	if err != nil {
		log.Info("Keycloak не настроен, пропуск синхронизации")
		return nil //nolint:nilerr // KC опционален, отсутствие клиента — не ошибка
	}

	roleName := role.Spec.RoleID
	if err := kcClient.EnsureClientRole(ctx, roleName, keycloakManagedDesc); err != nil {
		return fmt.Errorf("keycloak ensure role: %w", err)
	}

	if err := kcClient.UpdateClientRole(ctx, roleName, keycloak.Role{
		Name:        roleName,
		Description: keycloakManagedDesc,
	}); err != nil {
		log.Error(err, "Ошибка обновления описания KC роли", "roleId", roleName)
	}

	log.Info("KC роль синхронизирована", "roleId", roleName)
	return nil
}

// roleDiff возвращает список полей роли, отличающихся между Nexus и desired-состоянием.
func (r *RoleReconciler) roleDiff(current, desired *nexus.Role) []string {
	var changed []string
	if current.Name != desired.Name {
		changed = append(changed, "name")
	}
	if current.Description != desired.Description {
		changed = append(changed, "description")
	}
	if !equalStringSlices(current.Privileges, desired.Privileges) {
		changed = append(changed, "privileges")
	}
	if !equalStringSlices(current.Roles, desired.Roles) {
		changed = append(changed, "roles")
	}
	return changed
}

func (r *RoleReconciler) finalizeRole(
	ctx context.Context,
	role *nexusv1alpha1.Role,
	log logr.Logger,
) (ctrl.Result, error) {
	log.Info("Запуск процедуры удаления роли")

	nexusClient, err := r.nexusClient()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка подключения к Nexus: %w", err)
	}

	if err := nexusClient.DeleteRole(ctx, role.Spec.RoleID); err != nil {
		if errors.Is(err, nexus.ErrRoleNotFound) {
			log.Info("Роль уже удалена в Nexus")
		} else {
			return ctrl.Result{}, fmt.Errorf("ошибка удаления роли из Nexus: %w", err)
		}
	}

	if role.Spec.KeycloakSync {
		if kcClient, err := r.keycloakClient(); err == nil {
			if err := kcClient.DeleteClientRole(ctx, role.Spec.RoleID); err != nil {
				log.Error(err, "Ошибка удаления KC роли", "roleId", role.Spec.RoleID)
			} else {
				log.Info("KC роль удалена", "roleId", role.Spec.RoleID)
			}
		}
	}

	role.Finalizers = utils.RemoveString(role.Finalizers, roleFinalizer)
	opmetrics.DeleteResourceReady("role", role.Namespace, role.Name)
	if err := r.Update(ctx, role); err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка удаления финализатора: %w", err)
	}

	log.Info("Финализатор успешно удален")
	return ctrl.Result{}, nil
}

func (r *RoleReconciler) updateStatus(
	ctx context.Context,
	role *nexusv1alpha1.Role,
	ready bool,
	cause error,
) (ctrl.Result, error) {
	opmetrics.SetResourceReady("role", role.Namespace, role.Name, ready)
	newCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: role.Generation,
	}

	if ready {
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = successReason
		newCondition.Message = "Роль синхронизирована с Nexus"
	} else {
		newCondition.Status = metav1.ConditionFalse
		newCondition.Reason = errorReason
		newCondition.Message = cause.Error()
	}

	needsStatusUpdate := false

	currentCondition := meta.FindStatusCondition(role.Status.Conditions, "Ready")
	if currentCondition == nil ||
		currentCondition.Status != newCondition.Status ||
		currentCondition.Reason != newCondition.Reason ||
		currentCondition.Message != newCondition.Message ||
		currentCondition.ObservedGeneration != role.Generation {
		needsStatusUpdate = true
	}

	// KeycloakSynced устанавливается до вызова updateStatus,
	// поэтому всегда сохраняем статус при включённом keycloakSync
	if role.Spec.KeycloakSync {
		needsStatusUpdate = true
	}

	if !needsStatusUpdate {
		if ready {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: roleRequeueDelay}, nil
	}

	meta.SetStatusCondition(&role.Status.Conditions, newCondition)

	if ready {
		now := metav1.Now()
		role.Status.LastSyncTime = &now
		role.Status.SyncErrors = 0
	} else {
		role.Status.SyncErrors++
	}

	if err := r.Status().Update(ctx, role); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка обновления статуса: %w", err)
	}

	if ready {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: roleRequeueDelay}, nil
}

func (r *RoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.Role{}).
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)).
		Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
