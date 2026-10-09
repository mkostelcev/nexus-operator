package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/internal/appconfig"
	"github.com/mkostelcev/nexus-operator/internal/controller"
	"github.com/mkostelcev/nexus-operator/internal/importer"
	nexuswebhook "github.com/mkostelcev/nexus-operator/internal/webhook"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	Version   = "dev"
	GoVersion = "unknown"
	Compiler  = "unknown"
	Platform  = "unknown"
	startTime = time.Now()
	appName   = "nexus-operator"

	errMissingEnvVar = nexus.ErrMissingEnvVars
	errUnknownMode   = errors.New("неизвестный режим")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(nexusv1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		secureMetrics        bool
		enableHTTP2          bool
		devMode              bool
		enableWebhooks       bool
		mode                 string
		watchNamespace       string
		importNamespace      string
		dryRun               bool
		skipBuiltins         bool
		controllers          string
		showVersion          bool
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8081", "Metrics bind address")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8080", "Health probe bind address")
	// DEPRECATED: leader election теперь конфигурируется через файл (см. internal/appconfig).
	// Флаг оставлен, чтобы не ломать внешние чарты, которые могут его передавать;
	// его значение игнорируется. Удалить после миграции всех окружений.
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"DEPRECATED: настраивается через config.leaderElection в values")
	flag.BoolVar(&secureMetrics, "metrics-secure", false, "Secure metrics serving")
	flag.BoolVar(&enableHTTP2, "enable-http2", false, "Enable HTTP/2")
	flag.BoolVar(&devMode, "dev", false, "Development mode")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", false, "Enable validating webhooks")
	flag.StringVar(&mode, "mode", "reconcile", "Operator mode: reconcile or import")
	flag.StringVar(&watchNamespace, "watch-namespace", "",
		"Namespace to watch in reconcile mode. Default: pod's own namespace")
	flag.StringVar(&importNamespace, "import-namespace", "",
		"Namespace to create CRs in import mode. Empty means all namespaces")
	flag.BoolVar(&dryRun, "dry-run", false, "Import mode: only log, do not create CRs")
	flag.BoolVar(&skipBuiltins, "skip-builtins", true, "Import mode: skip built-in resources (readOnly roles/privileges)")
	flag.StringVar(&controllers, "controllers", "",
		"Comma-separated list of controllers to enable (e.g. Repository,Role). Empty means all")
	flag.BoolVar(&showVersion, "version", false, "Print version information and exit")

	opts := zap.Options{
		Development: devMode,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	if showVersion {
		fmt.Printf("%s %s (go: %s, compiler: %s, platform: %s)\n",
			appName, Version, GoVersion, Compiler, Platform)
		os.Exit(0)
	}

	// Переменные окружения переопределяют флаги
	if envMode := os.Getenv("OPERATOR_MODE"); envMode != "" {
		mode = envMode
	}
	if envNs := os.Getenv("WATCH_NAMESPACE"); envNs != "" {
		watchNamespace = envNs
	}
	if envNs := os.Getenv("IMPORT_NAMESPACE"); envNs != "" {
		importNamespace = envNs
	}
	if envCtrl := os.Getenv("ENABLED_CONTROLLERS"); envCtrl != "" {
		controllers = envCtrl
	}
	if envWebhooks := os.Getenv("ENABLE_WEBHOOKS"); envWebhooks != "" {
		v, err := strconv.ParseBool(envWebhooks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ENABLE_WEBHOOKS: %v\n", err)
			os.Exit(1)
		}
		enableWebhooks = v
	}

	// Reconcile-режим: если watch namespace не задан — используем namespace пода
	if watchNamespace == "" {
		if ns, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
			watchNamespace = strings.TrimSpace(string(ns))
		}
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	logWatchNs := watchNamespace
	if logWatchNs == "" {
		logWatchNs = "<all>"
	}

	setupLog.Info("Starting Nexus Operator",
		"app", appName,
		"version", Version,
		"goVersion", GoVersion,
		"compiler", Compiler,
		"platform", Platform,
		"startTime", startTime.Format(time.RFC3339),
		"mode", mode,
		"watchNamespace", logWatchNs,
		"importNamespace", importNamespace,
	)

	if err := checkEnvVars(); err != nil {
		handleCriticalError(err, "Environment variables check failed")
	}

	// Грузим файловый конфиг (ConfigMap, монтируется из values.config).
	// Если файла нет — используется пустой Config с дефолтами.
	cfg, err := appconfig.Load()
	if err != nil {
		handleCriticalError(err, "Ошибка загрузки конфигурации оператора")
	}
	if enableLeaderElection {
		// Пользователь явно передал устаревший флаг — предупреждаем
		// и просим перенести настройку в config.leaderElection.
		setupLog.Info("Флаг --leader-elect устарел и игнорируется; используйте config.leaderElection")
	}

	// Парсим список контроллеров
	var enabledControllers map[string]bool
	if controllers != "" {
		enabledControllers = make(map[string]bool)
		for _, name := range strings.Split(controllers, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				enabledControllers[name] = true
			}
		}
		setupLog.Info("Включены только выбранные контроллеры", "controllers", controllers)
	}

	switch mode {
	case "reconcile":
		runReconcileMode(
			metricsAddr, probeAddr,
			secureMetrics, enableHTTP2, enableWebhooks, enabledControllers,
			watchNamespace, cfg,
		)
	case "import":
		runImportMode(importNamespace, dryRun, skipBuiltins, enabledControllers)
	default:
		handleCriticalError(fmt.Errorf("%w: %s", errUnknownMode, mode), "Invalid mode")
	}
}

func runReconcileMode(
	metricsAddr, probeAddr string,
	secureMetrics, enableHTTP2, enableWebhooks bool,
	enabledControllers map[string]bool,
	watchNamespace string,
	cfg *appconfig.Config,
) {
	// Настройка TLS
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}
	if !enableHTTP2 {
		tlsConfig.NextProtos = []string{"http/1.1"}
	}

	// Создаем функции для настройки TLS без копирования всего конфига
	tlsOpts := []func(*tls.Config){
		func(c *tls.Config) {
			c.MinVersion = tlsConfig.MinVersion
			c.NextProtos = tlsConfig.NextProtos
		},
	}

	// Параметры leader election: дефолт — включён, тайминги
	// nil -> используются стандартные (15s/10s/2s controller-runtime).
	le := cfg.LeaderElection
	setupLog.Info("Leader election",
		"enabled", le.IsEnabled(),
		"id", le.GetID(),
		"leaseDuration", le.LeaseDurationPtr(),
		"renewDeadline", le.RenewDeadlinePtr(),
		"retryPeriod", le.RetryPeriodPtr(),
	)

	mgrOptions := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			TLSOpts: tlsOpts,
		}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         le.IsEnabled(),
		LeaderElectionID:       le.GetID(),
		LeaseDuration:          le.LeaseDurationPtr(),
		RenewDeadline:          le.RenewDeadlinePtr(),
		RetryPeriod:            le.RetryPeriodPtr(),
	}

	if watchNamespace != "" {
		mgrOptions.Cache = cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				watchNamespace: {},
			},
		}
		setupLog.Info("Ограничение watch namespace", "namespace", watchNamespace)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOptions)
	if err != nil {
		handleCriticalError(err, "Ошибка инициализации Manager")
	}

	if err := initControllers(mgr, enabledControllers); err != nil {
		handleCriticalError(err, "Ошибка инициализации контроллеров")
	}

	if enableWebhooks {
		if err := setupWebhooks(mgr); err != nil {
			handleCriticalError(err, "Ошибка инициализации вебхуков")
		}
	}

	if err := setupHealthChecks(mgr); err != nil {
		handleCriticalError(err, "Ошибка инициализации Health-Checks")
	}

	setupLog.Info("Запуск менеджера")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		handleCriticalError(err, "Ошибка запуска менеджера")
	}
}

func runImportMode(namespace string, dryRun, skipBuiltins bool, enabledControllers map[string]bool) {
	setupLog.Info("Запуск режима импорта",
		"namespace", namespace,
		"dryRun", dryRun,
		"skipBuiltins", skipBuiltins,
	)

	ctx := context.Background()

	// Создаём Nexus-клиент
	nexusClient, err := nexus.GetClient()
	if err != nil {
		handleCriticalError(err, "Ошибка создания Nexus-клиента")
	}

	// Создаём K8s-клиент
	cfg := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		handleCriticalError(err, "Ошибка создания K8s-клиента")
	}

	imp := importer.NewImporter(k8sClient, nexusClient, setupLog.WithName("importer"), importer.ImportOptions{
		Namespace:          namespace,
		SkipBuiltins:       skipBuiltins,
		DryRun:             dryRun,
		EnabledControllers: enabledControllers,
	})

	if err := imp.Run(ctx); err != nil {
		handleCriticalError(err, "Ошибка импорта")
	}

	setupLog.Info("Импорт завершён успешно")
	os.Exit(0)
}

func checkEnvVars() error {
	required := map[string]string{
		"NEXUS_URL":      "Nexus server URL",
		"NEXUS_USER":     "Nexus admin username",
		"NEXUS_PASSWORD": "Nexus admin password",
	}

	var missing []string
	for env, desc := range required {
		if os.Getenv(env) == "" {
			missing = append(missing, fmt.Sprintf("%s (%s)", env, desc))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("%w: %v", errMissingEnvVar, missing)
	}

	// Keycloak env vars are optional — log a warning if not set.
	keycloakVars := []string{
		"KEYCLOAK_URL",
		"KEYCLOAK_REALM",
		"KEYCLOAK_CLIENT_ID",
		"KEYCLOAK_CLIENT_SECRET",
		"KEYCLOAK_NEXUS_CLIENT",
	}
	var missingKC []string
	for _, env := range keycloakVars {
		if os.Getenv(env) == "" {
			missingKC = append(missingKC, env)
		}
	}
	if len(missingKC) > 0 {
		setupLog.Info("Keycloak env vars not set, KeycloakRoleSync controller will not work",
			"missing", missingKC)
	}

	return nil
}

func initControllers(mgr ctrl.Manager, enabledControllers map[string]bool) error {
	controllers := []struct {
		name     string
		disabled bool // по умолчанию не запускается без явного указания
		init     func() error
	}{
		{
			name: "Repository",
			init: func() error {
				return (&controller.RepositoryReconciler{
					Client:             mgr.GetClient(),
					Scheme:             mgr.GetScheme(),
					Log:                mgr.GetLogger().WithValues("controller", "Repository"),
					ESOSecretStore:     os.Getenv("ESO_SECRET_STORE"),
					ESOVaultPath:       os.Getenv("ESO_VAULT_PATH"),
					ESOTargetSecret:    getEnvOrDefault("ESO_PROXY_AUTH_SECRET", "nexus-proxy-auth"),
					ESORefreshInterval: getEnvOrDefault("ESO_REFRESH_INTERVAL", "1h"),
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "ContentSelector",
			init: func() error {
				return (&controller.ContentSelectorReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Log:    mgr.GetLogger().WithValues("controller", "ContentSelector"),
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "Privilege",
			init: func() error {
				return (&controller.PrivilegeReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Log:    mgr.GetLogger().WithValues("controller", "Privilege"),
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "Role",
			init: func() error {
				return (&controller.RoleReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Log:    mgr.GetLogger().WithValues("controller", "Role"),
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "RoutingRule",
			init: func() error {
				return (&controller.RoutingRuleReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Log:    mgr.GetLogger().WithValues("controller", "RoutingRule"),
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "NexusConfiguration",
			init: func() error {
				return (&controller.NexusConfigurationReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Log:    mgr.GetLogger().WithValues("controller", "NexusConfiguration"),
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "NexusTeamAccess",
			init: func() error {
				return (&controller.NexusTeamAccessReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Log:    mgr.GetLogger().WithValues("controller", "NexusTeamAccess"),
				}).SetupWithManager(mgr)
			},
		},
		{
			name: "KeycloakRoleSync",
			init: func() error {
				return (&controller.KeycloakRoleSyncReconciler{
					Client: mgr.GetClient(),
					Log:    mgr.GetLogger().WithValues("controller", "KeycloakRoleSync"),
				}).SetupWithManager(mgr)
			},
		},
		// NexusTeamBinding disabled: role-to-user assignment is managed
		// by Keycloak admins via groups, not declaratively.
		// {
		// 	name: "NexusTeamBinding",
		// 	init: func() error {
		// 		return (&controller.NexusTeamBindingReconciler{
		// 			Client: mgr.GetClient(),
		// 			Log:    mgr.GetLogger().WithValues("controller", "NexusTeamBinding"),
		// 		}).SetupWithManager(mgr)
		// 	},
		// },
		{
			name:     "NexusUser",
			disabled: true, // экспериментальный, включается явно через --controllers или ENABLED_CONTROLLERS
			init: func() error {
				return (&controller.NexusUserReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Log:    mgr.GetLogger().WithValues("controller", "NexusUser"),
				}).SetupWithManager(mgr)
			},
		},
	}

	enabledNames := make([]string, 0, len(controllers))
	skippedNames := make([]string, 0, len(controllers))

	for _, c := range controllers {
		if enabledControllers != nil {
			// Если список задан явно — запускаем только перечисленные
			if !enabledControllers[c.name] {
				skippedNames = append(skippedNames, c.name)
				continue
			}
		} else if c.disabled {
			// Список не задан, но контроллер отключён по умолчанию
			skippedNames = append(skippedNames, c.name)
			continue
		}

		if err := c.init(); err != nil {
			return fmt.Errorf("%s controller: %w", c.name, err)
		}
		enabledNames = append(enabledNames, c.name)
	}

	setupLog.Info("Инициализация контроллеров завершена",
		"enabled", enabledNames,
		"skipped", skippedNames,
	)
	return nil
}

func setupWebhooks(mgr ctrl.Manager) error {
	if err := ctrl.NewWebhookManagedBy(mgr).
		For(&nexusv1alpha1.Repository{}).
		WithValidator(&nexuswebhook.RepositoryValidator{}).
		Complete(); err != nil {
		return fmt.Errorf("repository webhook: %w", err)
	}
	setupLog.Info("Вебхук инициализирован", "webhook", "Repository")

	if err := ctrl.NewWebhookManagedBy(mgr).
		For(&nexusv1alpha1.RoutingRule{}).
		WithValidator(&nexuswebhook.RoutingRuleValidator{}).
		Complete(); err != nil {
		return fmt.Errorf("routingrule webhook: %w", err)
	}
	setupLog.Info("Вебхук инициализирован", "webhook", "RoutingRule")

	if err := ctrl.NewWebhookManagedBy(mgr).
		For(&nexusv1alpha1.NexusConfiguration{}).
		WithValidator(&nexuswebhook.NexusConfigurationValidator{}).
		Complete(); err != nil {
		return fmt.Errorf("nexusconfiguration webhook: %w", err)
	}
	setupLog.Info("Вебхук инициализирован", "webhook", "NexusConfiguration")

	return nil
}

func setupHealthChecks(mgr ctrl.Manager) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("healthz check: %w", err)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("readyz check: %w", err)
	}

	setupLog.Info("Health-checks настроены")
	return nil
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func handleCriticalError(err error, message string) {
	setupLog.Error(err, message)
	os.Exit(1)
}
