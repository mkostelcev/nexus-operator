package controller

import (
	"context"
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
	nexusConfigurationFinalizer    = "finalizer.nexus.kostoed.ru"
	nexusConfigurationRequeueDelay = 30 * time.Second
)

// defaultRealms — минимальный набор realm'ов, которые остаются при удалении CR.
var defaultRealms = []string{"NexusAuthenticatingRealm", "NexusAuthorizingRealm"}

type NexusConfigurationReconciler struct {
	client.Client
	ExternalClients
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusconfigurations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nexus.kostoed.ru,resources=nexusconfigurations/finalizers,verbs=update

func (r *NexusConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("NexusConfiguration", req.NamespacedName)
	log.V(1).Info("Начало обработки NexusConfiguration")

	var cr nexusv1alpha1.NexusConfiguration
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Info("Ресурс NexusConfiguration не найден, возможно был удален")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка получения NexusConfiguration: %w", err)
	}

	if !cr.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.finalizeNexusConfiguration(ctx, &cr, log)
	}

	if !utils.ContainsString(cr.Finalizers, nexusConfigurationFinalizer) {
		log.Info("Добавление финализатора")
		cr.Finalizers = append(cr.Finalizers, nexusConfigurationFinalizer)
		if err := r.Update(ctx, &cr); err != nil {
			return ctrl.Result{}, fmt.Errorf("ошибка добавления финализатора: %w", err)
		}
	}

	return r.syncNexusConfiguration(ctx, &cr, log)
}

func (r *NexusConfigurationReconciler) syncNexusConfiguration(
	ctx context.Context,
	cr *nexusv1alpha1.NexusConfiguration,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := r.nexusClient()
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("nexusconfiguration", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("nexusconfiguration").Inc()
		return r.updateStatus(ctx, cr, false, fmt.Errorf("ошибка подключения к Nexus: %w", err))
	}

	// 1. Sync HTTP Proxy
	if cr.Spec.HttpProxy != nil {
		if err := r.syncHttpProxy(ctx, nexusClient, cr, log); err != nil {
			opmetrics.SyncTotal.WithLabelValues("nexusconfiguration", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("nexusconfiguration").Inc()
			return r.updateStatus(ctx, cr, false, err)
		}
	}

	// 2. Sync Security Realms
	if cr.Spec.SecurityRealms != nil {
		if err := r.syncSecurityRealms(ctx, nexusClient, cr, log); err != nil {
			opmetrics.SyncTotal.WithLabelValues("nexusconfiguration", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("nexusconfiguration").Inc()
			return r.updateStatus(ctx, cr, false, err)
		}
	}

	opmetrics.SyncTotal.WithLabelValues("nexusconfiguration", "success").Inc()
	return r.updateStatus(ctx, cr, true, nil)
}

func (r *NexusConfigurationReconciler) syncHttpProxy(
	ctx context.Context,
	nexusClient *nexus.Client,
	cr *nexusv1alpha1.NexusConfiguration,
	log logr.Logger,
) error {
	start := time.Now()
	currentConfig, err := nexusClient.GetHttpProxy(ctx)
	opmetrics.NexusAPIDuration.WithLabelValues("GetHttpProxy").Observe(time.Since(start).Seconds())
	if err != nil {
		return fmt.Errorf("ошибка получения HTTP Proxy конфигурации: %w", err)
	}

	desiredConfig, err := nexus.BuildHttpProxyConfig(cr.Spec.HttpProxy)
	if err != nil {
		return fmt.Errorf("ошибка конфигурации HTTP Proxy: %w", err)
	}

	if nexus.HttpProxyNeedsUpdate(currentConfig, &desiredConfig) {
		start = time.Now()
		if err := nexusClient.UpdateHttpProxy(ctx, desiredConfig); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("UpdateHttpProxy").Observe(time.Since(start).Seconds())
			return fmt.Errorf("ошибка обновления HTTP Proxy конфигурации: %w", err)
		}
		opmetrics.NexusAPIDuration.WithLabelValues("UpdateHttpProxy").Observe(time.Since(start).Seconds())
		log.Info("HTTP Proxy конфигурация успешно обновлена")
	}

	return nil
}

func (r *NexusConfigurationReconciler) syncSecurityRealms(
	ctx context.Context,
	nexusClient *nexus.Client,
	cr *nexusv1alpha1.NexusConfiguration,
	log logr.Logger,
) error {
	start := time.Now()
	currentRealms, err := nexusClient.GetActiveRealms(ctx)
	opmetrics.NexusAPIDuration.WithLabelValues("GetActiveRealms").Observe(time.Since(start).Seconds())
	if err != nil {
		return fmt.Errorf("ошибка получения активных Security Realms: %w", err)
	}

	desiredRealms := cr.Spec.SecurityRealms.Active

	if nexus.SecurityRealmsNeedUpdate(currentRealms, desiredRealms) {
		start = time.Now()
		if err := nexusClient.SetActiveRealms(ctx, desiredRealms); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("SetActiveRealms").Observe(time.Since(start).Seconds())
			return fmt.Errorf("ошибка установки активных Security Realms: %w", err)
		}
		opmetrics.NexusAPIDuration.WithLabelValues("SetActiveRealms").Observe(time.Since(start).Seconds())
		log.Info("Security Realms успешно обновлены", "active", desiredRealms)
	}

	return nil
}

func (r *NexusConfigurationReconciler) finalizeNexusConfiguration(
	ctx context.Context,
	cr *nexusv1alpha1.NexusConfiguration,
	log logr.Logger,
) (ctrl.Result, error) {
	log.Info("Запуск процедуры удаления NexusConfiguration")

	nexusClient, err := r.nexusClient()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка подключения к Nexus: %w", err)
	}

	// Сброс HTTP Proxy
	if cr.Spec.HttpProxy != nil {
		if err := nexusClient.ResetHttpProxy(ctx); err != nil {
			return ctrl.Result{}, fmt.Errorf("ошибка сброса HTTP Proxy: %w", err)
		}
		log.Info("HTTP Proxy конфигурация сброшена")
	}

	// Сброс Security Realms
	if cr.Spec.SecurityRealms != nil {
		currentRealms, err := nexusClient.GetActiveRealms(ctx)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("ошибка получения активных Security Realms: %w", err)
		}

		managedSet := make(map[string]bool, len(cr.Spec.SecurityRealms.Active))
		for _, r := range cr.Spec.SecurityRealms.Active {
			managedSet[r] = true
		}

		var remaining []string
		for _, r := range currentRealms {
			if !managedSet[r] {
				remaining = append(remaining, r)
			}
		}

		if len(remaining) == 0 {
			remaining = make([]string, len(defaultRealms))
			copy(remaining, defaultRealms)
		}

		if err := nexusClient.SetActiveRealms(ctx, remaining); err != nil {
			return ctrl.Result{}, fmt.Errorf("ошибка сброса Security Realms: %w", err)
		}
		log.Info("Security Realms сброшены", "remaining", remaining)
	}

	cr.Finalizers = utils.RemoveString(cr.Finalizers, nexusConfigurationFinalizer)
	opmetrics.DeleteResourceReady("nexusconfiguration", cr.Namespace, cr.Name)
	if err := r.Update(ctx, cr); err != nil {
		return ctrl.Result{}, fmt.Errorf("ошибка удаления финализатора: %w", err)
	}

	log.Info("Финализатор успешно удален")
	return ctrl.Result{}, nil
}

func (r *NexusConfigurationReconciler) updateStatus(
	ctx context.Context,
	cr *nexusv1alpha1.NexusConfiguration,
	ready bool,
	cause error,
) (ctrl.Result, error) {
	opmetrics.SetResourceReady("nexusconfiguration", cr.Namespace, cr.Name, ready)
	newCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: cr.Generation,
	}

	if ready {
		newCondition.Status = metav1.ConditionTrue
		newCondition.Reason = successReason
		newCondition.Message = "NexusConfiguration синхронизирована с Nexus"
	} else {
		newCondition.Status = metav1.ConditionFalse
		newCondition.Reason = errorReason
		newCondition.Message = cause.Error()
	}

	currentCondition := meta.FindStatusCondition(cr.Status.Conditions, "Ready")
	if currentCondition != nil &&
		currentCondition.Status == newCondition.Status &&
		currentCondition.Reason == newCondition.Reason &&
		currentCondition.Message == newCondition.Message &&
		currentCondition.ObservedGeneration == cr.Generation {
		if ready {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: nexusConfigurationRequeueDelay}, nil
	}

	meta.SetStatusCondition(&cr.Status.Conditions, newCondition)

	if ready {
		now := metav1.Now()
		cr.Status.LastSyncTime = &now
		cr.Status.SyncErrors = 0
	} else {
		cr.Status.SyncErrors++
	}

	if err := r.Status().Update(ctx, cr); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка обновления статуса: %w", err)
	}

	if ready {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: nexusConfigurationRequeueDelay}, nil
}

func (r *NexusConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.NexusConfiguration{}).
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)).
		Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}
