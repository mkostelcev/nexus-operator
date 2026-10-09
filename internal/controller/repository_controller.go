package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	opmetrics "github.com/mkostelcev/nexus-operator/pkg/metrics"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	repositoryFinalizer    = "finalizer.nexus.kostoed.ru"
	repositoryRequeueDelay = 30 * time.Second
	annotationNeedsAuth    = "nexus.kostoed.ru/needs-auth-config"
)

type RepositoryReconciler struct {
	client.Client
	ExternalClients
	Scheme             *runtime.Scheme
	Log                logr.Logger
	ESOSecretStore     string
	ESOVaultPath       string
	ESOTargetSecret    string
	ESORefreshInterval string
}

//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=repositories,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=repositories/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nexus.kostoed.ru,resources=repositories/finalizers,verbs=update
//+kubebuilder:rbac:groups=external-secrets.io,resources=externalsecrets,verbs=get;list;create;update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *RepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("repository", req.NamespacedName)
	log.V(1).Info("Начало обработки репозитория")

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
	nexusClient, err := r.nexusClient()
	if err != nil {
		log.Error(err, "Ошибка создания клиента Nexus")
		opmetrics.SyncTotal.WithLabelValues("repository", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("repository").Inc()
		return r.updateStatus(ctx, repo, false, fmt.Errorf("не удалось создать клиент Nexus: %w", err))
	}

	// Получаем полную конфигурацию через format-specific endpoint.
	// Generic /repositories/{name} не возвращает httpClient и другие секции.
	var currentConfig map[string]interface{}
	exists := true
	parts := strings.SplitN(repo.Spec.Type, "-", 2)
	start := time.Now()
	if len(parts) == 2 {
		currentConfig, err = nexusClient.GetRepositoryByType(ctx, parts[0], parts[1], repo.Spec.Name)
	} else {
		currentConfig, err = nexusClient.GetRepository(ctx, repo.Spec.Name)
	}
	opmetrics.NexusAPIDuration.WithLabelValues("GetRepository").Observe(time.Since(start).Seconds())
	if err != nil {
		if errors.Is(err, nexus.ErrRepositoryNotFound) {
			exists = false
		} else {
			log.Error(err, "Ошибка проверки репозитория", "name", repo.Spec.Name)
			opmetrics.SyncTotal.WithLabelValues("repository", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("repository").Inc()
			return r.updateStatus(ctx, repo, false, fmt.Errorf("ошибка проверки репозитория: %w", err))
		}
	}

	// Если в Nexus уже настроена аутентификация — снимаем аннотацию needs-auth-config
	if exists && repo.Annotations[annotationNeedsAuth] == "true" {
		if nexus.ConfigHasAuthentication(currentConfig) {
			delete(repo.Annotations, annotationNeedsAuth)
			if updateErr := r.Update(ctx, repo); updateErr != nil {
				log.Error(updateErr, "Ошибка удаления аннотации needs-auth-config")
			} else {
				log.Info("Аннотация needs-auth-config снята, аутентификация настроена в Nexus")
			}
		}
	}

	// Nexus API сбрасывает authentication при PUT без пароля, а пароль из API не получить.
	// Если в Nexus настроена auth, а в CR её нет — пропускаем обновление, чтобы не затереть пароль.
	if exists && nexus.ConfigHasAuthentication(currentConfig) {
		hasAuthInCR := repo.Spec.HttpClient != nil && repo.Spec.HttpClient.Authentication != nil
		if !hasAuthInCR {
			log.Info("Пропуск обновления: в Nexus настроена аутентификация, отсутствующая в CR. "+
				"Заполните spec.httpClient.authentication для управления этим репозиторием",
				"name", repo.Spec.Name)
			opmetrics.SyncTotal.WithLabelValues("repository", "success").Inc()
			return r.updateStatusAuthPending(ctx, repo)
		}
	}

	// Резолвим credentials из Secret, если указан secretRef
	if err := r.resolveAuthFromSecret(ctx, repo); err != nil {
		log.Error(err, "Ошибка чтения credentials из Secret")
		opmetrics.SyncTotal.WithLabelValues("repository", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("repository").Inc()
		return r.updateStatus(ctx, repo, false, fmt.Errorf("ошибка чтения credentials: %w", err))
	}

	desiredConfig, err := nexus.BuildRepositoryConfig(*repo)
	if err != nil {
		log.Error(err, "Ошибка создания конфигурации")
		opmetrics.SyncTotal.WithLabelValues("repository", "error").Inc()
		opmetrics.SyncErrorsTotal.WithLabelValues("repository").Inc()
		return r.updateStatus(ctx, repo, false, fmt.Errorf("ошибка создания конфигурации: %w", err))
	}

	if !exists {
		return r.applyConfiguration(ctx, repo, nexusClient, desiredConfig, exists, nil, log)
	}
	if diff := r.configDiff(desiredConfig, currentConfig); len(diff) > 0 {
		return r.applyConfiguration(ctx, repo, nexusClient, desiredConfig, exists, diff, log)
	}

	log.V(1).Info("Конфигурация актуальна")
	opmetrics.SyncTotal.WithLabelValues("repository", "success").Inc()
	return r.updateStatus(ctx, repo, true, nil)
}

// configDiff возвращает пути расхождений между desired и current конфигурациями
// (без значений — безопасно для логирования). Пустой список = обновление не нужно.
// desired нормализуется через JSON round-trip (Go structs → примитивы),
// current фильтруется: остаются только ключи, присутствующие в desired.
func (r *RepositoryReconciler) configDiff(desired, current map[string]interface{}) []string {
	normalized, err := normalizeConfig(desired)
	if err != nil {
		// при ошибке нормализации — обновляем на всякий случай
		return []string{"<ошибка нормализации desired-конфигурации>"}
	}

	// Nexus API возвращает routingRuleName в GET, но ожидает routingRule в PUT/POST.
	// Нормализуем ключ в current для корректного сравнения с desired.
	if rr, ok := current["routingRuleName"]; ok {
		current["routingRule"] = rr
	}

	filtered := filterKeys(current, normalized)

	ignoreFields := cmp.Options{
		cmpopts.IgnoreMapEntries(func(k string, v interface{}) bool {
			return k == "lastUpdated" || k == "taskId" || k == "url" || k == "contentDisposition"
		}),
		cmp.FilterPath(func(p cmp.Path) bool {
			return p.String() == "Attributes.checksum" ||
				p.String() == "raw.contentDisposition"
		}, cmp.Ignore()),
	}
	return diffPaths(normalized, filtered, ignoreFields)
}

// normalizeConfig выполняет JSON round-trip: Go structs → JSON → map[string]interface{}.
// Это приводит типы к тем же примитивам, что возвращает Nexus API (float64 для чисел и т.д.).
func normalizeConfig(config map[string]interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("ошибка маршалинга конфигурации: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("ошибка демаршалинга конфигурации: %w", err)
	}
	return result, nil
}

// filterKeys рекурсивно фильтрует source, оставляя только ключи, присутствующие в reference.
// Для вложенных map — рекурсия. Это позволяет игнорировать дополнительные поля,
// которые Nexus API добавляет в ответ (format, type, replication и т.д.).
// Null из Nexus нормализуется к zero value из desired (null == "" == 0 == false).
func filterKeys(source, reference map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(reference))
	for key, refVal := range reference {
		srcVal, exists := source[key]
		if !exists {
			continue
		}
		refMap, refIsMap := refVal.(map[string]interface{})
		srcMap, srcIsMap := srcVal.(map[string]interface{})
		if refIsMap && srcIsMap {
			result[key] = filterKeys(srcMap, refMap)
		} else if srcVal == nil && isZeroValue(refVal) {
			// Nexus возвращает null для неустановленных полей,
			// а desired содержит Go zero value ("", 0, false) — это эквивалентно.
			result[key] = refVal
		} else {
			result[key] = srcVal
		}
	}
	return result
}

// isZeroValue проверяет, является ли значение zero value для своего типа.
func isZeroValue(v interface{}) bool {
	switch val := v.(type) {
	case string:
		return val == ""
	case float64:
		return val == 0
	case bool:
		return !val
	default:
		return v == nil
	}
}

func (r *RepositoryReconciler) applyConfiguration(
	ctx context.Context,
	repo *nexusv1alpha1.Repository,
	nexusClient *nexus.Client,
	config map[string]interface{},
	exists bool,
	changedFields []string,
	log logr.Logger,
) (ctrl.Result, error) {
	if exists {
		start := time.Now()
		if err := nexusClient.UpdateRepository(ctx, repo.Spec.Type, repo.Spec.Name, config); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("UpdateRepository").Observe(time.Since(start).Seconds())
			log.Error(err, "Ошибка обновления репозитория", "name", repo.Spec.Name, "changed", changedFields)
			opmetrics.SyncTotal.WithLabelValues("repository", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("repository").Inc()
			return r.updateStatus(ctx, repo, false, fmt.Errorf("ошибка обновления: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("UpdateRepository").Observe(time.Since(start).Seconds())
		log.Info("Репозиторий успешно обновлен", "name", repo.Spec.Name, "changed", changedFields)
	} else {
		start := time.Now()
		if err := nexusClient.CreateRepository(ctx, repo.Spec.Type, config); err != nil {
			opmetrics.NexusAPIDuration.WithLabelValues("CreateRepository").Observe(time.Since(start).Seconds())
			log.Error(err, "Ошибка создания репозитория", "name", repo.Spec.Name, "type", repo.Spec.Type)
			opmetrics.SyncTotal.WithLabelValues("repository", "error").Inc()
			opmetrics.SyncErrorsTotal.WithLabelValues("repository").Inc()
			return r.updateStatus(ctx, repo, false, fmt.Errorf("ошибка создания: %w", err))
		}
		opmetrics.NexusAPIDuration.WithLabelValues("CreateRepository").Observe(time.Since(start).Seconds())
		log.Info("Репозиторий успешно создан", "name", repo.Spec.Name, "type", repo.Spec.Type)
	}

	opmetrics.SyncTotal.WithLabelValues("repository", "success").Inc()
	return r.updateStatus(ctx, repo, true, nil)
}

func (r *RepositoryReconciler) finalizeRepository(
	ctx context.Context,
	repo *nexusv1alpha1.Repository,
	log logr.Logger,
) (ctrl.Result, error) {
	log.Info("Начало процедуры удаления репозитория")

	if os.Getenv("ENABLE_REPOSITORY_DELETION") == "true" {
		nexusClient, err := r.nexusClient()
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
	opmetrics.DeleteResourceReady("repository", repo.Namespace, repo.Name)
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
	opmetrics.SetResourceReady("repository", repo.Namespace, repo.Name, ready)
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

	if ready {
		now := metav1.Now()
		repo.Status.LastSyncTime = &now
		repo.Status.SyncErrors = 0
	} else {
		repo.Status.SyncErrors++
	}

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

// resolveAuthFromSecret читает credentials из Kubernetes Secret, если указан secretRef.
// Значение Secret-ключа — JSON: {"username":"...","password":"..."}.
// Заполняет поля Username и Password в AuthConfig для отправки в Nexus API.
func (r *RepositoryReconciler) resolveAuthFromSecret(
	ctx context.Context,
	repo *nexusv1alpha1.Repository,
) error {
	if repo.Spec.HttpClient == nil || repo.Spec.HttpClient.Authentication == nil {
		return nil
	}
	auth := repo.Spec.HttpClient.Authentication
	if auth.SecretRef == nil {
		return nil
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Name:      auth.SecretRef.Name,
		Namespace: repo.Namespace,
	}, &secret); err != nil {
		if k8serrors.IsNotFound(err) && r.esoEnabled() && auth.SecretRef.Name == r.ESOTargetSecret {
			if esoErr := r.ensureExternalSecret(ctx, repo.Namespace); esoErr != nil {
				return fmt.Errorf("не удалось создать ExternalSecret: %w", esoErr)
			}
			ready, msg, statusErr := r.getExternalSecretStatus(ctx, repo.Namespace)
			if statusErr != nil {
				return fmt.Errorf("%w: %s, ошибка проверки статуса ExternalSecret: %w",
					errSecretNotFound, auth.SecretRef.Name, statusErr)
			}
			switch {
			case msg == "":
				// Нет conditions — только что создан
				return fmt.Errorf("%w: %s, ExternalSecret создан — ожидаем синхронизацию ESO",
					errSecretNotFound, auth.SecretRef.Name)
			case ready:
				// ESO считает, что синхронизация прошла, но Secret не появился
				return fmt.Errorf("%w: %s, ExternalSecret синхронизирован, но Secret не создан (проверьте target.name): %s",
					errSecretNotFound, auth.SecretRef.Name, msg)
			default:
				// Ready=False — ошибка синхронизации
				return fmt.Errorf("%w: %s, ExternalSecret не синхронизирован: %s",
					errSecretNotFound, auth.SecretRef.Name, msg)
			}
		}
		return fmt.Errorf("не удалось получить Secret %s: %w", auth.SecretRef.Name, err)
	}

	usedKey := auth.SecretRef.Key
	raw, ok := secret.Data[usedKey]
	if !ok && repo.Spec.Name != auth.SecretRef.Key {
		// Fallback: имя в K8s (metadata.name) может отличаться от имени в Nexus (spec.name),
		// например repo-cft vs repo.cft. В Vault ключи часто именуются по Nexus-имени.
		usedKey = repo.Spec.Name
		raw, ok = secret.Data[usedKey]
		if ok {
			r.Log.V(1).Info("Ключ secretRef.key в Secret не найден, использован fallback по spec.name",
				"repository", repo.Spec.Name, "secret", auth.SecretRef.Name,
				"secretRefKey", auth.SecretRef.Key, "usedKey", usedKey)
		}
	}
	if !ok {
		return fmt.Errorf("%w: %s в %s", errSecretKeyNotFound, auth.SecretRef.Key, auth.SecretRef.Name)
	}

	var creds struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(raw, &creds); err != nil {
		return fmt.Errorf("ошибка парсинга JSON из ключа %s Secret %s: %w",
			auth.SecretRef.Key, auth.SecretRef.Name, err)
	}

	if creds.Password == "" {
		return fmt.Errorf("%w: ключ %s в %s", errEmptyPassword, auth.SecretRef.Key, auth.SecretRef.Name)
	}

	auth.Username = creds.Username
	auth.Password = creds.Password
	r.Log.V(1).Info("Креды для репозитория прочитаны из Secret",
		"repository", repo.Spec.Name, "secret", auth.SecretRef.Name, "key", usedKey)
	return nil
}

func (r *RepositoryReconciler) updateStatusAuthPending(
	ctx context.Context,
	repo *nexusv1alpha1.Repository,
) (ctrl.Result, error) {
	opmetrics.SetResourceReady("repository", repo.Namespace, repo.Name, true)
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "AuthPending",
		Message:            "Обновление пропущено: в Nexus настроена аутентификация, отсутствующая в CR. Заполните spec.httpClient.authentication",
		ObservedGeneration: repo.Generation,
	}

	currentCondition := meta.FindStatusCondition(repo.Status.Conditions, "Ready")
	if currentCondition != nil &&
		currentCondition.Status == condition.Status &&
		currentCondition.Reason == condition.Reason &&
		currentCondition.ObservedGeneration == repo.Generation {
		return ctrl.Result{}, nil
	}

	meta.SetStatusCondition(&repo.Status.Conditions, condition)

	if err := r.Status().Update(ctx, repo); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("ошибка обновления статуса: %w", err)
	}
	return ctrl.Result{}, nil
}

// esoEnabled возвращает true, если настроена ESO-интеграция (заданы SecretStore и VaultPath).
func (r *RepositoryReconciler) esoEnabled() bool {
	return r.ESOSecretStore != "" && r.ESOVaultPath != ""
}

// ensureExternalSecret создаёт или обновляет ExternalSecret в указанном namespace.
// При обновлении синхронизирует refreshInterval с настройкой оператора.
// Используется unstructured client, чтобы не зависеть от ESO Go-типов.
func (r *RepositoryReconciler) ensureExternalSecret(ctx context.Context, namespace string) error {
	esGVK := schema.GroupVersionKind{
		Group:   "external-secrets.io",
		Version: "v1",
		Kind:    "ExternalSecret",
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(esGVK)
	err := r.Get(ctx, types.NamespacedName{
		Name:      r.ESOTargetSecret,
		Namespace: namespace,
	}, existing)

	if err == nil {
		// ExternalSecret существует — проверяем refreshInterval
		currentInterval, _, _ := unstructured.NestedString(existing.Object, "spec", "refreshInterval")
		if currentInterval != r.ESORefreshInterval {
			r.Log.Info("Обновление refreshInterval в ExternalSecret",
				"name", r.ESOTargetSecret, "namespace", namespace,
				"old", currentInterval, "new", r.ESORefreshInterval)
			if err := unstructured.SetNestedField(existing.Object, r.ESORefreshInterval, "spec", "refreshInterval"); err != nil {
				return fmt.Errorf("ошибка установки refreshInterval: %w", err)
			}
			if err := r.Update(ctx, existing); err != nil {
				return fmt.Errorf("ошибка обновления ExternalSecret %s/%s: %w", namespace, r.ESOTargetSecret, err)
			}
		}
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("ошибка проверки ExternalSecret: %w", err)
	}

	es := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "external-secrets.io/v1",
			"kind":       "ExternalSecret",
			"metadata": map[string]interface{}{
				"name":      r.ESOTargetSecret,
				"namespace": namespace,
				"annotations": map[string]interface{}{
					"nexus.kostoed.ru/managed-by": "nexus-operator",
				},
			},
			"spec": map[string]interface{}{
				"refreshInterval": r.ESORefreshInterval,
				"secretStoreRef": map[string]interface{}{
					"name": r.ESOSecretStore,
					"kind": "ClusterSecretStore",
				},
				"target": map[string]interface{}{
					"name":           r.ESOTargetSecret,
					"creationPolicy": "Owner",
				},
				"dataFrom": []interface{}{
					map[string]interface{}{
						"rewrite": []interface{}{
							map[string]interface{}{
								"regexp": map[string]interface{}{
									"source": ".*/([^/]+)$",
									"target": "$1",
								},
							},
						},
						"find": map[string]interface{}{
							"path": r.ESOVaultPath,
							"name": map[string]interface{}{
								"regexp": ".*",
							},
						},
					},
				},
			},
		},
	}

	if err := r.Create(ctx, es); err != nil {
		return fmt.Errorf("ошибка создания ExternalSecret %s/%s: %w", namespace, r.ESOTargetSecret, err)
	}

	r.Log.Info("ExternalSecret создан, ожидаем синхронизацию ESO",
		"name", r.ESOTargetSecret, "namespace", namespace)
	return nil
}

// getExternalSecretStatus проверяет статус ExternalSecret (condition Ready).
// Возвращает: ready (status=="True"), message из condition, ошибку при проблемах с API.
// Если conditions пусты (только создан), возвращает ready=false, message="".
func (r *RepositoryReconciler) getExternalSecretStatus(ctx context.Context, namespace string) (bool, string, error) {
	esGVK := schema.GroupVersionKind{
		Group:   "external-secrets.io",
		Version: "v1",
		Kind:    "ExternalSecret",
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(esGVK)
	if err := r.Get(ctx, types.NamespacedName{
		Name:      r.ESOTargetSecret,
		Namespace: namespace,
	}, obj); err != nil {
		return false, "", fmt.Errorf("ошибка получения ExternalSecret: %w", err)
	}

	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return false, "", fmt.Errorf("ошибка чтения conditions ExternalSecret: %w", err)
	}
	if !found || len(conditions) == 0 {
		return false, "", nil
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(cond, "type")
		if condType != "Ready" {
			continue
		}
		status, _, _ := unstructured.NestedString(cond, "status")
		message, _, _ := unstructured.NestedString(cond, "message")
		reason, _, _ := unstructured.NestedString(cond, "reason")
		if message == "" && reason != "" {
			message = reason
		}
		return status == "True", message, nil
	}

	return false, "", nil
}

// repositoryRequestsForSecret возвращает запросы на reconcile для Repository
// в namespace секрета, ссылающихся на него через spec.httpClient.authentication.secretRef.
// Интересует только target-Secret ESO-интеграции (r.ESOTargetSecret).
// Если changedKeys не nil — только репозитории, чьи креды лежат в изменённых ключах
// (ключ из secretRef.Key либо fallback по spec.name — та же логика, что в resolveAuthFromSecret).
func (r *RepositoryReconciler) repositoryRequestsForSecret(
	ctx context.Context,
	secret client.Object,
	changedKeys map[string]bool,
) []reconcile.Request {
	if secret.GetName() != r.ESOTargetSecret {
		return nil
	}

	var repos nexusv1alpha1.RepositoryList
	if err := r.List(ctx, &repos, client.InNamespace(secret.GetNamespace())); err != nil {
		r.Log.Error(err, "Ошибка получения списка Repository для Secret",
			"secret", secret.GetName(), "namespace", secret.GetNamespace())
		return nil
	}

	requests := make([]reconcile.Request, 0, len(repos.Items))
	for i := range repos.Items {
		repo := &repos.Items[i]
		hc := repo.Spec.HttpClient
		if hc == nil || hc.Authentication == nil || hc.Authentication.SecretRef == nil {
			continue
		}
		secretRef := hc.Authentication.SecretRef
		if secretRef.Name != secret.GetName() {
			continue
		}
		if changedKeys != nil && !changedKeys[secretRef.Key] && !changedKeys[repo.Spec.Name] {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      repo.Name,
				Namespace: repo.Namespace,
			},
		})
	}
	return requests
}

// changedSecretKeys возвращает отсортированный список ключей, чьи значения
// различаются между old и new (добавленные и удалённые тоже считаются изменёнными).
func changedSecretKeys(oldData, newData map[string][]byte) []string {
	changed := make(map[string]bool)
	for key, oldVal := range oldData {
		if newVal, ok := newData[key]; !ok || !bytes.Equal(oldVal, newVal) {
			changed[key] = true
		}
	}
	for key := range newData {
		if _, ok := oldData[key]; !ok {
			changed[key] = true
		}
	}
	keys := make([]string, 0, len(changed))
	for key := range changed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// onSecretCreate обрабатывает появление target-Secret (включая реплей informer'а
// на старте оператора — тогда репозитории и так проходят полный reconcile,
// поэтому логируем на уровне debug).
func (r *RepositoryReconciler) onSecretCreate(
	ctx context.Context,
	e event.CreateEvent,
	q workqueue.RateLimitingInterface,
) {
	requests := r.repositoryRequestsForSecret(ctx, e.Object, nil)
	if len(requests) == 0 {
		return
	}
	r.Log.V(1).Info("Secret с кредами прокси-репозиториев появился в кэше",
		"secret", e.Object.GetName(), "namespace", e.Object.GetNamespace(),
		"repositories", requestNames(requests))
	for _, req := range requests {
		q.Add(req)
	}
}

// onSecretUpdate обрабатывает обновление target-Secret: определяет изменённые
// ключи и ставит в очередь только затронутые репозитории. В лог попадают
// имена ключей (совпадают с именами репозиториев), но не значения.
func (r *RepositoryReconciler) onSecretUpdate(
	ctx context.Context,
	e event.UpdateEvent,
	q workqueue.RateLimitingInterface,
) {
	if e.ObjectNew.GetName() != r.ESOTargetSecret {
		return
	}
	oldSecret, okOld := e.ObjectOld.(*corev1.Secret)
	newSecret, okNew := e.ObjectNew.(*corev1.Secret)
	if !okOld || !okNew {
		return
	}

	changed := changedSecretKeys(oldSecret.Data, newSecret.Data)
	if len(changed) == 0 {
		// Изменились только метаданные (labels, annotations) — креды прежние
		return
	}

	changedSet := make(map[string]bool, len(changed))
	for _, key := range changed {
		changedSet[key] = true
	}

	requests := r.repositoryRequestsForSecret(ctx, newSecret, changedSet)
	if len(requests) == 0 {
		r.Log.Info("Secret с кредами обновлён, но изменённые ключи не используются ни одним Repository",
			"secret", newSecret.Name, "namespace", newSecret.Namespace, "changedKeys", changed)
		return
	}

	r.Log.Info("Secret с кредами обновлён, ставим репозитории в очередь синхронизации",
		"secret", newSecret.Name, "namespace", newSecret.Namespace,
		"changedKeys", changed, "repositories", requestNames(requests))
	for _, req := range requests {
		q.Add(req)
	}
}

// onSecretDelete логирует удаление target-Secret — синхронизация кредов
// затронутых репозиториев работать не будет, оператор пересоздаст
// ExternalSecret при следующем reconcile.
func (r *RepositoryReconciler) onSecretDelete(
	ctx context.Context,
	e event.DeleteEvent,
	_ workqueue.RateLimitingInterface,
) {
	requests := r.repositoryRequestsForSecret(ctx, e.Object, nil)
	if len(requests) == 0 {
		return
	}
	r.Log.Info("Secret с кредами прокси-репозиториев удалён — синхронизация auth недоступна до пересоздания",
		"secret", e.Object.GetName(), "namespace", e.Object.GetNamespace(),
		"repositories", requestNames(requests))
}

// requestNames возвращает имена ресурсов из запросов reconcile для логирования.
func requestNames(requests []reconcile.Request) []string {
	names := make([]string, 0, len(requests))
	for _, req := range requests {
		names = append(names, req.Name)
	}
	return names
}

func (r *RepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Предикаты навешаны через builder.WithPredicates на конкретные источники,
	// а не глобальным WithEventFilter — иначе фильтр по generation/annotations
	// отсёк бы события Secret (у него generation не меняется при обновлении data).
	ctrlBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&nexusv1alpha1.Repository{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)))

	// Watch на Secret нужен только при включённой ESO-интеграции —
	// без неё не запускаем informer по Secrets впустую.
	if r.esoEnabled() {
		ctrlBuilder = ctrlBuilder.Watches(&corev1.Secret{}, handler.Funcs{
			CreateFunc: r.onSecretCreate,
			UpdateFunc: r.onSecretUpdate,
			DeleteFunc: r.onSecretDelete,
		})
	} else {
		r.Log.Info("ESO-интеграция выключена, watch на Secret с кредами прокси-репозиториев не регистрируется")
	}

	if err := ctrlBuilder.Complete(r); err != nil {
		return fmt.Errorf("не удалось создать контроллер: %w", err)
	}
	return nil
}
