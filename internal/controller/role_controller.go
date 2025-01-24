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

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
)

const (
	roleFinalizer    = "finalizer.nexus.operators.dev.kostoed.ru"
	roleRequeueDelay = 30 * time.Second
)

type RoleReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=roles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=roles/finalizers,verbs=update

func (r *RoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("Role", req.NamespacedName)
	log.Info("Начало обработки роли")

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

	if !containsString(roleCR.Finalizers, roleFinalizer) {
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
	nexusClient, err := nexus.GetClient()
	if err != nil {
		return r.updateStatus(ctx, role, false, fmt.Errorf("ошибка подключения к Nexus: %w", err))
	}

	desiredRole := nexus.BuildRoleConfig(role.Spec)

	exists, err := nexusClient.RoleExists(ctx, role.Spec.RoleID)
	if err != nil {
		return r.updateStatus(ctx, role, false, fmt.Errorf("ошибка проверки существования роли: %w", err))
	}

	if !exists {
		if err := nexusClient.CreateRole(ctx, desiredRole); err != nil {
			return r.updateStatus(ctx, role, false, fmt.Errorf("ошибка создания роли: %w", err))
		}
		log.Info("Роль успешно создана", "roleID", role.Spec.RoleID)
		return r.updateStatus(ctx, role, true, nil)
	}

	currentRole, err := nexusClient.GetRole(ctx, role.Spec.RoleID)
	if err != nil {
		return r.updateStatus(ctx, role, false, fmt.Errorf("ошибка получения роли из Nexus: %w", err))
	}

	if r.needsUpdate(currentRole, &desiredRole) {
		if err := nexusClient.UpdateRole(ctx, role.Spec.RoleID, desiredRole); err != nil {
			return r.updateStatus(ctx, role, false, fmt.Errorf("ошибка обновления роли: %w", err))
		}
		log.Info("Роль успешно обновлена", "roleID", role.Spec.RoleID)
	}

	return r.updateStatus(ctx, role, true, nil)
}

func (r *RoleReconciler) needsUpdate(current, desired *nexus.Role) bool {
	// Сравниваем основные параметры роли
	return current.Name != desired.Name ||
		current.Description != desired.Description ||
		!equalStringSlices(current.Privileges, desired.Privileges) ||
		!equalStringSlices(current.Roles, desired.Roles)
}

func (r *RoleReconciler) finalizeRole(
	ctx context.Context,
	role *nexusv1alpha1.Role,
	log logr.Logger,
) (ctrl.Result, error) {
	log.Info("Запуск процедуры удаления роли")

	nexusClient, err := nexus.GetClient()
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

	role.Finalizers = removeString(role.Finalizers, roleFinalizer)
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
	newCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: role.Generation,
	}

	if ready {
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = "Success"
		newCondition.Message = "Роль синхронизирована с Nexus"
	} else {
		newCondition.Status = metav1.ConditionFalse
		newCondition.Reason = "Error"
		newCondition.Message = cause.Error()
	}

	meta.SetStatusCondition(&role.Status.Conditions, newCondition)
	if err := r.Status().Update(ctx, role); err != nil {
		return ctrl.Result{Requeue: true}, fmt.Errorf("ошибка обновления статуса: %w", err)
	}

	if ready {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: roleRequeueDelay}, nil
}

func (r *RoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.Role{}).
		Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}

// Вспомогательные функции
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) []string {
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if item != s {
			result = append(result, item)
		}
	}
	return result
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
