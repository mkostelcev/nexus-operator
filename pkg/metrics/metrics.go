package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// SyncTotal — счётчик итого синхронизаций по типу ресурса и результату.
	SyncTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nexus_operator_sync_total",
			Help: "Total number of sync operations by resource type and result",
		},
		[]string{"type", "result"},
	)

	// SyncErrorsTotal — счётчик итого ошибок синхронизации по типу ресурса.
	SyncErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nexus_operator_sync_errors_total",
			Help: "Total number of sync errors by resource type",
		},
		[]string{"type"},
	)

	// NexusAPIDuration — гистограмма latency обращений к Nexus API.
	NexusAPIDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nexus_operator_nexus_api_duration_seconds",
			Help:    "Duration of Nexus API calls in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method"},
	)

	// ResourceReady — состояние каждого CR по условию Ready: 1 — синхронизирован
	// с Nexus, 0 — последняя синхронизация завершилась ошибкой (в том числе 400 от
	// Nexus API на невалидную декларацию). В отличие от счётчиков ошибок метрика
	// именует конкретный ресурс, поэтому алерт может сказать, что именно чинить.
	// Метка resource_namespace, а не namespace: при скрейпе через ServiceMonitor
	// vmagent вешает свою target-метку namespace, и одноимённая метка метрики
	// молча переименовалась бы в exported_namespace.
	ResourceReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nexus_operator_resource_ready",
			Help: "Ready condition of a custom resource: 1 — synced with Nexus, 0 — last sync failed",
		},
		[]string{"type", "resource_namespace", "name"},
	)
)

// SetResourceReady вызывается при каждой записи статуса, а не только при смене
// состояния: после рестарта оператора серия восстанавливается первым же reconcile.
func SetResourceReady(resourceType, namespace, name string, ready bool) {
	value := 0.0
	if ready {
		value = 1
	}
	ResourceReady.WithLabelValues(resourceType, namespace, name).Set(value)
}

// DeleteResourceReady убирает серию удалённого CR, иначе после удаления ресурса
// с Ready=False алерт висел бы вечно.
func DeleteResourceReady(resourceType, namespace, name string) {
	ResourceReady.DeleteLabelValues(resourceType, namespace, name)
}

func init() {
	metrics.Registry.MustRegister(SyncTotal, SyncErrorsTotal, NexusAPIDuration, ResourceReady)
}
