package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/internal/controller"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	version   = "0.1.0-dev"
	commit    = "none"
	buildDate = "unknown"
	startTime = time.Now()
	appName   = "nexus-operator-kostoed"

	errMissingEnvVar = nexus.ErrMissingEnvVars
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
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8081", "Metrics bind address")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8080", "Health probe bind address")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election")
	flag.BoolVar(&secureMetrics, "metrics-secure", false, "Secure metrics serving")
	flag.BoolVar(&enableHTTP2, "enable-http2", false, "Enable HTTP/2")
	flag.BoolVar(&devMode, "dev", false, "Development mode")

	opts := zap.Options{
		Development: devMode,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("Запуск Nexus Operator",
		"app", appName,
		"version", version,
		"commit", commit,
		"buildDate", buildDate,
		"startTime", startTime.Format(time.RFC3339),
	)

	if err := checkEnvVars(); err != nil {
		handleCriticalError(err, "Ошибка проверки ENV-переменных")
	}

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

	// Запуск кастомного health-сервера
	go startHealthServer(probeAddr)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			TLSOpts: tlsOpts,
		}),
		// HealthProbeBindAddress: probeAddr,
		LeaderElection:   enableLeaderElection,
		LeaderElectionID: "nexus-operator-lock",
	})
	if err != nil {
		handleCriticalError(err, "Ошибка инициализации Manager")
	}

	if err := initControllers(mgr); err != nil {
		handleCriticalError(err, "Ошибка инициализации контроллеров")
	}

	if err := setupHealthChecks(mgr); err != nil {
		handleCriticalError(err, "Ошибка инициализации Health-Checks")
	}

	setupLog.Info("Запуск менеджера")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		handleCriticalError(err, "Ошибка запуска менеджера")
	}
}

func checkEnvVars() error {
	required := map[string]string{
		"NEXUS_URL":      "URL Nexus",
		"NEXUS_USER":     "Пользователь Nexus",
		"NEXUS_PASSWORD": "Пароль Nexus",
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
	return nil
}

// Новый метод для запуска health-сервера
func startHealthServer(address string) {
	router := http.NewServeMux()
	router.HandleFunc("/health/liveness", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	router.HandleFunc("/health/readiness", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:              address,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	setupLog.Info("Запуск health-сервера", "address", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		setupLog.Error(err, "Ошибка health-сервера")
		os.Exit(1)
	}
}

func initControllers(mgr ctrl.Manager) error {
	controllers := []struct {
		name string
		init func() error
	}{
		{
			name: "Repository",
			init: func() error {
				return (&controller.RepositoryReconciler{
					Client: mgr.GetClient(),
					Scheme: mgr.GetScheme(),
					Log:    mgr.GetLogger().WithValues("controller", "Repository"),
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
	}

	// // Настройка health-сервера
	// router := http.NewServeMux()
	// router.HandleFunc("/health/liveness", func(w http.ResponseWriter, r *http.Request) {
	// 	_, _ = w.Write([]byte("OK"))
	// })
	// router.HandleFunc("/health/readiness", func(w http.ResponseWriter, r *http.Request) {
	// 	_, _ = w.Write([]byte("OK"))
	// })

	for _, c := range controllers {
		if err := c.init(); err != nil {
			return fmt.Errorf("%s controller: %w", c.name, err)
		}
		setupLog.Info("Контроллер инициализирован", "controller", c.name)
	}
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

func handleCriticalError(err error, message string) {
	setupLog.Error(err, message)
	os.Exit(1)
}
