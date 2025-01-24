package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	repositoryFinalizer    = "finalizer.nexus.operators.dev.kostoed.ru"
	repositoryRequeueDelay = 30 * time.Second
)

type RepositoryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=repositories,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=repositories/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=repositories/finalizers,verbs=update

func (r *RepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("repository", req.NamespacedName)
	log.Info("Начало обработки репозитория")

	var repoCR nexusv1alpha1.Repository
	if err := r.Get(ctx, req.NamespacedName, &repoCR); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Ресурс не найден, возможно был удален")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Ошибка получения ресурса")
		return ctrl.Result{}, fmt.Errorf("ошибка получения ресурса: %w", err)
	}

	if !repoCR.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.finalizeRepository(ctx, &repoCR, log)
	}

	if !utils.ContainsString(repoCR.Finalizers, repositoryFinalizer) {
		log.Info("Добавление финализатора", "finalizer", repositoryFinalizer)
		repoCR.Finalizers = append(repoCR.Finalizers, repositoryFinalizer)
		if err := r.Update(ctx, &repoCR); err != nil {
			log.Error(err, "Ошибка добавления финализатора")
			return ctrl.Result{}, fmt.Errorf("не удалось добавить финализатор: %w", err)
		}
	}

	return r.syncRepository(ctx, &repoCR, log)
}

func (r *RepositoryReconciler) syncRepository(
	ctx context.Context,
	repo *nexusv1alpha1.Repository,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := nexus.GetClient()
	if err != nil {
		log.Error(err, "Ошибка создания клиента Nexus")
		return r.updateStatus(ctx, repo, false, fmt.Errorf("не удалось создать клиент Nexus: %w", err))
	}

	currentConfig, err := nexusClient.GetRepository(ctx, repo.Spec.Name)
	exists := true
	if err != nil {
		if errors.Is(err, nexus.ErrRepositoryNotFound) {
			exists = false
		} else {
			log.Error(err, "Ошибка проверки репозитория", "name", repo.Spec.Name)
			return r.updateStatus(ctx, repo, false, fmt.Errorf("ошибка проверки репозитория: %w", err))
		}
	}

	desiredConfig, err := nexus.BuildRepositoryConfig(*repo)
	if err != nil {
		log.Error(err, "Ошибка создания конфигурации")
		return r.updateStatus(ctx, repo, false, fmt.Errorf("ошибка создания конфигурации: %w", err))
	}

	if !exists || r.needsUpdate(desiredConfig, currentConfig) {
		// log.Info("Обнаружены изменения конфигурации", "diff", cmp.Diff(currentConfig, desiredConfig, cmpopts.IgnoreMapEntries(func(k string, v interface{}) bool {
		// 	return k == "lastUpdated" || k == "taskId" || k == "url"
		// })))
		return r.applyConfiguration(ctx, repo, desiredConfig, exists, log)
	}

	log.Info("Конфигурация актуальна")
	return r.updateStatus(ctx, repo, true, nil)
}

// Обновляем сравнение конфигураций
func (r *RepositoryReconciler) needsUpdate(desired, current map[string]interface{}) bool {
	ignoreFields := cmp.Options{
		cmpopts.IgnoreMapEntries(func(k string, v interface{}) bool {
			return k == "lastUpdated" || k == "taskId" || k == "url" || k == "contentDisposition"
		}),
		cmp.FilterPath(func(p cmp.Path) bool {
			// Игнорируем вложенные служебные поля
			return p.String() == "Attributes.checksum" ||
				p.String() == "raw.contentDisposition"
		}, cmp.Ignore()),
	}
	return !cmp.Equal(desired, current, ignoreFields)
}

func (r *RepositoryReconciler) applyConfiguration(
	ctx context.Context,
	repo *nexusv1alpha1.Repository,
	config map[string]interface{},
	exists bool,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := nexus.GetClient()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка получения клиента Nexus: %w", err)
	}

	if exists {
		if err := nexusClient.UpdateRepository(ctx, repo.Spec.Type, repo.Spec.Name, config); err != nil {
			log.Error(err, "Ошибка обновления репозитория")
			return r.updateStatus(ctx, repo, false, fmt.Errorf("ошибка обновления: %w", err))
		}
		log.Info("Репозиторий успешно обновлен")
	} else {
		if err := nexusClient.CreateRepository(ctx, repo.Spec.Type, config); err != nil {
			log.Error(err, "Ошибка создания репозитория")
			return r.updateStatus(ctx, repo, false, fmt.Errorf("ошибка создания: %w", err))
		}
		log.Info("Репозиторий успешно создан")
	}

	return r.updateStatus(ctx, repo, true, nil)
}

func (r *RepositoryReconciler) finalizeRepository(
	ctx context.Context,
	repo *nexusv1alpha1.Repository,
	log logr.Logger,
) (ctrl.Result, error) {
	log.Info("Начало процедуры удаления репозитория")

	if os.Getenv("ENABLE_REPOSITORY_DELETION") == "true" {
		nexusClient, err := nexus.GetClient()
		if err != nil {
			log.Error(err, "Ошибка подключения к Nexus")
			return ctrl.Result{}, fmt.Errorf("ошибка подключения к Nexus: %w", err)
		}

		if err := nexusClient.DeleteRepository(ctx, repo.Spec.Name); err != nil && !errors.Is(err, nexus.ErrRepositoryNotFound) {
			log.Error(err, "Ошибка удаления репозитория", "name", repo.Spec.Name)
			return ctrl.Result{}, fmt.Errorf("ошибка удаления репозитория: %w", err)
		}
	}

	repo.Finalizers = utils.RemoveString(repo.Finalizers, repositoryFinalizer)
	if err := r.Update(ctx, repo); err != nil {
		log.Error(err, "Ошибка удаления финализатора")
		return ctrl.Result{}, fmt.Errorf("ошибка удаления финализатора: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *RepositoryReconciler) updateStatus(
	ctx context.Context,
	repo *nexusv1alpha1.Repository,
	ready bool,
	cause error,
) (ctrl.Result, error) {
	newCondition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "Error",
		Message:            "",
		ObservedGeneration: repo.Generation,
	}

	if ready {
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = "Success"
		newCondition.Message = "Репозиторий успешно синхронизирован"
	} else if cause != nil {
		newCondition.Message = cause.Error()
	}

	currentCondition := meta.FindStatusCondition(repo.Status.Conditions, "Ready")
	if currentCondition != nil &&
		currentCondition.Status == newCondition.Status &&
		currentCondition.Reason == newCondition.Reason &&
		currentCondition.Message == newCondition.Message &&
		currentCondition.ObservedGeneration == repo.Generation {
		// Нет изменений
		if ready {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: repositoryRequeueDelay}, nil
	}

	meta.SetStatusCondition(&repo.Status.Conditions, newCondition)

	if err := r.Status().Update(ctx, repo); err != nil {
		if k8serrors.IsConflict(err) {
			// Конфликт версий, повторная попытка
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка обновления статуса: %w", err)
	}

	if ready {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: repositoryRequeueDelay}, nil
}

func (r *RepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.Repository{}).
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)).
		Complete(r)

	if err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}
