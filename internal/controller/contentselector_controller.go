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
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	contentSelectorFinalizer    = "finalizer.nexus.operators.dev.kostoed.ru"
	contentSelectorRequeueDelay = 30 * time.Second
)

type ContentSelectorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=contentselectors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=contentselectors/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nexus.operators.dev.kostoed.ru,resources=contentselectors/finalizers,verbs=update

func (r *ContentSelectorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("contentselector", req.NamespacedName)
	log.Info("Начало обработки Content Selector")

	var cs nexusv1alpha1.ContentSelector
	if err := r.Get(ctx, req.NamespacedName, &cs); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка получения Content Selector: %w", err)
	}

	if !cs.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.finalizeContentSelector(ctx, &cs, log)
	}

	if !utils.ContainsString(cs.Finalizers, contentSelectorFinalizer) {
		cs.Finalizers = append(cs.Finalizers, contentSelectorFinalizer)
		if err := r.Update(ctx, &cs); err != nil {
			return ctrl.Result{}, fmt.Errorf("ошибка при добавлении финализатора: %w", err)
		}
	}

	return r.syncContentSelector(ctx, &cs, log)
}

func (r *ContentSelectorReconciler) syncContentSelector(
	ctx context.Context,
	cs *nexusv1alpha1.ContentSelector,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := nexus.GetClient()
	if err != nil {
		return r.updateStatus(ctx, cs, false, fmt.Errorf("ошибка подключения к Nexus: %w", err))
	}

	exists, err := nexusClient.ContentSelectorExists(ctx, cs.Spec.Name)
	if err != nil {
		return r.updateStatus(ctx, cs, false, fmt.Errorf("ошибка проверки Content Selector: %w", err))
	}

	if !exists {
		err := nexusClient.CreateContentSelector(
			ctx,
			cs.Spec.Name,
			cs.Spec.Description,
			cs.Spec.Expression,
		)
		if err != nil {
			return r.updateStatus(ctx, cs, false, fmt.Errorf("ошибка создания Content Selector: %w", err))
		}
		log.Info("Content Selector успешно создан")
		return r.updateStatus(ctx, cs, true, nil)
	}

	current, err := nexusClient.GetContentSelector(ctx, cs.Spec.Name)
	if err != nil {
		return r.updateStatus(ctx, cs, false, fmt.Errorf("ошибка получения Content Selector: %w", err))
	}

	if r.needsUpdate(current, cs.Spec) {
		err := nexusClient.UpdateContentSelector(
			ctx,
			cs.Spec.Name,
			cs.Spec.Description,
			cs.Spec.Expression,
		)
		if err != nil {
			return r.updateStatus(ctx, cs, false, fmt.Errorf("ошибка обновления Content Selector: %w", err))
		}
		log.Info("Content Selector успешно обновлен")
	}

	return r.updateStatus(ctx, cs, true, nil)
}

func (r *ContentSelectorReconciler) needsUpdate(current *nexus.ContentSelectorResponse, desired nexusv1alpha1.ContentSelectorSpec) bool {
	return current.Description != desired.Description ||
		current.Expression != desired.Expression
}

func (r *ContentSelectorReconciler) finalizeContentSelector(
	ctx context.Context,
	cs *nexusv1alpha1.ContentSelector,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := nexus.GetClient()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка подключения к Nexus: %w", err)
	}

	if err := nexusClient.DeleteContentSelector(ctx, cs.Spec.Name); err != nil {
		if errors.Is(err, nexus.ErrContentSelectorNotFound) {
			log.Info("Content Selector уже удален в Nexus")
		} else {
			return ctrl.Result{}, fmt.Errorf("ошибка удаления Content Selector в Nexus: %w", err)
		}
	}

	cs.Finalizers = utils.RemoveString(cs.Finalizers, contentSelectorFinalizer)
	if err := r.Update(ctx, cs); err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка удаления финализатора: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *ContentSelectorReconciler) updateStatus(
	ctx context.Context,
	cs *nexusv1alpha1.ContentSelector,
	ready bool,
	cause error,
) (ctrl.Result, error) {
	newCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: cs.Generation,
	}

	if ready {
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = successReason
		newCondition.Message = "Content Selector успешно синхронизирован"
	} else {
		newCondition.Status = metav1.ConditionFalse
		newCondition.Reason = errorReason
		newCondition.Message = cause.Error()
	}

	meta.SetStatusCondition(&cs.Status.Conditions, newCondition)
	if err := r.Status().Update(ctx, cs); err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка обновления статуса: %w", err)
	}

	if ready {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: contentSelectorRequeueDelay}, nil
}

func (r *ContentSelectorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.ContentSelector{}).
		Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}
