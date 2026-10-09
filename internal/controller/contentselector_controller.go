package controller

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
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
	contentSelectorFinalizer    = "finalizer.nexus.kostoed.ru"
	contentSelectorRequeueDelay = 30 * time.Second
)

var errInvalidCSELExpression = errors.New("невалидный CSEL expression")

// Паттерны для извлечения regex из CSEL expression.
// CSEL синтаксис: path =~ "regex" или path =~ 'regex'
var cselRegexPattern = regexp.MustCompile(`=~\s*["']([^"']+)["']`)

// Неподдерживаемые конструкции Perl regex в PostgreSQL POSIX regex.
var unsupportedPOSIXPatterns = []struct {
	pattern *regexp.Regexp
	name    string
}{
	{regexp.MustCompile(`\(\?[=!<]`), "lookahead/lookbehind (?=, ?!, ?<= или ?<!)"},
	{regexp.MustCompile(`\(\?[imsx]`), "inline флаги (?i, ?m, ?s, ?x)"},
	{regexp.MustCompile(`\(\?:`), "non-capturing group (?:)"},
	{regexp.MustCompile(`\(\?>`), "atomic group (?>)"},
	{regexp.MustCompile(`\(\?#`), "комментарий (?#)"},
	{regexp.MustCompile(`\(\?[+-]`), "conditional (?+, ?-)"},
	{regexp.MustCompile(`[+*?]\+`), "possessive quantifier (++, *+, ?+)"},
}

// validateCSELExpression проверяет CSEL expression на совместимость с PostgreSQL POSIX regex.
func validateCSELExpression(expression string) error {
	// Извлекаем все regex паттерны из выражения
	matches := cselRegexPattern.FindAllStringSubmatch(expression, -1)
	if len(matches) == 0 {
		return nil // Нет regex паттернов
	}

	var errors []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		regex := match[1]

		for _, check := range unsupportedPOSIXPatterns {
			if check.pattern.MatchString(regex) {
				errors = append(errors, fmt.Sprintf(
					"regex '%s' содержит неподдерживаемую конструкцию: %s (PostgreSQL использует POSIX regex, не Perl)",
					regex, check.name,
				))
				break // Одна ошибка на regex достаточно
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("%w: %s", errInvalidCSELExpression, strings.Join(errors, "; "))
	}
	return nil
}

type ContentSelectorReconciler struct {
	client.Client
	ExternalClients
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=contentselectors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=contentselectors/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=contentselectors/finalizers,verbs=update

func (r *ContentSelectorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("contentselector", req.NamespacedName)
	log.V(1).Info("Начало обработки Content Selector")

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
	// Валидация CSEL expression перед отправкой в Nexus
	if err := validateCSELExpression(cs.Spec.Expression); err != nil {
		opmetrics.SyncTotal.WithLabelValues("contentselector", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("contentselector").Inc()
		return r.updateStatus(ctx, cs, false, err)
	}

	nexusClient, err := r.nexusClient()
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("contentselector", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("contentselector").Inc()
		return r.updateStatus(ctx, cs, false, fmt.Errorf("ошибка подключения к Nexus: %w", err))
	}

	start := time.Now()
	exists, err := nexusClient.ContentSelectorExists(ctx, cs.Spec.Name)
	opmetrics.NexusAPIDuration.WithLabelValues("ContentSelectorExists").Observe(time.Since(start).Seconds())
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("contentselector", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("contentselector").Inc()
		return r.updateStatus(ctx, cs, false, fmt.Errorf("ошибка проверки Content Selector: %w", err))
	}

	if !exists {
		start = time.Now()
		err := nexusClient.CreateContentSelector(
			ctx,
			cs.Spec.Name,
			cs.Spec.Description,
			cs.Spec.Expression,
		)
		opmetrics.NexusAPIDuration.WithLabelValues("CreateContentSelector").Observe(time.Since(start).Seconds())
		if err != nil {
			opmetrics.SyncTotal.WithLabelValues("contentselector", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("contentselector").Inc()
			return r.updateStatus(ctx, cs, false, fmt.Errorf("ошибка создания Content Selector: %w", err))
		}
		log.Info("Content Selector успешно создан", "name", cs.Spec.Name)
		opmetrics.SyncTotal.WithLabelValues("contentselector", "success").Inc()
		return r.updateStatus(ctx, cs, true, nil)
	}

	start = time.Now()
	current, err := nexusClient.GetContentSelector(ctx, cs.Spec.Name)
	opmetrics.NexusAPIDuration.WithLabelValues("GetContentSelector").Observe(time.Since(start).Seconds())
	if err != nil {
		opmetrics.SyncTotal.WithLabelValues("contentselector", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("contentselector").Inc()
		return r.updateStatus(ctx, cs, false, fmt.Errorf("ошибка получения Content Selector: %w", err))
	}

	if diff := r.selectorDiff(current, cs.Spec); len(diff) > 0 {
		start = time.Now()
		err := nexusClient.UpdateContentSelector(
			ctx,
			cs.Spec.Name,
			cs.Spec.Description,
			cs.Spec.Expression,
		)
		opmetrics.NexusAPIDuration.WithLabelValues("UpdateContentSelector").Observe(time.Since(start).Seconds())
		if err != nil {
			opmetrics.SyncTotal.WithLabelValues("contentselector", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("contentselector").Inc()
			return r.updateStatus(ctx, cs, false, fmt.Errorf("ошибка обновления Content Selector: %w", err))
		}
		log.Info("Content Selector успешно обновлен", "name", cs.Spec.Name, "changed", diff)
	}

	opmetrics.SyncTotal.WithLabelValues("contentselector", "success").Inc()
	return r.updateStatus(ctx, cs, true, nil)
}

// selectorDiff возвращает список полей Content Selector, отличающихся между Nexus и desired-состоянием.
func (r *ContentSelectorReconciler) selectorDiff(current *nexus.ContentSelectorResponse, desired nexusv1alpha1.ContentSelectorSpec) []string {
	var changed []string
	if current.Description != desired.Description {
		changed = append(changed, "description")
	}
	if current.Expression != desired.Expression {
		changed = append(changed, "expression")
	}
	return changed
}

func (r *ContentSelectorReconciler) finalizeContentSelector(
	ctx context.Context,
	cs *nexusv1alpha1.ContentSelector,
	log logr.Logger,
) (ctrl.Result, error) {
	nexusClient, err := r.nexusClient()
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
	opmetrics.DeleteResourceReady("contentselector", cs.Namespace, cs.Name)
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
	opmetrics.SetResourceReady("contentselector", cs.Namespace, cs.Name, ready)
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

	currentCondition := meta.FindStatusCondition(cs.Status.Conditions, "Ready")
	if currentCondition != nil &&
		currentCondition.Status == newCondition.Status &&
		currentCondition.Reason == newCondition.Reason &&
		currentCondition.Message == newCondition.Message &&
		currentCondition.ObservedGeneration == cs.Generation {
		if ready {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: contentSelectorRequeueDelay}, nil
	}

	meta.SetStatusCondition(&cs.Status.Conditions, newCondition)

	if ready {
		now := metav1.Now()
		cs.Status.LastSyncTime = &now
		cs.Status.SyncErrors = 0
	} else {
		cs.Status.SyncErrors++
	}

	if err := r.Status().Update(ctx, cs); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
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
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)).
		Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}
