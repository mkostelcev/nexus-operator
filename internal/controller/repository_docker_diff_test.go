package controller

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/mkostelcev/nexus-operator/pkg/nexus"
)

// Ответ GET /v1/repositories/docker/proxy/<name> живого Nexus 3.77 (mirror.gcr.io-proxy в stage).
// remoteUrl и сроки кэша Nexus отдаёт в proxy, а в dockerProxy только настройки индекса.
const nexusDockerProxyGET = `{
 "name": "mirror.gcr.io-proxy",
 "url": "https://nexus.example.com/repository/mirror.gcr.io-proxy",
 "online": true,
 "storage": {"blobStoreName": "docker-proxy", "strictContentTypeValidation": true, "writePolicy": "ALLOW"},
 "cleanup": null,
 "docker": {"v1Enabled": false, "forceBasicAuth": false, "httpPort": null, "httpsPort": null, "subdomain": null},
 "dockerProxy": {"indexType": "REGISTRY", "indexUrl": null, "cacheForeignLayers": null, "foreignLayerUrlWhitelist": []},
 "proxy": {"remoteUrl": "https://mirror.gcr.io", "contentMaxAge": -1, "metadataMaxAge": 1440},
 "negativeCache": {"enabled": true, "timeToLive": 1440},
 "httpClient": {"blocked": false, "autoBlock": true,
  "connection": {"retries": 0, "userAgentSuffix": "", "timeout": 60, "enableCircularRedirects": false, "enableCookies": false, "useTrustStore": false},
  "authentication": null},
 "routingRuleName": null,
 "replication": {"preemptivePullEnabled": false, "assetPathRegex": null},
 "format": "docker",
 "type": "proxy"
}`

func dockerProxyCR() nexusv1alpha1.Repository {
	return nexusv1alpha1.Repository{
		Spec: nexusv1alpha1.RepositorySpec{
			Name:   "mirror.gcr.io-proxy",
			Type:   nexus.TypeDockerProxy,
			Online: true,
			Storage: nexusv1alpha1.StorageConfig{
				BlobStoreName:               "docker-proxy",
				StrictContentTypeValidation: true,
				WritePolicy:                 "ALLOW",
			},
			Docker:        &nexusv1alpha1.DockerConfig{},
			Proxy:         &nexusv1alpha1.ProxyConfig{RemoteUrl: "https://mirror.gcr.io", ContentMaxAge: -1, MetadataMaxAge: 1440},
			NegativeCache: &nexusv1alpha1.NegativeCacheConfig{Enabled: true, TimeToLive: 1440},
			HttpClient:    &nexusv1alpha1.HttpClientConfig{AutoBlock: true},
		},
	}
}

func nexusDockerProxyCurrent(t *testing.T) map[string]interface{} {
	t.Helper()
	var current map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(nexusDockerProxyGET), &current))
	return current
}

func TestConfigDiffDockerProxy(t *testing.T) {
	r := &RepositoryReconciler{}

	t.Run("CR совпадает с Nexus → обновлять нечего", func(t *testing.T) {
		desired, err := nexus.BuildRepositoryConfig(dockerProxyCR())
		require.NoError(t, err)
		assert.Empty(t, r.configDiff(desired, nexusDockerProxyCurrent(t)))
	})

	t.Run("в Nexus другой remoteUrl → diff по proxy.remoteUrl", func(t *testing.T) {
		desired, err := nexus.BuildRepositoryConfig(dockerProxyCR())
		require.NoError(t, err)
		current := nexusDockerProxyCurrent(t)
		current["proxy"].(map[string]interface{})["remoteUrl"] = "https://registry-1.docker.io"
		assert.Equal(t, []string{"proxy.remoteUrl"}, r.configDiff(desired, current))
	})

	t.Run("в Nexus другой срок метаданных → diff по proxy.metadataMaxAge", func(t *testing.T) {
		desired, err := nexus.BuildRepositoryConfig(dockerProxyCR())
		require.NoError(t, err)
		current := nexusDockerProxyCurrent(t)
		current["proxy"].(map[string]interface{})["metadataMaxAge"] = float64(60)
		assert.Equal(t, []string{"proxy.metadataMaxAge"}, r.configDiff(desired, current))
	})
}
