package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	opmetrics "github.com/mkostelcev/nexus-operator/pkg/metrics"
)

// ---------------------------------------------------------------------------
// normalizeConfig
// ---------------------------------------------------------------------------

func TestNormalizeConfig(t *testing.T) {
	t.Run("converts Go structs to primitive maps", func(t *testing.T) {
		input := map[string]interface{}{
			"name":   "test-repo",
			"online": true,
			"storage": nexusv1alpha1.StorageConfig{
				BlobStoreName:               "default",
				StrictContentTypeValidation: true,
				WritePolicy:                 "ALLOW",
			},
			"httpClient": &nexusv1alpha1.HttpClientConfig{
				Blocked:   false,
				AutoBlock: true,
			},
		}

		result, err := normalizeConfig(input)
		require.NoError(t, err)

		// storage должен стать map[string]interface{}, а не StorageConfig
		storage, ok := result["storage"].(map[string]interface{})
		require.True(t, ok, "storage should be map[string]interface{} after normalization")
		assert.Equal(t, "default", storage["blobStoreName"])
		assert.Equal(t, true, storage["strictContentTypeValidation"])
		assert.Equal(t, "ALLOW", storage["writePolicy"])

		// httpClient тоже
		hc, ok := result["httpClient"].(map[string]interface{})
		require.True(t, ok, "httpClient should be map[string]interface{} after normalization")
		assert.Equal(t, false, hc["blocked"])
		assert.Equal(t, true, hc["autoBlock"])
	})

	t.Run("int fields become float64 after JSON round-trip", func(t *testing.T) {
		input := map[string]interface{}{
			"negativeCache": &nexusv1alpha1.NegativeCacheConfig{
				Enabled:    true,
				TimeToLive: 300,
			},
		}

		result, err := normalizeConfig(input)
		require.NoError(t, err)

		nc, ok := result["negativeCache"].(map[string]interface{})
		require.True(t, ok)
		// JSON числа всегда float64
		assert.Equal(t, float64(300), nc["timeToLive"])
	})

	t.Run("plain maps pass through unchanged", func(t *testing.T) {
		input := map[string]interface{}{
			"name":   "test",
			"online": true,
			"storage": map[string]interface{}{
				"blobStoreName": "default",
			},
		}

		result, err := normalizeConfig(input)
		require.NoError(t, err)
		assert.Equal(t, "test", result["name"])
		assert.Equal(t, true, result["online"])
		storage := result["storage"].(map[string]interface{})
		assert.Equal(t, "default", storage["blobStoreName"])
	})
}

// ---------------------------------------------------------------------------
// filterKeys
// ---------------------------------------------------------------------------

func TestFilterKeys(t *testing.T) {
	t.Run("keeps only keys from reference", func(t *testing.T) {
		source := map[string]interface{}{
			"name":            "repo1",
			"online":          true,
			"format":          "maven2",
			"type":            "hosted",
			"routingRuleName": nil,
		}
		reference := map[string]interface{}{
			"name":   "repo1",
			"online": true,
		}

		result := filterKeys(source, reference)
		assert.Equal(t, map[string]interface{}{
			"name":   "repo1",
			"online": true,
		}, result)
	})

	t.Run("recursive filtering for nested maps", func(t *testing.T) {
		source := map[string]interface{}{
			"httpClient": map[string]interface{}{
				"blocked":        false,
				"autoBlock":      true,
				"connection":     map[string]interface{}{"timeout": float64(20)},
				"authentication": map[string]interface{}{"type": "username"},
			},
		}
		reference := map[string]interface{}{
			"httpClient": map[string]interface{}{
				"blocked":   false,
				"autoBlock": true,
			},
		}

		result := filterKeys(source, reference)
		hc := result["httpClient"].(map[string]interface{})
		assert.Equal(t, false, hc["blocked"])
		assert.Equal(t, true, hc["autoBlock"])
		assert.Nil(t, hc["connection"], "connection should be filtered out")
		assert.Nil(t, hc["authentication"], "authentication should be filtered out")
	})

	t.Run("missing keys in source are skipped", func(t *testing.T) {
		source := map[string]interface{}{
			"name": "repo1",
		}
		reference := map[string]interface{}{
			"name":   "repo1",
			"online": true,
		}

		result := filterKeys(source, reference)
		assert.Equal(t, map[string]interface{}{
			"name": "repo1",
		}, result)
	})

	t.Run("empty reference returns empty result", func(t *testing.T) {
		source := map[string]interface{}{
			"name": "repo1",
		}
		reference := map[string]interface{}{}

		result := filterKeys(source, reference)
		assert.Equal(t, map[string]interface{}{}, result)
	})

	t.Run("null from Nexus normalized to zero value from desired", func(t *testing.T) {
		// Nexus возвращает null для неустановленных полей,
		// desired содержит Go zero value ("", 0, false)
		source := map[string]interface{}{
			"subdomain":   nil,
			"httpPort":    nil,
			"httpsPort":   nil,
			"online":      true,
			"contentDisp": nil,
		}
		reference := map[string]interface{}{
			"subdomain":   "",
			"httpPort":    float64(0),
			"httpsPort":   float64(0),
			"online":      true,
			"contentDisp": "",
		}

		result := filterKeys(source, reference)
		assert.Equal(t, "", result["subdomain"])
		assert.Equal(t, float64(0), result["httpPort"])
		assert.Equal(t, float64(0), result["httpsPort"])
		assert.Equal(t, true, result["online"])
		assert.Equal(t, "", result["contentDisp"])
	})

	t.Run("null from Nexus NOT normalized when desired is non-zero", func(t *testing.T) {
		source := map[string]interface{}{
			"subdomain": nil,
		}
		reference := map[string]interface{}{
			"subdomain": "my-sub",
		}

		result := filterKeys(source, reference)
		// nil != "my-sub", так что остаётся nil — configDiff увидит разницу
		assert.Nil(t, result["subdomain"])
	})
}

// ---------------------------------------------------------------------------
// configDiff
// ---------------------------------------------------------------------------

func TestNeedsUpdate(t *testing.T) {
	r := &RepositoryReconciler{}

	t.Run("identical configs after normalization → false", func(t *testing.T) {
		desired := map[string]interface{}{
			"name":   "test-repo",
			"online": true,
			"storage": nexusv1alpha1.StorageConfig{
				BlobStoreName:               "default",
				StrictContentTypeValidation: true,
				WritePolicy:                 "ALLOW",
			},
		}
		// Nexus API возвращает всё как примитивы + доп. поля
		current := map[string]interface{}{
			"name":   "test-repo",
			"online": true,
			"format": "maven2",
			"type":   "hosted",
			"url":    "https://nexus.example.com/repository/test-repo",
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
		}

		assert.Empty(t, r.configDiff(desired, current))
	})

	t.Run("changed field → true", func(t *testing.T) {
		desired := map[string]interface{}{
			"name":   "test-repo",
			"online": false, // changed
			"storage": nexusv1alpha1.StorageConfig{
				BlobStoreName:               "default",
				StrictContentTypeValidation: true,
				WritePolicy:                 "ALLOW",
			},
		}
		current := map[string]interface{}{
			"name":   "test-repo",
			"online": true,
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
		}

		assert.NotEmpty(t, r.configDiff(desired, current))
	})

	t.Run("extra Nexus fields ignored → false", func(t *testing.T) {
		desired := map[string]interface{}{
			"name":   "proxy-repo",
			"online": true,
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
			},
		}
		current := map[string]interface{}{
			"name":            "proxy-repo",
			"online":          true,
			"format":          "maven2",
			"type":            "proxy",
			"routingRuleName": nil,
			"replication":     map[string]interface{}{"preemptivePullEnabled": false},
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
			},
		}

		assert.Empty(t, r.configDiff(desired, current))
	})

	t.Run("httpClient struct vs map comparison → false when equal", func(t *testing.T) {
		desired := map[string]interface{}{
			"name":   "proxy-repo",
			"online": true,
			"storage": nexusv1alpha1.StorageConfig{
				BlobStoreName:               "default",
				StrictContentTypeValidation: true,
				WritePolicy:                 "ALLOW",
			},
			"httpClient": &nexusv1alpha1.HttpClientConfig{
				Blocked:   false,
				AutoBlock: true,
			},
		}
		current := map[string]interface{}{
			"name":   "proxy-repo",
			"online": true,
			"format": "npm",
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
			"httpClient": map[string]interface{}{
				"blocked":   false,
				"autoBlock": true,
				"connection": map[string]interface{}{
					"retries":                 float64(0),
					"timeout":                 float64(20),
					"enableCircularRedirects": false,
					"enableCookies":           false,
					"useTrustStore":           false,
				},
				"authentication": nil,
			},
		}

		assert.Empty(t, r.configDiff(desired, current))
	})

	t.Run("negativeCache int vs float64 → false when equal", func(t *testing.T) {
		desired := map[string]interface{}{
			"name":   "repo",
			"online": true,
			"storage": nexusv1alpha1.StorageConfig{
				BlobStoreName:               "default",
				StrictContentTypeValidation: true,
				WritePolicy:                 "ALLOW",
			},
			"negativeCache": &nexusv1alpha1.NegativeCacheConfig{
				Enabled:    true,
				TimeToLive: 1440,
			},
		}
		current := map[string]interface{}{
			"name":   "repo",
			"online": true,
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
			"negativeCache": map[string]interface{}{
				"enabled":    true,
				"timeToLive": float64(1440),
			},
		}

		assert.Empty(t, r.configDiff(desired, current))
	})

	t.Run("null vs zero value from Nexus → false", func(t *testing.T) {
		// Docker proxy: desired имеет v1Enabled: false (Go zero), Nexus возвращает subdomain: null
		// DockerConfig.Subdomain имеет omitempty, поэтому после JSON round-trip его не будет.
		// Но v1Enabled без omitempty — будет false. Nexus может вернуть null для таких полей.
		desired := map[string]interface{}{
			"name":   "docker-proxy",
			"online": true,
			"storage": nexusv1alpha1.StorageConfig{
				BlobStoreName:               "default",
				StrictContentTypeValidation: true,
				WritePolicy:                 "ALLOW",
			},
			"docker": nexusv1alpha1.DockerConfig{
				ForceBasicAuth: false,
				V1Enabled:      false,
			},
		}
		current := map[string]interface{}{
			"name":   "docker-proxy",
			"online": true,
			"format": "docker",
			"type":   "proxy",
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
			"docker": map[string]interface{}{
				"forceBasicAuth": false,
				"v1Enabled":      false,
				"subdomain":      nil, // Nexus возвращает null — но desired не имеет этого ключа после normalization
				"httpPort":       nil,
				"httpsPort":      nil,
			},
		}

		assert.Empty(t, r.configDiff(desired, current))
	})

	t.Run("null vs zero value for explicitly set fields → false", func(t *testing.T) {
		// Прямое тестирование: desired имеет zero values, Nexus возвращает null
		desired := map[string]interface{}{
			"name":   "repo",
			"online": true,
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
			"docker": map[string]interface{}{
				"forceBasicAuth": false,
				"v1Enabled":      false,
				"subdomain":      "",
				"httpPort":       float64(0),
			},
		}
		current := map[string]interface{}{
			"name":   "repo",
			"online": true,
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
			"docker": map[string]interface{}{
				"forceBasicAuth": false,
				"v1Enabled":      false,
				"subdomain":      nil,
				"httpPort":       nil,
			},
		}

		assert.Empty(t, r.configDiff(desired, current))
	})

	t.Run("routingRule in desired vs routingRuleName in current → true when different", func(t *testing.T) {
		desired := map[string]interface{}{
			"name":        "proxy-repo",
			"online":      true,
			"routingRule": "block-com-example",
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
		}
		current := map[string]interface{}{
			"name":            "proxy-repo",
			"online":          true,
			"routingRuleName": nil,
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
		}

		assert.NotEmpty(t, r.configDiff(desired, current))
	})

	t.Run("routingRule in desired matches routingRuleName in current → false", func(t *testing.T) {
		desired := map[string]interface{}{
			"name":        "proxy-repo",
			"online":      true,
			"routingRule": "block-com-example",
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
		}
		current := map[string]interface{}{
			"name":            "proxy-repo",
			"online":          true,
			"routingRuleName": "block-com-example",
			"format":          "maven2",
			"type":            "proxy",
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
		}

		assert.Empty(t, r.configDiff(desired, current))
	})

	t.Run("cleanup policy struct vs map → false when equal", func(t *testing.T) {
		desired := map[string]interface{}{
			"name":   "repo",
			"online": true,
			"storage": nexusv1alpha1.StorageConfig{
				BlobStoreName:               "default",
				StrictContentTypeValidation: true,
				WritePolicy:                 "ALLOW",
			},
			"cleanup": &nexusv1alpha1.CleanupPolicy{
				PolicyNames: []string{"policy-1", "policy-2"},
			},
		}
		current := map[string]interface{}{
			"name":   "repo",
			"online": true,
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW",
			},
			"cleanup": map[string]interface{}{
				"policyNames": []interface{}{"policy-1", "policy-2"},
			},
		}

		assert.Empty(t, r.configDiff(desired, current))
	})
}

// ---------------------------------------------------------------------------
// repositoryRequestsForSecret / changedSecretKeys / onSecretUpdate
// ---------------------------------------------------------------------------

func newRepoWithSecretRef(name, ns, secretName string) *nexusv1alpha1.Repository {
	return &nexusv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: nexusv1alpha1.RepositorySpec{
			Name: name,
			Type: "npm-proxy",
			HttpClient: &nexusv1alpha1.HttpClientConfig{
				Authentication: &nexusv1alpha1.AuthConfig{
					Type:      "username",
					SecretRef: &nexusv1alpha1.SecretKeySelector{Name: secretName, Key: name},
				},
			},
		},
	}
}

func newSecretWatchReconciler(t *testing.T, objs ...client.Object) *RepositoryReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, nexusv1alpha1.AddToScheme(scheme))
	return &RepositoryReconciler{
		Client:          fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build(),
		Log:             logr.Discard(),
		ESOTargetSecret: "nexus-proxy-auth",
	}
}

func TestRepositoryRequestsForSecret(t *testing.T) {
	// Репозитории: два ссылаются на target-Secret, один без auth,
	// один с другим Secret, один в другом namespace.
	r := newSecretWatchReconciler(t,
		newRepoWithSecretRef("consaltica-npm", "nexus", "nexus-proxy-auth"),
		newRepoWithSecretRef("other-proxy", "nexus", "nexus-proxy-auth"),
		newRepoWithSecretRef("foreign-secret", "nexus", "another-secret"),
		newRepoWithSecretRef("other-ns", "elsewhere", "nexus-proxy-auth"),
		&nexusv1alpha1.Repository{
			ObjectMeta: metav1.ObjectMeta{Name: "no-auth", Namespace: "nexus"},
			Spec:       nexusv1alpha1.RepositorySpec{Name: "no-auth", Type: "npm-proxy"},
		},
	)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "nexus-proxy-auth", Namespace: "nexus"},
	}

	t.Run("nil changedKeys enqueues all repositories referencing the secret in its namespace", func(t *testing.T) {
		requests := r.repositoryRequestsForSecret(context.Background(), secret, nil)
		var names []string
		for _, req := range requests {
			assert.Equal(t, "nexus", req.Namespace)
			names = append(names, req.Name)
		}
		assert.ElementsMatch(t, []string{"consaltica-npm", "other-proxy"}, names)
	})

	t.Run("changedKeys narrows to affected repositories", func(t *testing.T) {
		changed := map[string]bool{"consaltica-npm": true}
		requests := r.repositoryRequestsForSecret(context.Background(), secret, changed)
		require.Len(t, requests, 1)
		assert.Equal(t, "consaltica-npm", requests[0].Name)
	})

	t.Run("changedKeys matches by spec.name fallback", func(t *testing.T) {
		// Имя CR в K8s отличается от имени репозитория в Nexus (ключа в Vault)
		repo := newRepoWithSecretRef("repo-cft", "nexus", "nexus-proxy-auth")
		repo.Spec.Name = "repo.cft"
		repo.Spec.HttpClient.Authentication.SecretRef.Key = "repo-cft"
		rr := newSecretWatchReconciler(t, repo)

		requests := rr.repositoryRequestsForSecret(context.Background(), secret, map[string]bool{"repo.cft": true})
		require.Len(t, requests, 1)
		assert.Equal(t, "repo-cft", requests[0].Name)
	})

	t.Run("ignores secrets with a different name", func(t *testing.T) {
		other := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "some-other-secret", Namespace: "nexus"},
		}
		assert.Empty(t, r.repositoryRequestsForSecret(context.Background(), other, nil))
	})
}

func TestChangedSecretKeys(t *testing.T) {
	oldData := map[string][]byte{
		"unchanged": []byte("same"),
		"modified":  []byte("old"),
		"removed":   []byte("gone"),
	}
	newData := map[string][]byte{
		"unchanged": []byte("same"),
		"modified":  []byte("new"),
		"added":     []byte("fresh"),
	}

	assert.Equal(t, []string{"added", "modified", "removed"}, changedSecretKeys(oldData, newData))
	assert.Empty(t, changedSecretKeys(oldData, oldData))
}

func TestOnSecretUpdate(t *testing.T) {
	r := newSecretWatchReconciler(t,
		newRepoWithSecretRef("consaltica-npm", "nexus", "nexus-proxy-auth"),
		newRepoWithSecretRef("other-proxy", "nexus", "nexus-proxy-auth"),
	)

	base := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "nexus-proxy-auth", Namespace: "nexus"},
		Data: map[string][]byte{
			"consaltica-npm": []byte(`{"username":"u","password":"old"}`),
			"other-proxy":    []byte(`{"username":"u","password":"p"}`),
		},
	}

	drainQueue := func(q workqueue.RateLimitingInterface) []reconcile.Request {
		var requests []reconcile.Request
		for q.Len() > 0 {
			item, _ := q.Get()
			requests = append(requests, item.(reconcile.Request))
			q.Done(item)
		}
		return requests
	}

	t.Run("enqueues only repositories with changed keys", func(t *testing.T) {
		updated := base.DeepCopy()
		updated.Data["consaltica-npm"] = []byte(`{"username":"u","password":"new"}`)

		q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		r.onSecretUpdate(context.Background(), event.UpdateEvent{ObjectOld: base, ObjectNew: updated}, q)

		requests := drainQueue(q)
		require.Len(t, requests, 1)
		assert.Equal(t, "consaltica-npm", requests[0].Name)
	})

	t.Run("metadata-only change enqueues nothing", func(t *testing.T) {
		updated := base.DeepCopy()
		updated.ResourceVersion = "2"
		updated.Labels = map[string]string{"touched": "true"}

		q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		r.onSecretUpdate(context.Background(), event.UpdateEvent{ObjectOld: base, ObjectNew: updated}, q)

		assert.Zero(t, q.Len())
	})

	t.Run("foreign secret enqueues nothing", func(t *testing.T) {
		other := base.DeepCopy()
		other.Name = "some-other-secret"
		updated := other.DeepCopy()
		updated.Data["consaltica-npm"] = []byte(`{"username":"u","password":"new"}`)

		q := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		r.onSecretUpdate(context.Background(), event.UpdateEvent{ObjectOld: other, ObjectNew: updated}, q)

		assert.Zero(t, q.Len())
	})
}

// ---------------------------------------------------------------------------
// nexus_operator_resource_ready
// ---------------------------------------------------------------------------

// assertResourceReadySeries сверяет экспозицию метрики целиком.
// Сверяем именно экспозицию, а не ToFloat64(WithLabelValues(...)): последний
// молча создаёт отсутствующую серию со значением 0, поэтому проверка
// «ожидали 0» проходила бы и при полностью убранном вызове SetResourceReady.
//
// Метрика — пакетная переменная, общая на весь процесс теста, поэтому каждый
// тест начинается с ResourceReady.Reset() и убирает за собой через t.Cleanup.
// Изоляция держится только пока тесты идут последовательно: t.Parallel() в
// любом тесте, трогающем эту метрику, сломает сверку полной экспозиции.
func assertResourceReadySeries(t *testing.T, lines ...string) {
	t.Helper()
	expected := "# HELP nexus_operator_resource_ready " +
		"Ready condition of a custom resource: 1 — synced with Nexus, 0 — last sync failed\n" +
		"# TYPE nexus_operator_resource_ready gauge\n"
	if len(lines) > 0 {
		expected += strings.Join(lines, "\n") + "\n"
	}
	require.NoError(t, testutil.CollectAndCompare(
		opmetrics.ResourceReady, strings.NewReader(expected), "nexus_operator_resource_ready",
	), "экспозиция nexus_operator_resource_ready разошлась с ожидаемой")
}

// newMetricRepo — CR для проверок метрики. Финализатор на месте, чтобы тем же
// объектом можно было прогнать путь удаления.
func newMetricRepo() *nexusv1alpha1.Repository {
	return &nexusv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "maven-public",
			Namespace:  "nexus",
			Generation: 1,
			Finalizers: []string{repositoryFinalizer},
		},
		// Имя в Nexus намеренно отличается от имени CR: в метке name должно
		// оказаться имя объекта в кластере, иначе по алерту не сделать kubectl get.
		Spec: nexusv1alpha1.RepositorySpec{Name: "maven-public-group", Type: "maven2-group"},
	}
}

// newMetricReconciler собирает реконсайлер на fake-клиенте: ни сети, ни Nexus.
func newMetricReconciler(t *testing.T, repo *nexusv1alpha1.Repository) *RepositoryReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, nexusv1alpha1.AddToScheme(scheme))
	return &RepositoryReconciler{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(repo).
			WithStatusSubresource(repo).
			Build(),
		Log: logr.Discard(),
	}
}

// Проверяем связку статуса и метрики: неудачная синхронизация даёт Ready=False и
// nexus_operator_resource_ready=0, успешная — Ready=True и 1.
func TestUpdateStatusSetsResourceReadyGauge(t *testing.T) {
	opmetrics.ResourceReady.Reset()
	t.Cleanup(opmetrics.ResourceReady.Reset)

	repo := newMetricRepo()
	r := newMetricReconciler(t, repo)

	res, err := r.updateStatus(context.Background(), repo, false, errors.New("400 Bad Request: invalid recipe"))
	require.NoError(t, err)
	// Неуспех обязан вернуться в очередь, иначе ресурс залипнет в Ready=False навсегда.
	// Значение задаём литералом, а не константой repositoryRequeueDelay: сравнение
	// с той же константой, которую подставляет код, прошло бы при любом её значении.
	assert.Equal(t, 30*time.Second, res.RequeueAfter)
	cond := meta.FindStatusCondition(repo.Status.Conditions, "Ready")
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "Error", cond.Reason)
	// Причина отказа должна доехать до пользователя дословно, а не в виде общей фразы.
	assert.Equal(t, "400 Bad Request: invalid recipe", cond.Message)
	assert.EqualValues(t, 1, repo.Status.SyncErrors)
	assertResourceReadySeries(t,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="nexus",type="repository"} 0`,
	)

	res, err = r.updateStatus(context.Background(), repo, true, nil)
	require.NoError(t, err)
	// Успех в очередь не возвращается: ждём события.
	assert.Zero(t, res.RequeueAfter)
	cond = meta.FindStatusCondition(repo.Status.Conditions, "Ready")
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, "Success", cond.Reason)
	assert.EqualValues(t, 0, repo.Status.SyncErrors)
	require.NotNil(t, repo.Status.LastSyncTime)
	assertResourceReadySeries(t,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="nexus",type="repository"} 1`,
	)
}

// После рестарта оператора реестр пуст, а статус в кластере уже актуален, поэтому
// updateStatus уходит в ветку «нет изменений» и в API ничего не пишет. Серия
// обязана восстановиться всё равно — иначе алерт по absent() останется слепым.
func TestUpdateStatusRestoresGaugeWhenStatusUnchanged(t *testing.T) {
	opmetrics.ResourceReady.Reset()
	t.Cleanup(opmetrics.ResourceReady.Reset)

	repo := newMetricRepo()
	r := newMetricReconciler(t, repo)

	_, err := r.updateStatus(context.Background(), repo, true, nil)
	require.NoError(t, err)

	var stored nexusv1alpha1.Repository
	require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(repo), &stored))
	rvBefore := stored.ResourceVersion

	// Имитируем рестарт: процесс новый, значения метрик потеряны.
	opmetrics.ResourceReady.Reset()
	assertResourceReadySeries(t)

	_, err = r.updateStatus(context.Background(), repo, true, nil)
	require.NoError(t, err)

	require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(repo), &stored))
	// Если ResourceVersion не изменился, ветка «нет изменений» действительно сработала,
	// то есть серию восстановил именно вызов до раннего return.
	require.Equal(t, rvBefore, stored.ResourceVersion,
		"статус переписан — тест перестал проверять ветку «нет изменений»")
	assertResourceReadySeries(t,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="nexus",type="repository"} 1`,
	)
}

// AuthPending — это Ready=True: обновление осознанно пропущено, ресурс не сломан.
// Метрика обязана показывать 1, иначе дежурный получит алерт на здоровый CR.
func TestUpdateStatusAuthPendingSetsResourceReady(t *testing.T) {
	opmetrics.ResourceReady.Reset()
	t.Cleanup(opmetrics.ResourceReady.Reset)

	repo := newMetricRepo()
	r := newMetricReconciler(t, repo)

	_, err := r.updateStatusAuthPending(context.Background(), repo)
	require.NoError(t, err)

	cond := meta.FindStatusCondition(repo.Status.Conditions, "Ready")
	require.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, "AuthPending", cond.Reason)
	assertResourceReadySeries(t,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="nexus",type="repository"} 1`,
	)
}

// Удалённый CR не должен оставлять за собой серию: зависшая 0 держала бы алерт
// вечно. Сеть не нужна — ENABLE_REPOSITORY_DELETION выключен, поэтому в Nexus
// не ходим.
func TestFinalizeRepositoryDeletesResourceReady(t *testing.T) {
	t.Setenv("ENABLE_REPOSITORY_DELETION", "false")
	opmetrics.ResourceReady.Reset()
	t.Cleanup(opmetrics.ResourceReady.Reset)

	repo := newMetricRepo()
	r := newMetricReconciler(t, repo)

	_, err := r.updateStatus(context.Background(), repo, false, errors.New("500 Internal Server Error"))
	require.NoError(t, err)
	// Соседний ресурс живёт своей жизнью и должен пережить удаление.
	opmetrics.SetResourceReady("repository", "sandbox", "maven-proxy", true)
	assertResourceReadySeries(t,
		`nexus_operator_resource_ready{name="maven-proxy",resource_namespace="sandbox",type="repository"} 1`,
		`nexus_operator_resource_ready{name="maven-public",resource_namespace="nexus",type="repository"} 0`,
	)

	_, err = r.finalizeRepository(context.Background(), repo, logr.Discard())
	require.NoError(t, err)

	// Финализатор снят — значит путь удаления действительно пройден до конца.
	assert.NotContains(t, repo.Finalizers, repositoryFinalizer)
	assertResourceReadySeries(t,
		`nexus_operator_resource_ready{name="maven-proxy",resource_namespace="sandbox",type="repository"} 1`,
	)
}

// Метрику выставляют девять контроллеров по одному шаблону, а тесты до сих пор
// были только у repository. Забытый вызов SetResourceReady в любом из остальных
// восьми файлов или опечатка в метке type ничем не ловились: ресурс просто
// пропадал бы из алертинга. Поэтому проходим по всем типам одной таблицей.
func TestUpdateStatusSetsResourceReadyInEveryController(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, nexusv1alpha1.AddToScheme(scheme))

	objectMeta := func() metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: "sample", Namespace: "nexus", Generation: 1}
	}
	newClient := func(obj client.Object) client.Client {
		return fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(obj).
			WithStatusSubresource(obj).
			Build()
	}
	// Причина отказа нужна только для ready=false: в успешной ветке её не бывает.
	causeFor := func(ready bool) error {
		if ready {
			return nil
		}
		return errors.New("500 Internal Server Error")
	}

	cases := []struct {
		typeLabel string
		run       func(ready bool) error
	}{
		{"contentselector", func(ready bool) error {
			o := &nexusv1alpha1.ContentSelector{ObjectMeta: objectMeta()}
			r := &ContentSelectorReconciler{Client: newClient(o), Scheme: scheme, Log: logr.Discard()}
			_, err := r.updateStatus(context.Background(), o, ready, causeFor(ready))
			return err
		}},
		{"nexusconfiguration", func(ready bool) error {
			o := &nexusv1alpha1.NexusConfiguration{ObjectMeta: objectMeta()}
			r := &NexusConfigurationReconciler{Client: newClient(o), Scheme: scheme, Log: logr.Discard()}
			_, err := r.updateStatus(context.Background(), o, ready, causeFor(ready))
			return err
		}},
		{"nexusteamaccess", func(ready bool) error {
			o := &nexusv1alpha1.NexusTeamAccess{ObjectMeta: objectMeta()}
			r := &NexusTeamAccessReconciler{Client: newClient(o), Scheme: scheme, Log: logr.Discard()}
			_, err := r.updateStatus(context.Background(), o, ready, causeFor(ready))
			return err
		}},
		{"nexusteambinding", func(ready bool) error {
			o := &nexusv1alpha1.NexusTeamBinding{ObjectMeta: objectMeta()}
			r := &NexusTeamBindingReconciler{Client: newClient(o), Log: logr.Discard()}
			_, err := r.updateStatus(context.Background(), o, ready, causeFor(ready))
			return err
		}},
		{"privilege", func(ready bool) error {
			o := &nexusv1alpha1.Privilege{ObjectMeta: objectMeta()}
			r := &PrivilegeReconciler{Client: newClient(o), Scheme: scheme, Log: logr.Discard()}
			_, err := r.updateStatus(context.Background(), o, ready, causeFor(ready))
			return err
		}},
		{"repository", func(ready bool) error {
			o := &nexusv1alpha1.Repository{ObjectMeta: objectMeta()}
			r := &RepositoryReconciler{Client: newClient(o), Scheme: scheme, Log: logr.Discard()}
			_, err := r.updateStatus(context.Background(), o, ready, causeFor(ready))
			return err
		}},
		{"role", func(ready bool) error {
			o := &nexusv1alpha1.Role{ObjectMeta: objectMeta()}
			r := &RoleReconciler{Client: newClient(o), Scheme: scheme, Log: logr.Discard()}
			_, err := r.updateStatus(context.Background(), o, ready, causeFor(ready))
			return err
		}},
		{"routingrule", func(ready bool) error {
			o := &nexusv1alpha1.RoutingRule{ObjectMeta: objectMeta()}
			r := &RoutingRuleReconciler{Client: newClient(o), Scheme: scheme, Log: logr.Discard()}
			_, err := r.updateStatus(context.Background(), o, ready, causeFor(ready))
			return err
		}},
		{"user", func(ready bool) error {
			o := &nexusv1alpha1.NexusUser{ObjectMeta: objectMeta()}
			r := &NexusUserReconciler{Client: newClient(o), Scheme: scheme, Log: logr.Discard()}
			_, err := r.updateStatus(context.Background(), o, ready, true, causeFor(ready))
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.typeLabel, func(t *testing.T) {
			opmetrics.ResourceReady.Reset()
			t.Cleanup(opmetrics.ResourceReady.Reset)

			require.NoError(t, tc.run(false))
			assertResourceReadySeries(t, fmt.Sprintf(
				`nexus_operator_resource_ready{name="sample",resource_namespace="nexus",type=%q} 0`, tc.typeLabel))

			require.NoError(t, tc.run(true))
			assertResourceReadySeries(t, fmt.Sprintf(
				`nexus_operator_resource_ready{name="sample",resource_namespace="nexus",type=%q} 1`, tc.typeLabel))
		})
	}
}
