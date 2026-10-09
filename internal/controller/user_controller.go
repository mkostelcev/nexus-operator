package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	opmetrics "github.com/mkostelcev/nexus-operator/pkg/metrics"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	userFinalizer    = "finalizer.nexus.kostoed.ru"
	userRequeueDelay = 30 * time.Second
	userResyncDelay  = 5 * time.Minute
)

type NexusUserReconciler struct {
	client.Client
	ExternalClients
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexususers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexususers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexususers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *NexusUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("NexusUser", req.NamespacedName)
	log.V(1).Info("Начало обработки пользователя")

	var userCR nexusv1alpha1.NexusUser
	if err := r.Get(ctx, req.NamespacedName, &userCR); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Ресурс пользователя не найден, возможно был удалён")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка получения пользователя: %w", err)
	}

	if !userCR.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.finalizeUser(ctx, &userCR, log)
	}

	if !utils.ContainsString(userCR.Finalizers, userFinalizer) {
		log.Info("Добавление финализатора")
		userCR.Finalizers = append(userCR.Finalizers, userFinalizer)
		if err := r.Update(ctx, &userCR); err != nil {
			return ctrl.Result{}, fmt.Errorf("ошибка добавления финализатора: %w", err)
		}
	}

	return r.syncUser(ctx, &userCR, log)
}

func (r *NexusUserReconciler) syncUser(
	ctx context.Context,
	userCR *nexusv1alpha1.NexusUser,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := r.nexusClient()
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("user", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("user").Inc()
		return r.updateStatus(ctx, userCR, false, false, fmt.Errorf("ошибка подключения к Nexus: %w", err))
	}

	// Читаем пароль из Secret
	password, err := r.readPasswordFromSecret(ctx, userCR)
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("user", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("user").Inc()
		return r.updateStatus(ctx, userCR, false, false, fmt.Errorf("ошибка чтения пароля из Secret: %w", err))
	}

	start := time.Now()
	exists, err := nexusClient.UserExists(ctx, userCR.Spec.UserId)
	opmetrics.NexusAPIDuration.WithLabelValues("UserExists").Observe(time.Since(start).Seconds())
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("user", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("user").Inc()
		return r.updateStatus(ctx, userCR, false, false, fmt.Errorf("ошибка проверки существования пользователя: %w", err))
	}

	if !exists {
		createReq := nexus.UserCreateRequest{
			UserId:       userCR.Spec.UserId,
			FirstName:    userCR.Spec.FirstName,
			LastName:     userCR.Spec.LastName,
			EmailAddress: userCR.Spec.EmailAddress,
			Password:     password,
			Status:       userCR.Spec.Status,
			Roles:        userCR.Spec.Roles,
		}

		start = time.Now()
		if err := nexusClient.CreateUser(ctx, createReq); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("CreateUser").Observe(time.Since(start).Seconds())
			opmetrics.SyncTotal.WithLabelValues("user", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("user").Inc()
			return r.updateStatus(ctx, userCR, false, false, fmt.Errorf("ошибка создания пользователя: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("CreateUser").Observe(time.Since(start).Seconds())
		log.Info("Пользователь успешно создан", "userId", userCR.Spec.UserId)
		opmetrics.SyncTotal.WithLabelValues("user", "success").Inc()
		return r.updateStatus(ctx, userCR, true, true, nil)
	}

	// Пользователь существует — проверяем атрибуты
	start = time.Now()
	currentUser, err := nexusClient.GetUser(ctx, userCR.Spec.UserId)
	opmetrics.NexusAPIDuration.WithLabelValues("GetUser").Observe(time.Since(start).Seconds())
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("user", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("user").Inc()
		return r.updateStatus(ctx, userCR, false, false, fmt.Errorf("ошибка получения пользователя из Nexus: %w", err))
	}

	if diff := r.userDiff(currentUser, userCR); len(diff) > 0 {
		updateReq := nexus.UserUpdateRequest{
			UserId:       userCR.Spec.UserId,
			FirstName:    userCR.Spec.FirstName,
			LastName:     userCR.Spec.LastName,
			EmailAddress: userCR.Spec.EmailAddress,
			Status:       userCR.Spec.Status,
			Roles:        userCR.Spec.Roles,
		}

		start = time.Now()
		if err := nexusClient.UpdateUser(ctx, userCR.Spec.UserId, updateReq); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("UpdateUser").Observe(time.Since(start).Seconds())
			opmetrics.SyncTotal.WithLabelValues("user", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("user").Inc()
			return r.updateStatus(ctx, userCR, false, false, fmt.Errorf("ошибка обновления пользователя: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("UpdateUser").Observe(time.Since(start).Seconds())
		log.Info("Пользователь успешно обновлён", "userId", userCR.Spec.UserId, "changed", diff)
	}

	// Проверяем пароль
	passwordSynced := true
	start = time.Now()
	authOk := nexusClient.CheckUserAuth(ctx, userCR.Spec.UserId, password)
	opmetrics.NexusAPIDuration.WithLabelValues("CheckUserAuth").Observe(time.Since(start).Seconds())

	if !authOk {
		start = time.Now()
		if err := nexusClient.ChangeUserPassword(ctx, userCR.Spec.UserId, password); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("ChangeUserPassword").Observe(time.Since(start).Seconds())
			opmetrics.SyncTotal.WithLabelValues("user", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("user").Inc()
			return r.updateStatus(ctx, userCR, false, false, fmt.Errorf("ошибка смены пароля: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("ChangeUserPassword").Observe(time.Since(start).Seconds())
		log.Info("Пароль пользователя обновлён", "userId", userCR.Spec.UserId)
		passwordSynced = true
	}

	opmetrics.SyncTotal.WithLabelValues("user", "success").Inc()
	return r.updateStatus(ctx, userCR, true, passwordSynced, nil)
}

// userDiff возвращает список полей пользователя, отличающихся между Nexus и desired-состоянием.
func (r *NexusUserReconciler) userDiff(current *nexus.User, desired *nexusv1alpha1.NexusUser) []string {
	var changed []string
	if current.FirstName != desired.Spec.FirstName {
		changed = append(changed, "firstName")
	}
	if current.LastName != desired.Spec.LastName {
		changed = append(changed, "lastName")
	}
	if current.EmailAddress != desired.Spec.EmailAddress {
		changed = append(changed, "emailAddress")
	}
	if current.Status != desired.Spec.Status {
		changed = append(changed, "status")
	}
	if !equalStringSlices(current.Roles, desired.Spec.Roles) {
		changed = append(changed, "roles")
	}
	return changed
}

func (r *NexusUserReconciler) readPasswordFromSecret(
	ctx context.Context,
	userCR *nexusv1alpha1.NexusUser,
) (string, error) {
	secretRef := userCR.Spec.Credentials.SecretKeyRef
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Name:      secretRef.Name,
		Namespace: userCR.Namespace,
	}, &secret); err != nil {
		return "", fmt.Errorf("не удалось получить Secret %s: %w", secretRef.Name, err)
	}

	data, ok := secret.Data[secretRef.Key]
	if !ok {
		return "", fmt.Errorf("%w: %s в %s", errSecretKeyNotFound, secretRef.Key, secretRef.Name)
	}

	password := string(data)
	if password == "" {
		return "", fmt.Errorf("%w: ключ %s в %s", errCredentialsEmpty, secretRef.Key, secretRef.Name)
	}

	return password, nil
}

func (r *NexusUserReconciler) finalizeUser(
	ctx context.Context,
	userCR *nexusv1alpha1.NexusUser,
	log logr.Logger,
) (ctrl.Result, error) {
	log.Info("Запуск процедуры удаления пользователя")

	nexusClient, err := r.nexusClient()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка подключения к Nexus: %w", err)
	}

	if err := nexusClient.DeleteUser(ctx, userCR.Spec.UserId); err != nil {
		if errors.Is(err, nexus.ErrUserNotFound) {
			log.Info("Пользователь уже удалён в Nexus")
		} else {
			return ctrl.Result{}, fmt.Errorf("ошибка удаления пользователя из Nexus: %w", err)
		}
	}

	userCR.Finalizers = utils.RemoveString(userCR.Finalizers, userFinalizer)
	opmetrics.DeleteResourceReady("user", userCR.Namespace, userCR.Name)
	if err := r.Update(ctx, userCR); err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка удаления финализатора: %w", err)
	}

	log.Info("Финализатор успешно удалён")
	return ctrl.Result{}, nil
}

func (r *NexusUserReconciler) updateStatus(
	ctx context.Context,
	userCR *nexusv1alpha1.NexusUser,
	ready bool,
	passwordSynced bool,
	cause error,
) (ctrl.Result, error) {
	opmetrics.SetResourceReady("user", userCR.Namespace, userCR.Name, ready)
	newCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: userCR.Generation,
	}

	if ready {
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = successReason
		newCondition.Message = "Пользователь синхронизирован с Nexus"
	} else {
		newCondition.Status = metav1.ConditionFalse
		newCondition.Reason = errorReason
		newCondition.Message = cause.Error()
	}

	currentCondition := meta.FindStatusCondition(userCR.Status.Conditions, "Ready")
	if currentCondition != nil &&
		currentCondition.Status == newCondition.Status &&
		currentCondition.Reason == newCondition.Reason &&
		currentCondition.Message == newCondition.Message &&
		currentCondition.ObservedGeneration == userCR.Generation &&
		userCR.Status.CredentialsSynced == passwordSynced {
		if ready {
			return ctrl.Result{RequeueAfter: userResyncDelay}, nil
		}
		return ctrl.Result{RequeueAfter: userRequeueDelay}, nil
	}

	meta.SetStatusCondition(&userCR.Status.Conditions, newCondition)

	if ready {
		now := metav1.Now()
		userCR.Status.LastSyncTime = &now
		userCR.Status.SyncErrors = 0
		userCR.Status.CredentialsSynced = passwordSynced
	} else {
		userCR.Status.SyncErrors++
		userCR.Status.CredentialsSynced = false
	}

	if err := r.Status().Update(ctx, userCR); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка обновления статуса: %w", err)
	}

	if ready {
		return ctrl.Result{RequeueAfter: userResyncDelay}, nil
	}
	return ctrl.Result{RequeueAfter: userRequeueDelay}, nil
}

func (r *NexusUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.NexusUser{}).
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)).
		Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}
