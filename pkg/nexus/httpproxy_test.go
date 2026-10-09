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

func TestBuildHttpProxyConfig(t *testing.T) {
	t.Run("with basic auth", func(t *testing.T) {
		spec := &v1alpha1.HttpProxySpec{
			HttpProxy: &v1alpha1.HttpProxyServerConfig{
				Enabled: true,
				Host:    "proxy.example.com",
				Port:    "8080",
				AuthInfo: &v1alpha1.ProxyAuthConfig{
					Enabled:  true,
					Username: "user",
					Password: "pass",
				},
			},
			NonProxyHosts: []string{"localhost", "*.internal"},
		}

		config, err := BuildHttpProxyConfig(spec)
		require.NoError(t, err)

		require.NotNil(t, config.HttpProxy)
		assert.True(t, config.HttpProxy.Enabled)
		assert.Equal(t, "proxy.example.com", config.HttpProxy.Host)
		assert.Equal(t, 8080, config.HttpProxy.Port)
		require.NotNil(t, config.HttpProxy.Authentication)
		assert.Equal(t, "username", config.HttpProxy.Authentication.Type)
		assert.Equal(t, "user", config.HttpProxy.Authentication.Username)
		assert.Equal(t, "pass", config.HttpProxy.Authentication.Password)
		assert.Nil(t, config.HttpsProxy)
		assert.Equal(t, []string{"localhost", "*.internal"}, config.NonProxyHosts)
	})

	t.Run("without auth", func(t *testing.T) {
		spec := &v1alpha1.HttpProxySpec{
			HttpProxy: &v1alpha1.HttpProxyServerConfig{
				Enabled: true,
				Host:    "proxy.example.com",
				Port:    "3128",
			},
		}

		config, err := BuildHttpProxyConfig(spec)
		require.NoError(t, err)

		require.NotNil(t, config.HttpProxy)
		assert.True(t, config.HttpProxy.Enabled)
		assert.Equal(t, 3128, config.HttpProxy.Port)
		assert.Nil(t, config.HttpProxy.Authentication)
	})

	t.Run("auth present but disabled", func(t *testing.T) {
		spec := &v1alpha1.HttpProxySpec{
			HttpProxy: &v1alpha1.HttpProxyServerConfig{
				Enabled: true,
				Host:    "proxy.example.com",
				Port:    "3128",
				AuthInfo: &v1alpha1.ProxyAuthConfig{
					Enabled: false,
				},
			},
		}

		config, err := BuildHttpProxyConfig(spec)
		require.NoError(t, err)

		require.NotNil(t, config.HttpProxy)
		assert.Nil(t, config.HttpProxy.Authentication)
	})

	t.Run("auth enabled without username returns error", func(t *testing.T) {
		spec := &v1alpha1.HttpProxySpec{
			HttpProxy: &v1alpha1.HttpProxyServerConfig{
				Enabled: true,
				Host:    "proxy.example.com",
				Port:    "3128",
				AuthInfo: &v1alpha1.ProxyAuthConfig{
					Enabled:  true,
					Password: "pass",
				},
			},
		}

		_, err := BuildHttpProxyConfig(spec)
		assert.ErrorIs(t, err, ErrProxyAuthNoUsername)
	})

	t.Run("https auth enabled without username returns error", func(t *testing.T) {
		spec := &v1alpha1.HttpProxySpec{
			HttpsProxy: &v1alpha1.HttpProxyServerConfig{
				Enabled: true,
				Host:    "proxy.example.com",
				Port:    "3128",
				AuthInfo: &v1alpha1.ProxyAuthConfig{
					Enabled:  true,
					Password: "pass",
				},
			},
		}

		_, err := BuildHttpProxyConfig(spec)
		assert.ErrorIs(t, err, ErrProxyAuthNoUsername)
	})

	t.Run("with NTLM auth", func(t *testing.T) {
		spec := &v1alpha1.HttpProxySpec{
			HttpsProxy: &v1alpha1.HttpProxyServerConfig{
				Enabled: true,
				Host:    "ntlm-proxy.corp",
				Port:    "8443",
				AuthInfo: &v1alpha1.ProxyAuthConfig{
					Enabled:    true,
					Username:   "domain\\user",
					Password:   "secret",
					NtlmHost:   "workstation",
					NtlmDomain: "CORP",
				},
			},
		}

		config, err := BuildHttpProxyConfig(spec)
		require.NoError(t, err)

		assert.Nil(t, config.HttpProxy)
		require.NotNil(t, config.HttpsProxy)
		assert.True(t, config.HttpsProxy.Enabled)
		require.NotNil(t, config.HttpsProxy.Authentication)
		assert.Equal(t, "ntlm", config.HttpsProxy.Authentication.Type)
		assert.Equal(t, "workstation", config.HttpsProxy.Authentication.NtlmHost)
		assert.Equal(t, "CORP", config.HttpsProxy.Authentication.NtlmDomain)
	})
}

func TestBuildHttpProxySpecFromAPI(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		spec := BuildHttpProxySpecFromAPI(nil)
		assert.Nil(t, spec)
	})

	t.Run("basic config without auth", func(t *testing.T) {
		config := &HttpProxyConfig{
			HttpProxy: &HttpProxySettings{
				Enabled: true,
				Host:    "proxy.example.com",
				Port:    8080,
			},
			NonProxyHosts: []string{"localhost"},
		}

		spec := BuildHttpProxySpecFromAPI(config)

		require.NotNil(t, spec)
		require.NotNil(t, spec.HttpProxy)
		assert.True(t, spec.HttpProxy.Enabled)
		assert.Equal(t, "proxy.example.com", spec.HttpProxy.Host)
		assert.Equal(t, "8080", spec.HttpProxy.Port)
		assert.Nil(t, spec.HttpProxy.AuthInfo)
		assert.Nil(t, spec.HttpsProxy)
		assert.Equal(t, []string{"localhost"}, spec.NonProxyHosts)
	})
}

func TestBuildHttpProxySpecFromAPI_WithAuth(t *testing.T) {
	config := &HttpProxyConfig{
		HttpProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "proxy.example.com",
			Port:    8080,
			Authentication: &HttpProxyAuthSettings{
				Type:     "username",
				Username: "admin",
				Password: "****",
			},
		},
		HttpsProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "proxy.example.com",
			Port:    8443,
			Authentication: &HttpProxyAuthSettings{
				Type:       "ntlm",
				Username:   "domain\\user",
				Password:   "****",
				NtlmHost:   "ws01",
				NtlmDomain: "CORP",
			},
		},
	}

	spec := BuildHttpProxySpecFromAPI(config)

	require.NotNil(t, spec)

	require.NotNil(t, spec.HttpProxy)
	require.NotNil(t, spec.HttpProxy.AuthInfo)
	assert.True(t, spec.HttpProxy.AuthInfo.Enabled)
	assert.Equal(t, "admin", spec.HttpProxy.AuthInfo.Username)
	assert.Equal(t, "****", spec.HttpProxy.AuthInfo.Password)

	require.NotNil(t, spec.HttpsProxy)
	require.NotNil(t, spec.HttpsProxy.AuthInfo)
	assert.True(t, spec.HttpsProxy.AuthInfo.Enabled)
	assert.Equal(t, "domain\\user", spec.HttpsProxy.AuthInfo.Username)
	assert.Equal(t, "ws01", spec.HttpsProxy.AuthInfo.NtlmHost)
	assert.Equal(t, "CORP", spec.HttpsProxy.AuthInfo.NtlmDomain)
}

func TestHttpProxyNeedsUpdate_Equal(t *testing.T) {
	current := &HttpProxyConfig{
		HttpProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "proxy.example.com",
			Port:    8080,
		},
		NonProxyHosts: []string{"localhost"},
	}
	desired := &HttpProxyConfig{
		HttpProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "proxy.example.com",
			Port:    8080,
		},
		NonProxyHosts: []string{"localhost"},
	}

	assert.False(t, HttpProxyNeedsUpdate(current, desired))
}

func TestHttpProxyNeedsUpdate_Different(t *testing.T) {
	current := &HttpProxyConfig{
		HttpProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "proxy.example.com",
			Port:    8080,
		},
	}
	desired := &HttpProxyConfig{
		HttpProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "new-proxy.example.com",
			Port:    3128,
		},
	}

	assert.True(t, HttpProxyNeedsUpdate(current, desired))
}

func TestHttpProxyNeedsUpdate_PasswordAlwaysUpdates(t *testing.T) {
	current := &HttpProxyConfig{
		HttpProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "proxy.example.com",
			Port:    8080,
			Authentication: &HttpProxyAuthSettings{
				Type:     "username",
				Username: "admin",
				Password: "****",
			},
		},
	}
	desired := &HttpProxyConfig{
		HttpProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "proxy.example.com",
			Port:    8080,
			Authentication: &HttpProxyAuthSettings{
				Type:     "username",
				Username: "admin",
				Password: "new-password",
			},
		},
	}

	assert.True(t, HttpProxyNeedsUpdate(current, desired))
}

func TestHttpProxyConfigToExtDirect_NoAuthSendsFalse(t *testing.T) {
	config := &HttpProxyConfig{
		HttpProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "10.7.200.8",
			Port:    3129,
			// Authentication не указан
		},
		HttpsProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "10.7.200.8",
			Port:    3129,
			// Authentication не указан
		},
	}

	settings := httpProxyConfigToExtDirect(config)

	// httpAuthEnabled должен быть явно false, а не nil
	require.NotNil(t, settings.HttpAuthEnabled, "httpAuthEnabled must not be nil when auth is not configured")
	assert.False(t, *settings.HttpAuthEnabled)

	// httpsAuthEnabled тоже
	require.NotNil(t, settings.HttpsAuthEnabled, "httpsAuthEnabled must not be nil when auth is not configured")
	assert.False(t, *settings.HttpsAuthEnabled)

	// Проверяем что в JSON нет null для httpAuthEnabled
	data, err := json.Marshal(settings)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"httpAuthEnabled":false`)
	assert.Contains(t, string(data), `"httpsAuthEnabled":false`)
}

func TestGetHttpProxy_ExtDirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(ExtDirectPath, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		var req ExtDirectRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "coreui_HttpSettings", req.Action)
		assert.Equal(t, "read", req.Method)

		w.Header().Set("Content-Type", "application/json")

		httpEnabled := true
		httpHost := "proxy.corp"
		httpPort := 3128

		data := ExtDirectHttpSettings{
			HttpEnabled:   &httpEnabled,
			HttpHost:      &httpHost,
			HttpPort:      &httpPort,
			NonProxyHosts: []string{"*.internal"},
		}
		dataBytes, _ := json.Marshal(data)
		result := ExtDirectResult{Success: true, Data: dataBytes}
		resultBytes, _ := json.Marshal(result)

		resp := ExtDirectResponse{
			TID:    req.TID,
			Action: req.Action,
			Method: req.Method,
			Result: resultBytes,
			Type:   "rpc",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	config, err := client.GetHttpProxy(context.Background())

	require.NoError(t, err)
	require.NotNil(t, config)
	require.NotNil(t, config.HttpProxy)
	assert.True(t, config.HttpProxy.Enabled)
	assert.Equal(t, "proxy.corp", config.HttpProxy.Host)
	assert.Equal(t, 3128, config.HttpProxy.Port)
	assert.Equal(t, []string{"*.internal"}, config.NonProxyHosts)
}

func TestUpdateHttpProxy_ExtDirect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(ExtDirectPath, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)

		var req ExtDirectRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "coreui_HttpSettings", req.Action)
		assert.Equal(t, "update", req.Method)

		w.Header().Set("Content-Type", "application/json")
		result := ExtDirectResult{Success: true, Data: json.RawMessage(`{}`)}
		resultBytes, _ := json.Marshal(result)

		resp := ExtDirectResponse{
			TID:    req.TID,
			Action: req.Action,
			Method: req.Method,
			Result: resultBytes,
			Type:   "rpc",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.UpdateHttpProxy(context.Background(), HttpProxyConfig{
		HttpProxy: &HttpProxySettings{
			Enabled: true,
			Host:    "proxy.corp",
			Port:    3128,
		},
		NonProxyHosts: []string{"*.internal"},
	})

	require.NoError(t, err)
}
