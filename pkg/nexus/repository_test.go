package nexus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkostelcev/nexus-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 1. repoTypeToAPIPath
// ---------------------------------------------------------------------------

func TestRepoTypeToAPIPath(t *testing.T) {
	tests := []struct {
		name      string
		repoType  string
		wantPath  string
		wantError error
	}{
		{
			name:      "maven-hosted",
			repoType:  "maven-hosted",
			wantPath:  "/service/rest/v1/repositories/maven/hosted",
			wantError: nil,
		},
		{
			name:      "docker-proxy",
			repoType:  "docker-proxy",
			wantPath:  "/service/rest/v1/repositories/docker/proxy",
			wantError: nil,
		},
		{
			name:      "npm-group",
			repoType:  "npm-group",
			wantPath:  "/service/rest/v1/repositories/npm/group",
			wantError: nil,
		},
		{
			name:      "invalid no dash",
			repoType:  "invalid",
			wantPath:  "",
			wantError: ErrUnsupportedRepoType,
		},
		{
			name:      "empty string",
			repoType:  "",
			wantPath:  "",
			wantError: ErrUnsupportedRepoType,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repoTypeToAPIPath(tc.repoType)
			if tc.wantError != nil {
				assert.ErrorIs(t, err, tc.wantError)
				assert.Equal(t, "", got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantPath, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. CreateRepository
// ---------------------------------------------------------------------------

func TestCreateRepository(t *testing.T) {
	t.Run("success 201", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/maven/hosted", func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusCreated)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		err := client.CreateRepository(context.Background(), "maven-hosted", map[string]interface{}{
			"name":   "test-repo",
			"online": true,
		})
		require.NoError(t, err)
	})

	t.Run("failure 400", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/maven/hosted", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("bad request"))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		err := client.CreateRepository(context.Background(), "maven-hosted", map[string]interface{}{
			"name": "bad-repo",
		})
		assert.ErrorIs(t, err, ErrUnexpectedResponse)
	})

	t.Run("invalid repo type", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		defer server.Close()

		client := newTestClient(t, server)
		err := client.CreateRepository(context.Background(), "invalid", map[string]interface{}{})
		assert.ErrorIs(t, err, ErrUnsupportedRepoType)
	})
}

// ---------------------------------------------------------------------------
// 3. UpdateRepository
// ---------------------------------------------------------------------------

func TestUpdateRepository(t *testing.T) {
	t.Run("success 200", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/maven/hosted/my-repo", func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPut, r.Method)
			w.WriteHeader(http.StatusOK)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		err := client.UpdateRepository(context.Background(), "maven-hosted", "my-repo", map[string]interface{}{
			"name":   "my-repo",
			"online": true,
		})
		require.NoError(t, err)
	})

	t.Run("success 204", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/npm/proxy/npm-proxy-repo",
			func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				w.WriteHeader(http.StatusNoContent)
			})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		err := client.UpdateRepository(context.Background(), "npm-proxy", "npm-proxy-repo", map[string]interface{}{
			"name": "npm-proxy-repo",
		})
		require.NoError(t, err)
	})

	t.Run("failure 500", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/maven/hosted/my-repo", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		err := client.UpdateRepository(context.Background(), "maven-hosted", "my-repo", map[string]interface{}{})
		assert.ErrorIs(t, err, ErrUnexpectedResponse)
	})

	t.Run("invalid repo type", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		defer server.Close()

		client := newTestClient(t, server)
		err := client.UpdateRepository(context.Background(), "notype", "x", map[string]interface{}{})
		assert.ErrorIs(t, err, ErrUnsupportedRepoType)
	})
}

// ---------------------------------------------------------------------------
// 4. GetRepository
// ---------------------------------------------------------------------------

func TestGetRepository(t *testing.T) {
	t.Run("success 200", func(t *testing.T) {
		expected := map[string]interface{}{
			"name":   "my-repo",
			"format": "maven2",
			"type":   "hosted",
			"online": true,
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/my-repo", func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(expected)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		result, err := client.GetRepository(context.Background(), "my-repo")
		require.NoError(t, err)
		assert.Equal(t, "my-repo", result["name"])
		assert.Equal(t, "maven2", result["format"])
	})

	t.Run("not found 404", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/missing", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		result, err := client.GetRepository(context.Background(), "missing")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrRepositoryNotFound)
	})
}

// ---------------------------------------------------------------------------
// 5. DeleteRepository
// ---------------------------------------------------------------------------

func TestDeleteRepository(t *testing.T) {
	t.Run("success 204", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/del-repo", func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			w.WriteHeader(http.StatusNoContent)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		err := client.DeleteRepository(context.Background(), "del-repo")
		require.NoError(t, err)
	})

	t.Run("not found 404", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/missing", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		err := client.DeleteRepository(context.Background(), "missing")
		assert.ErrorIs(t, err, ErrRepositoryNotFound)
	})

	t.Run("error 500", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/err-repo", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		err := client.DeleteRepository(context.Background(), "err-repo")
		assert.ErrorIs(t, err, ErrUnexpectedResponse)
	})
}

// ---------------------------------------------------------------------------
// 6. ListRepositories
// ---------------------------------------------------------------------------

func TestListRepositories(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		items := []RepositoryListItem{
			{Name: "repo-a", Format: "maven2", Type: "hosted", URL: "http://nexus/repo-a"},
			{Name: "repo-b", Format: "npm", Type: "proxy", URL: "http://nexus/repo-b"},
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories", func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(items)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		result, err := client.ListRepositories(context.Background())
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, "repo-a", result[0].Name)
		assert.Equal(t, "maven2", result[0].Format)
		assert.Equal(t, "hosted", result[0].Type)
		assert.Equal(t, "repo-b", result[1].Name)
		assert.Equal(t, "npm", result[1].Format)
	})

	t.Run("error 500", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server error"))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		result, err := client.ListRepositories(context.Background())
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrUnexpectedResponse)
	})
}

// ---------------------------------------------------------------------------
// 7. GetRepositoryByType
// ---------------------------------------------------------------------------

func TestGetRepositoryByType(t *testing.T) {
	t.Run("success with maven2 format conversion", func(t *testing.T) {
		expected := map[string]interface{}{
			"name":   "maven-central",
			"online": true,
		}
		mux := http.NewServeMux()
		// maven2 format should be converted to "maven" in the endpoint path
		mux.HandleFunc("/service/rest/v1/repositories/maven/proxy/maven-central",
			func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(expected)
			})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		result, err := client.GetRepositoryByType(context.Background(), "maven2", "proxy", "maven-central")
		require.NoError(t, err)
		assert.Equal(t, "maven-central", result["name"])
	})

	t.Run("success with non-maven format", func(t *testing.T) {
		expected := map[string]interface{}{
			"name":   "npm-hosted-repo",
			"online": true,
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/npm/hosted/npm-hosted-repo",
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(expected)
			})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		result, err := client.GetRepositoryByType(context.Background(), "npm", "hosted", "npm-hosted-repo")
		require.NoError(t, err)
		assert.Equal(t, "npm-hosted-repo", result["name"])
	})

	t.Run("not found 404", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/maven/hosted/missing", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		result, err := client.GetRepositoryByType(context.Background(), "maven2", "hosted", "missing")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrRepositoryNotFound)
	})

	t.Run("error 500", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/docker/proxy/docker-proxy-repo",
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("server error"))
			})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		result, err := client.GetRepositoryByType(context.Background(), "docker", "proxy", "docker-proxy-repo")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, ErrUnexpectedResponse)
	})
}

// ---------------------------------------------------------------------------
// 8. RepositoryExists
// ---------------------------------------------------------------------------

func TestRepositoryExists(t *testing.T) {
	t.Run("exists 200", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/existing-repo", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"existing-repo"}`))
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		exists, err := client.RepositoryExists(context.Background(), "existing-repo")
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("not found 404", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/missing-repo", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		exists, err := client.RepositoryExists(context.Background(), "missing-repo")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("error 500", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/service/rest/v1/repositories/err-repo", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		server := httptest.NewServer(mux)
		defer server.Close()

		client := newTestClient(t, server)
		exists, err := client.RepositoryExists(context.Background(), "err-repo")
		assert.False(t, exists)
		assert.ErrorIs(t, err, ErrUnexpectedResponse)
	})
}

// ---------------------------------------------------------------------------
// 9. BuildRepositoryConfig
// ---------------------------------------------------------------------------

func TestBuildRepositoryConfig(t *testing.T) {
	t.Run("maven-hosted", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "maven-releases",
				Type:   TypeMavenHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName:               "default",
					StrictContentTypeValidation: true,
					WritePolicy:                 "ALLOW_ONCE",
				},
				Maven: &v1alpha1.MavenConfig{
					VersionPolicy:      "RELEASE",
					LayoutPolicy:       "STRICT",
					ContentDisposition: "INLINE",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.Equal(t, "maven-releases", config["name"])
		assert.Equal(t, true, config["online"])
		assert.NotNil(t, config["storage"])
		assert.NotNil(t, config["maven"])

		maven, ok := config["maven"].(*v1alpha1.MavenConfig)
		require.True(t, ok)
		assert.Equal(t, "RELEASE", maven.VersionPolicy)
		assert.Equal(t, "STRICT", maven.LayoutPolicy)
	})

	t.Run("maven-proxy", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "maven-central",
				Type:   TypeMavenProxy,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Maven: &v1alpha1.MavenConfig{
					VersionPolicy: "RELEASE",
					LayoutPolicy:  "STRICT",
				},
				Proxy: &v1alpha1.ProxyConfig{
					RemoteUrl:      "https://repo1.maven.org/maven2/",
					ContentMaxAge:  -1,
					MetadataMaxAge: 1440,
				},
				HttpClient: &v1alpha1.HttpClientConfig{
					AutoBlock: true,
				},
				NegativeCache: &v1alpha1.NegativeCacheConfig{
					Enabled:    true,
					TimeToLive: 1440,
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.Equal(t, "maven-central", config["name"])
		assert.NotNil(t, config["proxy"])
		assert.NotNil(t, config["maven"])
		assert.NotNil(t, config["httpClient"])
		assert.NotNil(t, config["negativeCache"])

		proxy, ok := config["proxy"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "https://repo1.maven.org/maven2/", proxy["remoteUrl"])
	})

	t.Run("httpClient with connection is serialized", func(t *testing.T) {
		retries := 3
		timeout := 120
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "proxy-with-conn",
				Type:   TypeMavenProxy,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Maven: &v1alpha1.MavenConfig{
					VersionPolicy: "RELEASE",
					LayoutPolicy:  "STRICT",
				},
				Proxy: &v1alpha1.ProxyConfig{
					RemoteUrl: "https://repo1.maven.org/maven2/",
				},
				HttpClient: &v1alpha1.HttpClientConfig{
					AutoBlock: true,
					Connection: &v1alpha1.ConnectionConfig{
						Retries:                 &retries,
						Timeout:                 &timeout,
						UserAgentSuffix:         "test-agent",
						EnableCircularRedirects: true,
						EnableCookies:           true,
						UseTrustStore:           false,
					},
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)

		hc, ok := config["httpClient"].(map[string]interface{})
		require.True(t, ok)
		conn, ok := hc["connection"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, 3, conn["retries"])
		assert.Equal(t, 120, conn["timeout"])
		assert.Equal(t, "test-agent", conn["userAgentSuffix"])
		assert.Equal(t, true, conn["enableCircularRedirects"])
		assert.Equal(t, true, conn["enableCookies"])
		assert.Equal(t, false, conn["useTrustStore"])
	})

	t.Run("httpClient without connection sends defaults", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "proxy-no-conn",
				Type:   TypeMavenProxy,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Maven: &v1alpha1.MavenConfig{
					VersionPolicy: "RELEASE",
					LayoutPolicy:  "STRICT",
				},
				Proxy: &v1alpha1.ProxyConfig{
					RemoteUrl: "https://repo1.maven.org/maven2/",
				},
				HttpClient: &v1alpha1.HttpClientConfig{
					AutoBlock: true,
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)

		hc, ok := config["httpClient"].(map[string]interface{})
		require.True(t, ok)
		conn, ok := hc["connection"].(map[string]interface{})
		require.True(t, ok, "connection should always be present with defaults")
		assert.Equal(t, 0, conn["retries"])
		assert.Equal(t, 60, conn["timeout"])
		assert.Equal(t, "", conn["userAgentSuffix"])
		assert.Equal(t, false, conn["enableCircularRedirects"])
		assert.Equal(t, false, conn["enableCookies"])
		assert.Equal(t, false, conn["useTrustStore"])
	})

	t.Run("docker-hosted with ports", func(t *testing.T) {
		httpPort := 8082
		httpsPort := 8083
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "docker-local",
				Type:   TypeDockerHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName:               "docker-store",
					StrictContentTypeValidation: true,
					WritePolicy:                 "ALLOW",
				},
				Docker: &v1alpha1.DockerConfig{
					HttpPort:       &httpPort,
					HttpsPort:      &httpsPort,
					ForceBasicAuth: true,
					V1Enabled:      false,
					Subdomain:      "docker",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.Equal(t, "docker-local", config["name"])
		assert.NotNil(t, config["docker"])

		docker, ok := config["docker"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, 8082, docker["httpPort"])
		assert.Equal(t, 8083, docker["httpsPort"])
		assert.Equal(t, true, docker["forceBasicAuth"])
		assert.Equal(t, "docker", docker["subdomain"])
	})

	t.Run("npm-group with writePolicy and group", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "npm-all",
				Type:   TypeNpmGroup,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName:               "default",
					StrictContentTypeValidation: true,
					WritePolicy:                 "ALLOW",
				},
				Group: &v1alpha1.GroupConfig{
					MemberNames: []string{"npm-hosted", "npm-proxy"},
				},
				Npm: &v1alpha1.NpmConfig{
					RemoveNonCataloged: true,
					RemoveQuarantined:  false,
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.Equal(t, "npm-all", config["name"])

		// npm-group overrides storage with writePolicy
		storage, ok := config["storage"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "ALLOW", storage["writePolicy"])
		assert.Equal(t, "default", storage["blobStoreName"])

		assert.NotNil(t, config["group"])
		assert.NotNil(t, config["npm"])
	})

	t.Run("raw-hosted storage fields", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "raw-local",
				Type:   TypeRawHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName:               "raw-store",
					StrictContentTypeValidation: false,
					WritePolicy:                 "ALLOW",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.Equal(t, "raw-local", config["name"])

		storage, ok := config["storage"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "raw-store", storage["blobStoreName"])
		assert.Equal(t, false, storage["strictContentTypeValidation"])
		assert.Equal(t, "ALLOW", storage["writePolicy"])
	})

	t.Run("unsupported type", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "unknown-repo",
				Type:   "unknown-type",
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		assert.Nil(t, config)
		assert.ErrorIs(t, err, ErrUnsupportedRepoType)
	})

	t.Run("docker-proxy", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "docker-hub",
				Type:   TypeDockerProxy,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Docker: &v1alpha1.DockerConfig{
					ForceBasicAuth: true,
					V1Enabled:      false,
				},
				Proxy: &v1alpha1.ProxyConfig{
					RemoteUrl:      "https://registry-1.docker.io",
					ContentMaxAge:  1440,
					MetadataMaxAge: 1440,
				},
				HttpClient: &v1alpha1.HttpClientConfig{
					AutoBlock: true,
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.NotNil(t, config["docker"])
		assert.Equal(t, map[string]interface{}{"indexType": "REGISTRY"}, config["dockerProxy"])
		assert.Equal(t, "https://registry-1.docker.io", config["proxy"].(map[string]interface{})["remoteUrl"])
		assert.NotNil(t, config["httpClient"])
	})

	t.Run("apt-hosted with signing", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "apt-local",
				Type:   TypeAptHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Apt: &v1alpha1.AptConfig{
					Distribution: "bionic",
				},
				AptSigning: &v1alpha1.AptSigningConfig{
					Keypair:    "test-keypair",
					Passphrase: "test-passphrase",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.NotNil(t, config["apt"])
		assert.NotNil(t, config["aptSigning"])
	})

	t.Run("apt-hosted without apt config defaults to empty map", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "apt-no-config",
				Type:   TypeAptHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.NotNil(t, config["apt"])
		assert.NotNil(t, config["aptSigning"])
	})

	t.Run("yum-hosted with yum config", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "yum-local",
				Type:   TypeYumHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Yum: &v1alpha1.YumConfig{
					RepodataDepth: 2,
					DeployPolicy:  "STRICT",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.NotNil(t, config["yum"])
	})

	t.Run("nuget-proxy", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "nuget-org",
				Type:   TypeNugetProxy,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Proxy: &v1alpha1.ProxyConfig{
					RemoteUrl: "https://api.nuget.org/v3/index.json",
				},
				NugetProxy: &v1alpha1.NugetProxyConfig{
					QueryCacheItemMaxAge: 3600,
					NugetVersion:         "V3",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.NotNil(t, config["nugetProxy"])
	})

	t.Run("cargo-proxy", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "crates-io",
				Type:   TypeCargoProxy,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Proxy: &v1alpha1.ProxyConfig{
					RemoteUrl: "https://crates.io",
				},
				Cargo: &v1alpha1.CargoConfig{
					RequireAuthentication: false,
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.NotNil(t, config["cargo"])
	})

	t.Run("conan-proxy", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "conan-center",
				Type:   TypeConanProxy,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Proxy: &v1alpha1.ProxyConfig{
					RemoteUrl: "https://center.conan.io",
				},
				ConanProxy: &v1alpha1.ConanProxyConfig{
					ConanVersion: "V1",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.NotNil(t, config["conanProxy"])
	})

	t.Run("helm-hosted no format-specific config", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "helm-local",
				Type:   TypeHelmHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
					WritePolicy:   "ALLOW",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.Equal(t, "helm-local", config["name"])
		// No format-specific keys should be added
		assert.Nil(t, config["maven"])
		assert.Nil(t, config["docker"])
	})

	t.Run("cleanup policies are included", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "docker-with-cleanup",
				Type:   TypeDockerHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName:               "default",
					StrictContentTypeValidation: true,
					WritePolicy:                 "ALLOW",
				},
				Docker: &v1alpha1.DockerConfig{
					ForceBasicAuth: true,
					V1Enabled:      false,
				},
				Cleanup: &v1alpha1.CleanupPolicy{
					PolicyNames: []string{"weekly-cleanup", "delete-old-snapshots"},
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.Equal(t, "docker-with-cleanup", config["name"])

		cleanup, ok := config["cleanup"].(*v1alpha1.CleanupPolicy)
		require.True(t, ok, "cleanup should be *v1alpha1.CleanupPolicy")
		assert.Equal(t, []string{"weekly-cleanup", "delete-old-snapshots"}, cleanup.PolicyNames)
	})

	t.Run("cleanup with empty policyNames", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "maven-empty-cleanup",
				Type:   TypeMavenHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
					WritePolicy:   "ALLOW",
				},
				Maven: &v1alpha1.MavenConfig{
					VersionPolicy: "RELEASE",
					LayoutPolicy:  "STRICT",
				},
				Cleanup: &v1alpha1.CleanupPolicy{
					PolicyNames: []string{},
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)

		cleanup, ok := config["cleanup"].(*v1alpha1.CleanupPolicy)
		require.True(t, ok, "cleanup should be present even with empty policyNames")
		assert.Empty(t, cleanup.PolicyNames)
	})

	t.Run("no cleanup when nil", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "helm-no-cleanup",
				Type:   TypeHelmHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
					WritePolicy:   "ALLOW",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.Nil(t, config["cleanup"], "cleanup should not be in config when spec.Cleanup is nil")
	})

	t.Run("raw-group with memberNames", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "raw-all",
				Type:   TypeRawGroup,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Group: &v1alpha1.GroupConfig{
					MemberNames: []string{"raw-hosted", "raw-proxy"},
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		group, ok := config["group"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, []string{"raw-hosted", "raw-proxy"}, group["memberNames"])
	})

	t.Run("routingRuleName set", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "maven-central",
				Type:   TypeMavenProxy,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Maven: &v1alpha1.MavenConfig{
					VersionPolicy: "RELEASE",
					LayoutPolicy:  "STRICT",
				},
				Proxy: &v1alpha1.ProxyConfig{
					RemoteUrl: "https://repo1.maven.org/maven2/",
				},
				RoutingRuleName: "block-com-example",
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		assert.Equal(t, "block-com-example", config["routingRule"])
	})

	t.Run("routingRule empty → not in config", func(t *testing.T) {
		repo := v1alpha1.Repository{
			Spec: v1alpha1.RepositorySpec{
				Name:   "maven-releases",
				Type:   TypeMavenHosted,
				Online: true,
				Storage: v1alpha1.StorageConfig{
					BlobStoreName: "default",
				},
				Maven: &v1alpha1.MavenConfig{
					VersionPolicy: "RELEASE",
					LayoutPolicy:  "STRICT",
				},
			},
		}

		config, err := BuildRepositoryConfig(repo)
		require.NoError(t, err)
		_, exists := config["routingRule"]
		assert.False(t, exists, "routingRule should not be present when empty")
	})
}

// ---------------------------------------------------------------------------
// 10. BuildRepositorySpecFromConfig
// ---------------------------------------------------------------------------

func TestBuildRepositorySpecFromConfig(t *testing.T) {
	t.Run("full config", func(t *testing.T) {
		httpPort := float64(8082)
		httpsPort := float64(8083)
		config := map[string]interface{}{
			"name":   "full-repo",
			"online": true,
			"storage": map[string]interface{}{
				"blobStoreName":               "default",
				"strictContentTypeValidation": true,
				"writePolicy":                 "ALLOW_ONCE",
			},
			"maven": map[string]interface{}{
				"versionPolicy":      "RELEASE",
				"layoutPolicy":       "STRICT",
				"contentDisposition": "INLINE",
			},
			"proxy": map[string]interface{}{
				"remoteUrl":      "https://repo1.maven.org/maven2/",
				"contentMaxAge":  float64(-1),
				"metadataMaxAge": float64(1440),
			},
			"group": map[string]interface{}{
				"memberNames": []interface{}{"member-a", "member-b"},
			},
			"httpClient": map[string]interface{}{
				"blocked":   false,
				"autoBlock": true,
				"connection": map[string]interface{}{
					"retries":                 float64(3),
					"timeout":                 float64(120),
					"userAgentSuffix":         "my-agent",
					"enableCircularRedirects": true,
					"enableCookies":           true,
					"useTrustStore":           false,
				},
				"authentication": map[string]interface{}{
					"type":     "username",
					"username": "user",
					"password": "pass",
				},
			},
			"negativeCache": map[string]interface{}{
				"enabled":    true,
				"timeToLive": float64(1440),
			},
			"cleanup": map[string]interface{}{
				"policyNames": []interface{}{"weekly-cleanup"},
			},
			"docker": map[string]interface{}{
				"httpPort":       httpPort,
				"httpsPort":      httpsPort,
				"forceBasicAuth": true,
				"v1Enabled":      false,
				"subdomain":      "docker",
			},
			"apt": map[string]interface{}{
				"distribution": "bionic",
				"flat":         false,
			},
			"aptSigning": map[string]interface{}{
				"keypair":    "my-keypair",
				"passphrase": "my-passphrase",
			},
			"yum": map[string]interface{}{
				"repodataDepth": float64(2),
				"deployPolicy":  "STRICT",
			},
			"yumSigning": map[string]interface{}{
				"keypair":    "yum-key",
				"passphrase": "yum-pass",
			},
			"nugetProxy": map[string]interface{}{
				"queryCacheItemMaxAge": float64(3600),
				"nugetVersion":         "V3",
			},
			"cargo": map[string]interface{}{
				"requireAuthentication": true,
			},
			"conanProxy": map[string]interface{}{
				"conanVersion": "V1",
			},
			"npm": map[string]interface{}{
				"removeNonCataloged": true,
				"removeQuarantined":  false,
			},
			"routingRuleName": "block-com-example",
		}

		spec := BuildRepositorySpecFromConfig(config, "maven-proxy")

		// Basic fields
		assert.Equal(t, "full-repo", spec.Name)
		assert.Equal(t, "maven-proxy", spec.Type)
		assert.True(t, spec.Online)

		// Storage
		assert.Equal(t, "default", spec.Storage.BlobStoreName)
		assert.True(t, spec.Storage.StrictContentTypeValidation)
		assert.Equal(t, "ALLOW_ONCE", spec.Storage.WritePolicy)

		// Maven
		require.NotNil(t, spec.Maven)
		assert.Equal(t, "RELEASE", spec.Maven.VersionPolicy)
		assert.Equal(t, "STRICT", spec.Maven.LayoutPolicy)
		assert.Equal(t, "INLINE", spec.Maven.ContentDisposition)

		// Proxy
		require.NotNil(t, spec.Proxy)
		assert.Equal(t, "https://repo1.maven.org/maven2/", spec.Proxy.RemoteUrl)
		assert.Equal(t, -1, spec.Proxy.ContentMaxAge)
		assert.Equal(t, 1440, spec.Proxy.MetadataMaxAge)

		// Group
		require.NotNil(t, spec.Group)
		assert.Equal(t, []string{"member-a", "member-b"}, spec.Group.MemberNames)

		// HttpClient
		require.NotNil(t, spec.HttpClient)
		assert.False(t, spec.HttpClient.Blocked)
		assert.True(t, spec.HttpClient.AutoBlock)
		require.NotNil(t, spec.HttpClient.Authentication)
		assert.Equal(t, "username", spec.HttpClient.Authentication.Type)
		assert.Equal(t, "user", spec.HttpClient.Authentication.Username)
		assert.Equal(t, "pass", spec.HttpClient.Authentication.Password)
		require.NotNil(t, spec.HttpClient.Connection)
		require.NotNil(t, spec.HttpClient.Connection.Retries)
		assert.Equal(t, 3, *spec.HttpClient.Connection.Retries)
		require.NotNil(t, spec.HttpClient.Connection.Timeout)
		assert.Equal(t, 120, *spec.HttpClient.Connection.Timeout)
		assert.Equal(t, "my-agent", spec.HttpClient.Connection.UserAgentSuffix)
		assert.True(t, spec.HttpClient.Connection.EnableCircularRedirects)
		assert.True(t, spec.HttpClient.Connection.EnableCookies)
		assert.False(t, spec.HttpClient.Connection.UseTrustStore)

		// NegativeCache
		require.NotNil(t, spec.NegativeCache)
		assert.True(t, spec.NegativeCache.Enabled)
		assert.Equal(t, 1440, spec.NegativeCache.TimeToLive)

		// Cleanup
		require.NotNil(t, spec.Cleanup)
		assert.Equal(t, []string{"weekly-cleanup"}, spec.Cleanup.PolicyNames)

		// Docker
		require.NotNil(t, spec.Docker)
		assert.True(t, spec.Docker.ForceBasicAuth)
		assert.False(t, spec.Docker.V1Enabled)
		assert.Equal(t, "docker", spec.Docker.Subdomain)
		require.NotNil(t, spec.Docker.HttpPort)
		assert.Equal(t, 8082, *spec.Docker.HttpPort)
		require.NotNil(t, spec.Docker.HttpsPort)
		assert.Equal(t, 8083, *spec.Docker.HttpsPort)

		// Apt
		require.NotNil(t, spec.Apt)
		assert.Equal(t, "bionic", spec.Apt.Distribution)
		assert.False(t, spec.Apt.Flat)

		// AptSigning
		require.NotNil(t, spec.AptSigning)
		assert.Equal(t, "my-keypair", spec.AptSigning.Keypair)
		assert.Equal(t, "my-passphrase", spec.AptSigning.Passphrase)

		// Yum
		require.NotNil(t, spec.Yum)
		assert.Equal(t, 2, spec.Yum.RepodataDepth)
		assert.Equal(t, "STRICT", spec.Yum.DeployPolicy)

		// YumSigning
		require.NotNil(t, spec.YumSigning)
		assert.Equal(t, "yum-key", spec.YumSigning.Keypair)
		assert.Equal(t, "yum-pass", spec.YumSigning.Passphrase)

		// NugetProxy
		require.NotNil(t, spec.NugetProxy)
		assert.Equal(t, 3600, spec.NugetProxy.QueryCacheItemMaxAge)
		assert.Equal(t, "V3", spec.NugetProxy.NugetVersion)

		// Cargo
		require.NotNil(t, spec.Cargo)
		assert.True(t, spec.Cargo.RequireAuthentication)

		// ConanProxy
		require.NotNil(t, spec.ConanProxy)
		assert.Equal(t, "V1", spec.ConanProxy.ConanVersion)

		// Npm
		require.NotNil(t, spec.Npm)
		assert.True(t, spec.Npm.RemoveNonCataloged)
		assert.False(t, spec.Npm.RemoveQuarantined)

		// RoutingRuleName
		assert.Equal(t, "block-com-example", spec.RoutingRuleName)
	})

	t.Run("routingRuleName empty string → not set", func(t *testing.T) {
		config := map[string]interface{}{
			"name":            "repo-no-rule",
			"online":          true,
			"routingRuleName": "",
		}

		spec := BuildRepositorySpecFromConfig(config, "maven-hosted")
		assert.Equal(t, "", spec.RoutingRuleName)
	})

	t.Run("routingRuleName null → not set", func(t *testing.T) {
		config := map[string]interface{}{
			"name":            "repo-null-rule",
			"online":          true,
			"routingRuleName": nil,
		}

		spec := BuildRepositorySpecFromConfig(config, "maven-hosted")
		assert.Equal(t, "", spec.RoutingRuleName)
	})

	t.Run("minimal config", func(t *testing.T) {
		config := map[string]interface{}{
			"name":   "minimal-repo",
			"online": false,
		}

		spec := BuildRepositorySpecFromConfig(config, "raw-hosted")

		assert.Equal(t, "minimal-repo", spec.Name)
		assert.Equal(t, "raw-hosted", spec.Type)
		assert.False(t, spec.Online)

		// All optional sections should be nil
		assert.Nil(t, spec.Maven)
		assert.Nil(t, spec.Npm)
		assert.Nil(t, spec.Docker)
		assert.Nil(t, spec.Proxy)
		assert.Nil(t, spec.Group)
		assert.Nil(t, spec.Cleanup)
		assert.Nil(t, spec.HttpClient)
		assert.Nil(t, spec.NegativeCache)
		assert.Nil(t, spec.Apt)
		assert.Nil(t, spec.AptSigning)
		assert.Nil(t, spec.Yum)
		assert.Nil(t, spec.YumSigning)
		assert.Nil(t, spec.NugetProxy)
		assert.Nil(t, spec.Cargo)
		assert.Nil(t, spec.ConanProxy)
		assert.Equal(t, "", spec.RoutingRuleName)
	})

	t.Run("config with empty/missing fields", func(t *testing.T) {
		config := map[string]interface{}{
			"name":   "empty-fields",
			"online": true,
			"storage": map[string]interface{}{
				"blobStoreName": "default",
				// writePolicy missing -> should default to "ALLOW"
			},
			"httpClient": map[string]interface{}{
				// no authentication section
				"blocked": true,
			},
			"group": map[string]interface{}{
				// memberNames missing
			},
		}

		spec := BuildRepositorySpecFromConfig(config, "maven-group")

		assert.Equal(t, "empty-fields", spec.Name)
		assert.True(t, spec.Online)

		// Storage: writePolicy defaults to "ALLOW"
		assert.Equal(t, "ALLOW", spec.Storage.WritePolicy)
		assert.Equal(t, "default", spec.Storage.BlobStoreName)

		// HttpClient without authentication
		require.NotNil(t, spec.HttpClient)
		assert.True(t, spec.HttpClient.Blocked)
		assert.Nil(t, spec.HttpClient.Authentication)

		// Group with nil memberNames
		require.NotNil(t, spec.Group)
		assert.Nil(t, spec.Group.MemberNames)
	})

	t.Run("docker with zero ports not set", func(t *testing.T) {
		config := map[string]interface{}{
			"name":   "docker-no-ports",
			"online": true,
			"docker": map[string]interface{}{
				"httpPort":       float64(0),
				"httpsPort":      float64(0),
				"forceBasicAuth": false,
				"v1Enabled":      false,
			},
		}

		spec := BuildRepositorySpecFromConfig(config, "docker-hosted")

		require.NotNil(t, spec.Docker)
		assert.Nil(t, spec.Docker.HttpPort, "zero httpPort should not be set")
		assert.Nil(t, spec.Docker.HttpsPort, "zero httpsPort should not be set")
	})

	t.Run("httpClient with connection", func(t *testing.T) {
		config := map[string]interface{}{
			"name":   "proxy-with-conn",
			"online": true,
			"httpClient": map[string]interface{}{
				"blocked":   false,
				"autoBlock": true,
				"connection": map[string]interface{}{
					"retries":                 float64(5),
					"timeout":                 float64(60),
					"userAgentSuffix":         "test-suffix",
					"enableCircularRedirects": true,
					"enableCookies":           false,
					"useTrustStore":           true,
				},
			},
		}

		spec := BuildRepositorySpecFromConfig(config, "maven-proxy")

		require.NotNil(t, spec.HttpClient)
		require.NotNil(t, spec.HttpClient.Connection)
		require.NotNil(t, spec.HttpClient.Connection.Retries)
		assert.Equal(t, 5, *spec.HttpClient.Connection.Retries)
		require.NotNil(t, spec.HttpClient.Connection.Timeout)
		assert.Equal(t, 60, *spec.HttpClient.Connection.Timeout)
		assert.Equal(t, "test-suffix", spec.HttpClient.Connection.UserAgentSuffix)
		assert.True(t, spec.HttpClient.Connection.EnableCircularRedirects)
		assert.False(t, spec.HttpClient.Connection.EnableCookies)
		assert.True(t, spec.HttpClient.Connection.UseTrustStore)
		assert.Nil(t, spec.HttpClient.Authentication)
	})

	t.Run("httpClient without connection", func(t *testing.T) {
		config := map[string]interface{}{
			"name":   "proxy-no-conn",
			"online": true,
			"httpClient": map[string]interface{}{
				"blocked":   false,
				"autoBlock": true,
			},
		}

		spec := BuildRepositorySpecFromConfig(config, "maven-proxy")

		require.NotNil(t, spec.HttpClient)
		assert.Nil(t, spec.HttpClient.Connection)
	})
}

// ---------------------------------------------------------------------------
// 11. ConfigHasAuthentication
// ---------------------------------------------------------------------------

func TestConfigHasAuthentication(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]interface{}
		want   bool
	}{
		{
			name: "has httpClient.authentication.type",
			config: map[string]interface{}{
				"httpClient": map[string]interface{}{
					"authentication": map[string]interface{}{
						"type":     "username",
						"username": "user",
					},
				},
			},
			want: true,
		},
		{
			name: "has httpClient but no authentication",
			config: map[string]interface{}{
				"httpClient": map[string]interface{}{
					"blocked":   false,
					"autoBlock": true,
				},
			},
			want: false,
		},
		{
			name:   "no httpClient",
			config: map[string]interface{}{},
			want:   false,
		},
		{
			name: "has authentication but type is empty",
			config: map[string]interface{}{
				"httpClient": map[string]interface{}{
					"authentication": map[string]interface{}{
						"type": "",
					},
				},
			},
			want: false,
		},
		{
			name: "has authentication but type is missing",
			config: map[string]interface{}{
				"httpClient": map[string]interface{}{
					"authentication": map[string]interface{}{
						"username": "user",
					},
				},
			},
			want: false,
		},
		{
			name: "bearerToken type",
			config: map[string]interface{}{
				"httpClient": map[string]interface{}{
					"authentication": map[string]interface{}{
						"type":        "bearerToken",
						"bearerToken": "abc123",
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ConfigHasAuthentication(tc.config)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// 12. Helper functions: getStringField, getBoolField, getIntField, getIntFieldPtr
// ---------------------------------------------------------------------------

func TestGetStringField(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]interface{}
		key  string
		want string
	}{
		{
			name: "existing string",
			m:    map[string]interface{}{"key": "value"},
			key:  "key",
			want: "value",
		},
		{
			name: "missing key",
			m:    map[string]interface{}{},
			key:  "missing",
			want: "",
		},
		{
			name: "non-string value",
			m:    map[string]interface{}{"key": 123},
			key:  "key",
			want: "",
		},
		{
			name: "nil value",
			m:    map[string]interface{}{"key": nil},
			key:  "key",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getStringField(tc.m, tc.key)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetBoolField(t *testing.T) {
	tests := []struct {
		name       string
		m          map[string]interface{}
		key        string
		defaultVal bool
		want       bool
	}{
		{
			name:       "existing true",
			m:          map[string]interface{}{"key": true},
			key:        "key",
			defaultVal: false,
			want:       true,
		},
		{
			name:       "existing false",
			m:          map[string]interface{}{"key": false},
			key:        "key",
			defaultVal: true,
			want:       false,
		},
		{
			name:       "missing key returns default true",
			m:          map[string]interface{}{},
			key:        "missing",
			defaultVal: true,
			want:       true,
		},
		{
			name:       "missing key returns default false",
			m:          map[string]interface{}{},
			key:        "missing",
			defaultVal: false,
			want:       false,
		},
		{
			name:       "non-bool value returns default",
			m:          map[string]interface{}{"key": "true"},
			key:        "key",
			defaultVal: false,
			want:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getBoolField(tc.m, tc.key, tc.defaultVal)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetIntField(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]interface{}
		key  string
		want int
	}{
		{
			name: "float64 value (JSON default)",
			m:    map[string]interface{}{"key": float64(42)},
			key:  "key",
			want: 42,
		},
		{
			name: "int value",
			m:    map[string]interface{}{"key": 100},
			key:  "key",
			want: 100,
		},
		{
			name: "missing key",
			m:    map[string]interface{}{},
			key:  "missing",
			want: 0,
		},
		{
			name: "non-numeric value",
			m:    map[string]interface{}{"key": "not-a-number"},
			key:  "key",
			want: 0,
		},
		{
			name: "negative float64",
			m:    map[string]interface{}{"key": float64(-1)},
			key:  "key",
			want: -1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getIntField(tc.m, tc.key)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGetIntFieldPtr(t *testing.T) {
	tests := []struct {
		name    string
		m       map[string]interface{}
		key     string
		wantVal *int
		wantOk  bool
	}{
		{
			name:    "float64 non-zero",
			m:       map[string]interface{}{"port": float64(8080)},
			key:     "port",
			wantVal: intPtr(8080),
			wantOk:  true,
		},
		{
			name:    "int non-zero",
			m:       map[string]interface{}{"port": 9090},
			key:     "port",
			wantVal: intPtr(9090),
			wantOk:  true,
		},
		{
			name:    "float64 zero",
			m:       map[string]interface{}{"port": float64(0)},
			key:     "port",
			wantVal: nil,
			wantOk:  false,
		},
		{
			name:    "int zero",
			m:       map[string]interface{}{"port": 0},
			key:     "port",
			wantVal: nil,
			wantOk:  false,
		},
		{
			name:    "missing key",
			m:       map[string]interface{}{},
			key:     "port",
			wantVal: nil,
			wantOk:  false,
		},
		{
			name:    "non-numeric value",
			m:       map[string]interface{}{"port": "not-a-number"},
			key:     "port",
			wantVal: nil,
			wantOk:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := getIntFieldPtr(tc.m, tc.key)
			assert.Equal(t, tc.wantOk, ok)
			if tc.wantVal != nil {
				require.NotNil(t, got)
				assert.Equal(t, *tc.wantVal, *got)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}

// intPtr is a helper that returns a pointer to the given int value.
func intPtr(v int) *int {
	return &v
}
