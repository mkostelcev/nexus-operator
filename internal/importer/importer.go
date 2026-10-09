package importer

import (
	"context"
	stderrors "errors"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
	"github.com/mkostelcev/nexus-operator/pkg/utils"
)

const (
	labelImported       = "nexus.kostoed.ru/imported"
	annotationNexusName = "nexus.kostoed.ru/nexus-name"
	annotationNeedsAuth = "nexus.kostoed.ru/needs-auth-config"
)

var errImportWithErrors = stderrors.New("импорт завершён с ошибками")

// ImportOptions содержит настройки импорта.
type ImportOptions struct {
	Namespace          string
	SkipBuiltins       bool
	DryRun             bool
	EnabledControllers map[string]bool // nil = все, иначе только перечисленные
}

// Importer выполняет импорт ресурсов из Nexus в Kubernetes CR.
type Importer struct {
	k8sClient   client.Client
	nexusClient *nexus.Client
	log         logr.Logger
	opts        ImportOptions
}

// NewImporter создаёт новый экземпляр Importer.
func NewImporter(k8sClient client.Client, nexusClient *nexus.Client, log logr.Logger, opts ImportOptions) *Importer {
	return &Importer{
		k8sClient:   k8sClient,
		nexusClient: nexusClient,
		log:         log,
		opts:        opts,
	}
}

// controllerEnabled проверяет, разрешён ли импорт для данного контроллера.
// Если EnabledControllers == nil (не задан фильтр), разрешены все.
func (imp *Importer) controllerEnabled(name string) bool {
	if imp.opts.EnabledControllers == nil {
		return true
	}
	return imp.opts.EnabledControllers[name]
}

// Run выполняет импорт всех поддерживаемых ресурсов из Nexus.
// Порядок импорта по зависимостям: content-selectors → repositories → privileges → roles.
// Если задан EnabledControllers, импортируются только указанные типы ресурсов.
func (imp *Importer) Run(ctx context.Context) error {
	imp.log.Info("Начало импорта ресурсов из Nexus",
		"namespace", imp.opts.Namespace,
		"skipBuiltins", imp.opts.SkipBuiltins,
		"dryRun", imp.opts.DryRun,
	)

	var totalCreated, totalSkipped, totalErrors int

	type importStep struct {
		controller string
		fn         func(context.Context) (int, int, int)
	}

	steps := []importStep{
		{"ContentSelector", imp.importContentSelectors},
		{"Repository", imp.importRepositories},
		{"Privilege", imp.importPrivileges},
		{"Role", imp.importRoles},
		{"NexusTeamAccess", imp.importNexusTeamAccesses},
		{"NexusUser", imp.importUsers},
		{"RoutingRule", imp.importRoutingRules},
		{"NexusConfiguration", imp.importNexusConfiguration},
	}

	for _, step := range steps {
		if !imp.controllerEnabled(step.controller) {
			imp.log.Info("Контроллер не включён, пропуск импорта", "controller", step.controller)
			continue
		}
		created, skipped, errs := step.fn(ctx)
		totalCreated += created
		totalSkipped += skipped
		totalErrors += errs
	}

	imp.log.Info("Импорт завершён",
		"created", totalCreated,
		"skipped", totalSkipped,
		"errors", totalErrors,
	)

	if totalErrors > 0 {
		return fmt.Errorf("%w: %d", errImportWithErrors, totalErrors)
	}
	return nil
}

func (imp *Importer) importContentSelectors(ctx context.Context) (created, skipped, errs int) {
	log := imp.log.WithName("content-selectors")
	log.Info("Импорт Content Selectors")

	selectors, err := imp.nexusClient.ListContentSelectors(ctx)
	if err != nil {
		log.Error(err, "Ошибка получения списка Content Selectors")
		return 0, 0, 1
	}
	log.Info("Получено Content Selectors из Nexus", "count", len(selectors))

	var skippedExists int

	for _, cs := range selectors {
		spec := nexus.BuildContentSelectorSpecFromAPI(cs)
		k8sName := utils.SanitizeK8sName(cs.Name)

		cr := &v1alpha1.ContentSelector{
			ObjectMeta: metav1.ObjectMeta{
				Name:      k8sName,
				Namespace: imp.opts.Namespace,
				Labels: map[string]string{
					labelImported: "true",
				},
				Annotations: map[string]string{
					annotationNexusName: cs.Name,
				},
			},
			Spec: spec,
		}

		if imp.opts.DryRun {
			log.Info("DRY-RUN: создание ContentSelector", "name", k8sName, "nexusName", cs.Name)
			created++
			continue
		}

		if err := imp.k8sClient.Create(ctx, cr); err != nil {
			if errors.IsAlreadyExists(err) {
				log.V(1).Info("ContentSelector уже существует, пропуск", "name", k8sName)
				skippedExists++
				skipped++
			} else {
				log.Error(err, "Ошибка создания ContentSelector", "name", k8sName)
				errs++
			}
		} else {
			log.Info("ContentSelector создан", "name", k8sName, "nexusName", cs.Name)
			created++
		}
	}

	if skipped > 0 {
		log.Info("ContentSelectors: итог пропусков",
			"skippedExists", skippedExists,
		)
	}

	return created, skipped, errs
}

func (imp *Importer) importRepositories(ctx context.Context) (created, skipped, errs int) {
	log := imp.log.WithName("repositories")
	log.Info("Импорт Repositories")

	repos, err := imp.nexusClient.ListRepositories(ctx)
	if err != nil {
		log.Error(err, "Ошибка получения списка репозиториев")
		return 0, 0, 1
	}
	log.Info("Получено репозиториев из Nexus", "count", len(repos))

	var skippedUnsupported, skippedNotFound, updated int

	for _, repo := range repos {
		operatorType, supported := nexusFormatTypeToOperatorType(repo.Format, repo.Type)
		if !supported {
			log.Info("Неподдерживаемый тип репозитория, пропуск",
				"name", repo.Name, "format", repo.Format, "type", repo.Type)
			skippedUnsupported++
			skipped++
			continue
		}

		// Получаем полную конфигурацию через format-specific endpoint
		config, err := imp.nexusClient.GetRepositoryByType(ctx, repo.Format, repo.Type, repo.Name)
		if err != nil {
			if stderrors.Is(err, nexus.ErrRepositoryNotFound) {
				log.Info("Репозиторий не найден через API (возможно удалён или имеет другой тип), пропуск",
					"name", repo.Name, "format", repo.Format, "type", repo.Type)
				skippedNotFound++
				skipped++
			} else {
				log.Error(err, "Ошибка получения конфигурации репозитория",
					"name", repo.Name, "format", repo.Format, "type", repo.Type)
				errs++
			}
			continue
		}

		spec := nexus.BuildRepositorySpecFromConfig(config, operatorType)
		k8sName := utils.SanitizeK8sName(repo.Name)

		// Обработка авторизации: Nexus API не возвращает пароли/токены,
		// поэтому обнуляем auth и помечаем аннотацией для ручной доработки.
		hasAuth := nexus.ConfigHasAuthentication(config)
		if hasAuth && spec.HttpClient != nil {
			spec.HttpClient.Authentication = nil
			log.Info("WARNING: репозиторий имеет авторизацию, секреты не импортированы — требуется ручная настройка",
				"name", repo.Name, "type", operatorType)
		}

		annotations := map[string]string{
			annotationNexusName: repo.Name,
		}
		if hasAuth {
			annotations[annotationNeedsAuth] = "true"
		}

		cr := &v1alpha1.Repository{
			ObjectMeta: metav1.ObjectMeta{
				Name:      k8sName,
				Namespace: imp.opts.Namespace,
				Labels: map[string]string{
					labelImported: "true",
				},
				Annotations: annotations,
			},
			Spec: spec,
		}

		if imp.opts.DryRun {
			log.Info("DRY-RUN: создание Repository",
				"name", k8sName, "nexusName", repo.Name, "type", operatorType, "needsAuth", hasAuth)
			created++
			continue
		}

		if err := imp.k8sClient.Create(ctx, cr); err != nil {
			if errors.IsAlreadyExists(err) {
				// Обновляем существующий CR
				existing := &v1alpha1.Repository{}
				if getErr := imp.k8sClient.Get(ctx, client.ObjectKeyFromObject(cr), existing); getErr != nil {
					log.Error(getErr, "Ошибка получения существующего Repository", "name", k8sName)
					errs++
					continue
				}
				existing.Spec = spec
				// Мержим аннотации
				if existing.Annotations == nil {
					existing.Annotations = make(map[string]string)
				}
				for k, v := range annotations {
					existing.Annotations[k] = v
				}
				if updateErr := imp.k8sClient.Update(ctx, existing); updateErr != nil {
					log.Error(updateErr, "Ошибка обновления Repository", "name", k8sName)
					errs++
				} else {
					log.Info("Repository обновлён", "name", k8sName, "nexusName", repo.Name, "type", operatorType)
					updated++
				}
			} else {
				log.Error(err, "Ошибка создания Repository", "name", k8sName)
				errs++
			}
		} else {
			log.Info("Repository создан", "name", k8sName, "nexusName", repo.Name, "type", operatorType)
			created++
		}
	}

	if skipped > 0 || updated > 0 {
		log.Info("Repositories: итог",
			"updated", updated,
			"skippedUnsupported", skippedUnsupported,
			"skippedNotFound", skippedNotFound,
		)
	}

	return created, skipped, errs
}

func (imp *Importer) importPrivileges(ctx context.Context) (created, skipped, errs int) {
	log := imp.log.WithName("privileges")
	log.Info("Импорт Privileges")

	privs, err := imp.nexusClient.ListPrivileges(ctx)
	if err != nil {
		log.Error(err, "Ошибка получения списка привилегий")
		return 0, 0, 1
	}
	log.Info("Получено привилегий из Nexus", "count", len(privs))

	var skippedBuiltins, skippedUnsupported, skippedExists int

	for _, priv := range privs {
		if imp.opts.SkipBuiltins && isBuiltinPrivilege(priv) {
			log.V(1).Info("Встроенная привилегия, пропуск", "name", priv.Name)
			skippedBuiltins++
			skipped++
			continue
		}

		if !isSupportedPrivilegeType(priv.Type) {
			log.V(1).Info("Неподдерживаемый тип привилегии, пропуск",
				"name", priv.Name, "type", priv.Type)
			skippedUnsupported++
			skipped++
			continue
		}

		// Получаем полные данные привилегии
		data, err := imp.nexusClient.GetPrivilege(ctx, priv.Name)
		if err != nil {
			log.Error(err, "Ошибка получения данных привилегии", "name", priv.Name)
			errs++
			continue
		}

		spec := nexus.BuildPrivilegeSpecFromAPI(data)
		k8sName := utils.SanitizeK8sName(priv.Name)

		cr := &v1alpha1.Privilege{
			ObjectMeta: metav1.ObjectMeta{
				Name:      k8sName,
				Namespace: imp.opts.Namespace,
				Labels: map[string]string{
					labelImported: "true",
				},
				Annotations: map[string]string{
					annotationNexusName: priv.Name,
				},
			},
			Spec: spec,
		}

		if imp.opts.DryRun {
			log.Info("DRY-RUN: создание Privilege",
				"name", k8sName, "nexusName", priv.Name, "type", priv.Type)
			created++
			continue
		}

		if err := imp.k8sClient.Create(ctx, cr); err != nil {
			if errors.IsAlreadyExists(err) {
				log.V(1).Info("Privilege уже существует, пропуск", "name", k8sName)
				skippedExists++
				skipped++
			} else {
				log.Error(err, "Ошибка создания Privilege", "name", k8sName)
				errs++
			}
		} else {
			log.Info("Privilege создан", "name", k8sName, "nexusName", priv.Name, "type", priv.Type)
			created++
		}
	}

	if skipped > 0 {
		log.Info("Privileges: итог пропусков",
			"skippedBuiltins", skippedBuiltins,
			"skippedUnsupported", skippedUnsupported,
			"skippedExists", skippedExists,
		)
	}

	return created, skipped, errs
}

func (imp *Importer) importRoles(ctx context.Context) (created, skipped, errs int) {
	log := imp.log.WithName("roles")
	log.Info("Импорт Roles")

	roles, err := imp.nexusClient.ListRoles(ctx)
	if err != nil {
		log.Error(err, "Ошибка получения списка ролей")
		return 0, 0, 1
	}
	log.Info("Получено ролей из Nexus", "count", len(roles))

	var skippedBuiltins, skippedExists int

	for _, role := range roles {
		if imp.opts.SkipBuiltins && isBuiltinRole(role) {
			log.V(1).Info("Встроенная роль, пропуск",
				"name", role.Name, "readOnly", role.ReadOnly, "source", role.Source)
			skippedBuiltins++
			skipped++
			continue
		}

		spec := nexus.BuildRoleSpecFromAPI(role)
		k8sName := utils.SanitizeK8sName(role.ID)

		cr := &v1alpha1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      k8sName,
				Namespace: imp.opts.Namespace,
				Labels: map[string]string{
					labelImported: "true",
				},
				Annotations: map[string]string{
					annotationNexusName: role.ID,
				},
			},
			Spec: spec,
		}

		if imp.opts.DryRun {
			log.Info("DRY-RUN: создание Role",
				"name", k8sName, "roleId", role.ID, "nexusName", role.Name)
			created++
			continue
		}

		if err := imp.k8sClient.Create(ctx, cr); err != nil {
			if errors.IsAlreadyExists(err) {
				log.V(1).Info("Role уже существует, пропуск", "name", k8sName)
				skippedExists++
				skipped++
			} else {
				log.Error(err, "Ошибка создания Role", "name", k8sName)
				errs++
			}
		} else {
			log.Info("Role создан", "name", k8sName, "roleId", role.ID, "nexusName", role.Name)
			created++
		}
	}

	if skipped > 0 {
		log.Info("Roles: итог пропусков",
			"skippedBuiltins", skippedBuiltins,
			"skippedExists", skippedExists,
		)
	}

	return created, skipped, errs
}

func (imp *Importer) importUsers(ctx context.Context) (created, skipped, errs int) {
	log := imp.log.WithName("users")
	log.Info("Импорт Users")

	users, err := imp.nexusClient.ListUsers(ctx)
	if err != nil {
		log.Error(err, "Ошибка получения списка пользователей")
		return 0, 0, 1
	}
	log.Info("Получено пользователей из Nexus", "count", len(users))

	var skippedBuiltins, skippedExists int

	for _, user := range users {
		if imp.opts.SkipBuiltins && isBuiltinUser(user) {
			log.V(1).Info("Встроенный пользователь, пропуск",
				"userId", user.UserId, "readOnly", user.ReadOnly)
			skippedBuiltins++
			skipped++
			continue
		}

		spec := nexus.BuildUserSpecFromAPI(user)
		k8sName := utils.SanitizeK8sName(user.UserId)

		cr := &v1alpha1.NexusUser{
			ObjectMeta: metav1.ObjectMeta{
				Name:      k8sName,
				Namespace: imp.opts.Namespace,
				Labels: map[string]string{
					labelImported: "true",
				},
				Annotations: map[string]string{
					annotationNexusName: user.UserId,
					annotationNeedsAuth: "true",
				},
			},
			Spec: spec,
		}

		if imp.opts.DryRun {
			log.Info("DRY-RUN: создание NexusUser",
				"name", k8sName, "userId", user.UserId)
			created++
			continue
		}

		if err := imp.k8sClient.Create(ctx, cr); err != nil {
			if errors.IsAlreadyExists(err) {
				log.V(1).Info("NexusUser уже существует, пропуск", "name", k8sName)
				skippedExists++
				skipped++
			} else {
				log.Error(err, "Ошибка создания NexusUser", "name", k8sName)
				errs++
			}
		} else {
			log.Info("NexusUser создан (требуется настройка credentials секрета)",
				"name", k8sName, "userId", user.UserId)
			created++
		}
	}

	if skipped > 0 {
		log.Info("Users: итог пропусков",
			"skippedBuiltins", skippedBuiltins,
			"skippedExists", skippedExists,
		)
	}

	return created, skipped, errs
}

func (imp *Importer) importNexusConfiguration(ctx context.Context) (created, skipped, errs int) {
	log := imp.log.WithName("nexus-configuration")
	log.Info("Импорт NexusConfiguration (HTTP Proxy + Security Realms)")

	// 1. Получить HTTP Proxy конфигурацию
	httpProxyConfig, err := imp.nexusClient.GetHttpProxy(ctx)
	if err != nil {
		log.Error(err, "Ошибка получения HTTP Proxy конфигурации")
		return 0, 0, 1
	}

	// 2. Получить Active Realms
	activeRealms, err := imp.nexusClient.GetActiveRealms(ctx)
	if err != nil {
		log.Error(err, "Ошибка получения активных Security Realms")
		return 0, 0, 1
	}

	// 3. Построить spec
	spec := v1alpha1.NexusConfigurationSpec{
		HttpProxy:      nexus.BuildHttpProxySpecFromAPI(httpProxyConfig),
		SecurityRealms: nexus.BuildSecurityRealmsSpecFromAPI(activeRealms),
	}

	// Проверяем наличие auth в прокси (пароли будут замаскированы)
	hasAuth := false
	if httpProxyConfig.HttpProxy != nil && httpProxyConfig.HttpProxy.Authentication != nil {
		hasAuth = true
	}
	if httpProxyConfig.HttpsProxy != nil && httpProxyConfig.HttpsProxy.Authentication != nil {
		hasAuth = true
	}

	annotations := map[string]string{
		annotationNexusName: "nexus-config",
	}
	if hasAuth {
		annotations[annotationNeedsAuth] = "true"
		log.Info("WARNING: HTTP Proxy имеет авторизацию, пароли замаскированы — требуется ручная настройка")
	}

	cr := &v1alpha1.NexusConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nexus-config",
			Namespace: imp.opts.Namespace,
			Labels: map[string]string{
				labelImported: "true",
			},
			Annotations: annotations,
		},
		Spec: spec,
	}

	if imp.opts.DryRun {
		log.Info("DRY-RUN: создание NexusConfiguration", "name", "nexus-config")
		return 1, 0, 0
	}

	if err := imp.k8sClient.Create(ctx, cr); err != nil {
		if errors.IsAlreadyExists(err) {
			log.Info("NexusConfiguration уже существует, пропуск", "name", "nexus-config")
			return 0, 1, 0
		}
		log.Error(err, "Ошибка создания NexusConfiguration", "name", "nexus-config")
		return 0, 0, 1
	}

	log.Info("NexusConfiguration создан", "name", "nexus-config")
	return 1, 0, 0
}

func (imp *Importer) importRoutingRules(ctx context.Context) (created, skipped, errs int) {
	log := imp.log.WithName("routing-rules")
	log.Info("Импорт Routing Rules")

	rules, err := imp.nexusClient.ListRoutingRules(ctx)
	if err != nil {
		log.Error(err, "Ошибка получения списка Routing Rules")
		return 0, 0, 1
	}
	log.Info("Получено Routing Rules из Nexus", "count", len(rules))

	var skippedExists int

	for _, rule := range rules {
		spec := nexus.BuildRoutingRuleSpecFromAPI(rule)
		k8sName := utils.SanitizeK8sName(rule.Name)

		cr := &v1alpha1.RoutingRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      k8sName,
				Namespace: imp.opts.Namespace,
				Labels: map[string]string{
					labelImported: "true",
				},
				Annotations: map[string]string{
					annotationNexusName: rule.Name,
				},
			},
			Spec: spec,
		}

		if imp.opts.DryRun {
			log.Info("DRY-RUN: создание RoutingRule",
				"name", k8sName, "nexusName", rule.Name, "mode", rule.Mode)
			created++
			continue
		}

		if err := imp.k8sClient.Create(ctx, cr); err != nil {
			if errors.IsAlreadyExists(err) {
				log.V(1).Info("RoutingRule уже существует, пропуск", "name", k8sName)
				skippedExists++
				skipped++
			} else {
				log.Error(err, "Ошибка создания RoutingRule", "name", k8sName)
				errs++
			}
		} else {
			log.Info("RoutingRule создан", "name", k8sName, "nexusName", rule.Name, "mode", rule.Mode)
			created++
		}
	}

	if skipped > 0 {
		log.Info("RoutingRules: итог пропусков",
			"skippedExists", skippedExists,
		)
	}

	return created, skipped, errs
}
