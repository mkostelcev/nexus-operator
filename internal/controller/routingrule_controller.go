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
	routingRuleFinalizer    = "finalizer.nexus.kostoed.ru"
	routingRuleRequeueDelay = 30 * time.Second
)

type RoutingRuleReconciler struct {
	client.Client
	ExternalClients
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=routingrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=routingrules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=routingrules/finalizers,verbs=update

func (r *RoutingRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("routingrule", req.NamespacedName)
	log.V(1).Info("Начало обработки Routing Rule")

	var rr nexusv1alpha1.RoutingRule
	if err := r.Get(ctx, req.NamespacedName, &rr); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка получения Routing Rule: %w", err)
	}

	if !rr.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.finalizeRoutingRule(ctx, &rr, log)
	}

	if !utils.ContainsString(rr.Finalizers, routingRuleFinalizer) {
		rr.Finalizers = append(rr.Finalizers, routingRuleFinalizer)
		if err := r.Update(ctx, &rr); err != nil {
			return ctrl.Result{}, fmt.Errorf("ошибка при добавлении финализатора: %w", err)
		}
	}

	return r.syncRoutingRule(ctx, &rr, log)
}

func (r *RoutingRuleReconciler) syncRoutingRule(
	ctx context.Context,
	rr *nexusv1alpha1.RoutingRule,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := r.nexusClient()
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("routingrule", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("routingrule").Inc()
		return r.updateStatus(ctx, rr, false, fmt.Errorf("ошибка подключения к Nexus: %w", err))
	}

	start := time.Now()
	exists, err := nexusClient.RoutingRuleExists(ctx, rr.Spec.Name)
	opmetrics.NexusAPIDuration.WithLabelValues("RoutingRuleExists").Observe(time.Since(start).Seconds())
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("routingrule", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("routingrule").Inc()
		return r.updateStatus(ctx, rr, false, fmt.Errorf("ошибка проверки Routing Rule: %w", err))
	}

	desired := nexus.BuildRoutingRuleConfig(rr.Spec)

	if !exists {
		start = time.Now()
		if err := nexusClient.CreateRoutingRule(ctx, desired); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("CreateRoutingRule").Observe(time.Since(start).Seconds())
			opmetrics.SyncTotal.WithLabelValues("routingrule", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("routingrule").Inc()
			return r.updateStatus(ctx, rr, false, fmt.Errorf("ошибка создания Routing Rule: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("CreateRoutingRule").Observe(time.Since(start).Seconds())
		log.Info("Routing Rule успешно создан", "name", rr.Spec.Name)
		opmetrics.SyncTotal.WithLabelValues("routingrule", "success").Inc()
		return r.updateStatus(ctx, rr, true, nil)
	}

	start = time.Now()
	current, err := nexusClient.GetRoutingRule(ctx, rr.Spec.Name)
	opmetrics.NexusAPIDuration.WithLabelValues("GetRoutingRule").Observe(time.Since(start).Seconds())
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("routingrule", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("routingrule").Inc()
		return r.updateStatus(ctx, rr, false, fmt.Errorf("ошибка получения Routing Rule: %w", err))
	}

	if diff := r.ruleDiff(current, &desired); len(diff) > 0 {
		start = time.Now()
		if err := nexusClient.UpdateRoutingRule(ctx, rr.Spec.Name, desired); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("UpdateRoutingRule").Observe(time.Since(start).Seconds())
			opmetrics.SyncTotal.WithLabelValues("routingrule", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("routingrule").Inc()
			return r.updateStatus(ctx, rr, false, fmt.Errorf("ошибка обновления Routing Rule: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("UpdateRoutingRule").Observe(time.Since(start).Seconds())
		log.Info("Routing Rule успешно обновлён", "name", rr.Spec.Name, "changed", diff)
	}

	opmetrics.SyncTotal.WithLabelValues("routingrule", "success").Inc()
	return r.updateStatus(ctx, rr, true, nil)
}

// ruleDiff возвращает список полей Routing Rule, отличающихся между Nexus и desired-состоянием.
func (r *RoutingRuleReconciler) ruleDiff(current *nexus.RoutingRuleXO, desired *nexus.RoutingRuleXO) []string {
	var changed []string
	if current.Description != desired.Description {
		changed = append(changed, "description")
	}
	if current.Mode != desired.Mode {
		changed = append(changed, "mode")
	}
	if !equalStringSlices(current.Matchers, desired.Matchers) {
		changed = append(changed, "matchers")
	}
	return changed
}

func (r *RoutingRuleReconciler) finalizeRoutingRule(
	ctx context.Context,
	rr *nexusv1alpha1.RoutingRule,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := r.nexusClient()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка подключения к Nexus: %w", err)
	}

	if err := nexusClient.DeleteRoutingRule(ctx, rr.Spec.Name); err != nil {
		if errors.Is(err, nexus.ErrRoutingRuleNotFound) {
			log.Info("Routing Rule уже удалён в Nexus")
		} else {
			return ctrl.Result{}, fmt.Errorf("ошибка удаления Routing Rule в Nexus: %w", err)
		}
	}

	rr.Finalizers = utils.RemoveString(rr.Finalizers, routingRuleFinalizer)
	opmetrics.DeleteResourceReady("routingrule", rr.Namespace, rr.Name)
	if err := r.Update(ctx, rr); err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка удаления финализатора: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *RoutingRuleReconciler) updateStatus(
	ctx context.Context,
	rr *nexusv1alpha1.RoutingRule,
	ready bool,
	cause error,
) (ctrl.Result, error) {
	opmetrics.SetResourceReady("routingrule", rr.Namespace, rr.Name, ready)
	newCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: rr.Generation,
	}

	if ready {
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = successReason
		newCondition.Message = "Routing Rule успешно синхронизирован"
	} else {
		newCondition.Status = metav1.ConditionFalse
		newCondition.Reason = errorReason
		newCondition.Message = cause.Error()
	}

	currentCondition := meta.FindStatusCondition(rr.Status.Conditions, "Ready")
	if currentCondition != nil &&
		currentCondition.Status == newCondition.Status &&
		currentCondition.Reason == newCondition.Reason &&
		currentCondition.Message == newCondition.Message &&
		currentCondition.ObservedGeneration == rr.Generation {
		if ready {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: routingRuleRequeueDelay}, nil
	}

	meta.SetStatusCondition(&rr.Status.Conditions, newCondition)

	if ready {
		now := metav1.Now()
		rr.Status.LastSyncTime = &now
		rr.Status.SyncErrors = 0
	} else {
		rr.Status.SyncErrors++
	}

	if err := r.Status().Update(ctx, rr); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка обновления статуса: %w", err)
	}

	if ready {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: routingRuleRequeueDelay}, nil
}

func (r *RoutingRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.RoutingRule{}).
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)).
		Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}
