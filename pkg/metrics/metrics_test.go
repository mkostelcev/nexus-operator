package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// resourceReadyHeader — шапка экспозиции, общая для всех ожиданий ниже.
// Имя метрики и текст Help — часть внешнего контракта: на них завязаны правила
// алертинга, поэтому сверяем их дословно, а не через Go-переменную.
const resourceReadyHeader = "# HELP nexus_operator_resource_ready " +
	"Ready condition of a custom resource: 1 — synced with Nexus, 0 — last sync failed\n" +
	"# TYPE nexus_operator_resource_ready gauge\n"

// assertResourceReady сверяет полную экспозицию метрики с ожидаемой.
// Именно полную: CollectAndCompare падает и когда серия отсутствует, и когда
// появилась лишняя, и когда разъехались имена меток. Чтение через
// ToFloat64(WithLabelValues(...)) так не умеет — WithLabelValues молча создаёт
// отсутствующую серию со значением 0, из-за чего проверка «ожидали 0»
// проходит даже при полностью неработающем SetResourceReady.
func assertResourceReady(t *testing.T, lines ...string) {
	t.Helper()
	expected := resourceReadyHeader
	if len(lines) > 0 {
		expected += strings.Join(lines, "\n") + "\n"
	}
	if err := testutil.CollectAndCompare(
		ResourceReady, strings.NewReader(expected), "nexus_operator_resource_ready",
	); err != nil {
		t.Fatalf("экспозиция метрики разошлась с ожидаемой: %v", err)
	}
}

// Проверяем полный цикл серии: выставление 0/1 и удаление после снятия финализатора.
func TestResourceReadyLifecycle(t *testing.T) {
	// Реестр глобальный, поэтому начинаем с чистого состояния и чистим за собой.
	ResourceReady.Reset()
	t.Cleanup(ResourceReady.Reset)

	// До первого вызова серий быть не должно: оператор не выдумывает ресурсы.
	assertResourceReady(t)

	SetResourceReady("repository", "nexus", "maven-public", false)
	assertResourceReady(t,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="nexus",type="repository"} 0`,
	)

	// Повторный вызов с ready=true обязан перезаписать ту же серию, а не завести вторую.
	SetResourceReady("repository", "nexus", "maven-public", true)
	assertResourceReady(t,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="nexus",type="repository"} 1`,
	)

	// Соседний ресурс не должен затрагиваться. Одноимённый ресурс в другом
	// namespace — отдельная серия: иначе алерт указал бы не на тот объект.
	SetResourceReady("role", "nexus", "dev-role", false)
	SetResourceReady("repository", "sandbox", "maven-public", false)
	assertResourceReady(t,
		`nexus_operator_resource_ready{name="dev-role",resource_namespace="nexus",type="role"} 0`,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="nexus",type="repository"} 1`,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="sandbox",type="repository"} 0`,
	)

	// Удаление снимает ровно одну серию — свою.
	DeleteResourceReady("repository", "nexus", "maven-public")
	assertResourceReady(t,
		`nexus_operator_resource_ready{name="dev-role",resource_namespace="nexus",type="role"} 0`,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="sandbox",type="repository"} 0`,
	)

	// Повторное удаление несуществующей серии не должно ни паниковать, ни задевать соседей.
	DeleteResourceReady("repository", "nexus", "maven-public")
	assertResourceReady(t,
		`nexus_operator_resource_ready{name="dev-role",resource_namespace="nexus",type="role"} 0`,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="sandbox",type="repository"} 0`,
	)
}

// Метрика бесполезна, если не зарегистрирована в реестре controller-runtime:
// vmagent скрейпит именно его. Заодно фиксируем имя метки resource_namespace —
// одноимённая с target-меткой namespace молча переехала бы в exported_namespace.
func TestResourceReadyExposedViaControllerRuntimeRegistry(t *testing.T) {
	ResourceReady.Reset()
	t.Cleanup(ResourceReady.Reset)

	SetResourceReady("repository", "nexus", "maven-public", true)

	expected := resourceReadyHeader +
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="nexus",type="repository"} 1` + "\n"
	if err := testutil.GatherAndCompare(
		ctrlmetrics.Registry, strings.NewReader(expected), "nexus_operator_resource_ready",
	); err != nil {
		t.Fatalf("метрика не отдаётся реестром controller-runtime в ожидаемом виде: %v", err)
	}
}
