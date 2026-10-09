package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

// helper: вызывает ValidateCreate для данного NexusConfiguration и возвращает ошибку.
func createAndValidateConfig(t *testing.T, cr *nexusv1alpha1.NexusConfiguration) error {
	t.Helper()
	v := &NexusConfigurationValidator{}
	_, err := v.ValidateCreate(context.Background(), cr)
	return err
}

// ---------------------------------------------------------------------------
// ValidateCreate — wrong type
// ---------------------------------------------------------------------------
func TestNexusConfigurationValidateCreate_WrongType(t *testing.T) {
	v := &NexusConfigurationValidator{}
	// Передаём объект неправильного типа (NexusConfigurationList вместо NexusConfiguration).
	_, err := v.ValidateCreate(context.Background(), &nexusv1alpha1.NexusConfigurationList{})

	assert.Error(t, err)
	assert.True(t, errors.Is(err, errExpectedNexusConfiguration),
		"ожидалась ошибка errExpectedNexusConfiguration, получена: %v", err)
	assert.ErrorContains(t, err, "ожидался NexusConfiguration")
}

// ---------------------------------------------------------------------------
// ValidateCreate — valid config
// ---------------------------------------------------------------------------
func TestNexusConfigurationValidateCreate_ValidConfig(t *testing.T) {
	cr := &nexusv1alpha1.NexusConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: nexusv1alpha1.NexusConfigurationSpec{
			HttpProxy: &nexusv1alpha1.HttpProxySpec{
				HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
					Enabled: true,
					Host:    "proxy.local",
					Port:    "8080",
				},
			},
			SecurityRealms: &nexusv1alpha1.SecurityRealmsSpec{
				Active: []string{"NexusAuthenticatingRealm"},
			},
		},
	}

	err := createAndValidateConfig(t, cr)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// ValidateUpdate — wrong type
// ---------------------------------------------------------------------------
func TestNexusConfigurationValidateUpdate_WrongType(t *testing.T) {
	v := &NexusConfigurationValidator{}
	_, err := v.ValidateUpdate(context.Background(),
		&nexusv1alpha1.NexusConfigurationList{},
		&nexusv1alpha1.NexusConfigurationList{},
	)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, errExpectedNexusConfiguration),
		"ожидалась ошибка errExpectedNexusConfiguration, получена: %v", err)
	assert.ErrorContains(t, err, "ожидался NexusConfiguration")
}

// ---------------------------------------------------------------------------
// ValidateUpdate — valid config
// ---------------------------------------------------------------------------
func TestNexusConfigurationValidateUpdate_ValidConfig(t *testing.T) {
	v := &NexusConfigurationValidator{}
	cr := &nexusv1alpha1.NexusConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: nexusv1alpha1.NexusConfigurationSpec{
			HttpProxy: &nexusv1alpha1.HttpProxySpec{
				HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
					Enabled: true,
					Host:    "proxy.local",
					Port:    "3128",
				},
			},
		},
	}

	_, err := v.ValidateUpdate(context.Background(), cr, cr)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// ValidateDelete — always nil
// ---------------------------------------------------------------------------
func TestNexusConfigurationValidateDelete_AlwaysNil(t *testing.T) {
	v := &NexusConfigurationValidator{}
	warnings, err := v.ValidateDelete(context.Background(), &nexusv1alpha1.NexusConfiguration{})

	assert.NoError(t, err)
	assert.Nil(t, warnings)
}

// ---------------------------------------------------------------------------
// validateNexusConfiguration — table-driven (вызывается через ValidateCreate)
// ---------------------------------------------------------------------------
func TestValidateNexusConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		spec        nexusv1alpha1.NexusConfigurationSpec
		wantErr     bool
		sentinelErr error  // проверяется через errors.Is, если не nil
		errContains string // проверяется через assert.ErrorContains, если не пуст
	}{
		// ---- пустая спецификация ----
		{
			name: "empty spec — no httpProxy, no securityRealms",
			spec: nexusv1alpha1.NexusConfigurationSpec{},
		},

		// ---- HttpProxy nil ----
		{
			name: "httpProxy nil",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: nil,
			},
		},

		// ---- HttpProxy with nil httpProxy and nil httpsProxy ----
		{
			name: "httpProxy set but httpProxy and httpsProxy are nil",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{},
			},
		},

		// ---- valid httpProxy ----
		{
			name: "httpProxy valid — enabled, host set, port valid",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "8080",
					},
				},
			},
		},

		// ---- httpProxy enabled, empty host ----
		{
			name: "httpProxy enabled, host empty — errProxyHostRequired",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "",
						Port:    "8080",
					},
				},
			},
			wantErr:     true,
			sentinelErr: errProxyHostRequired,
			errContains: "spec.httpProxy.httpProxy",
		},

		// ---- httpProxy enabled, host set, invalid port "abc" ----
		{
			name: "httpProxy port non-numeric — errProxyPortInvalid",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "abc",
					},
				},
			},
			wantErr:     true,
			sentinelErr: errProxyPortInvalid,
			errContains: "spec.httpProxy.httpProxy",
		},

		// ---- httpProxy port "0" (< 1) ----
		{
			name: "httpProxy port 0 — errProxyPortInvalid",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "0",
					},
				},
			},
			wantErr:     true,
			sentinelErr: errProxyPortInvalid,
			errContains: "spec.httpProxy.httpProxy",
		},

		// ---- httpProxy port "65536" (> 65535) ----
		{
			name: "httpProxy port 65536 — errProxyPortInvalid",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "65536",
					},
				},
			},
			wantErr:     true,
			sentinelErr: errProxyPortInvalid,
			errContains: "spec.httpProxy.httpProxy",
		},

		// ---- httpProxy port empty string ----
		{
			name: "httpProxy port empty string — errProxyPortInvalid",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "",
					},
				},
			},
			wantErr:     true,
			sentinelErr: errProxyPortInvalid,
			errContains: "spec.httpProxy.httpProxy",
		},

		// ---- httpProxy disabled, empty host, valid port → no error ----
		{
			name: "httpProxy disabled, empty host, valid port — no error",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: false,
						Host:    "",
						Port:    "8080",
					},
				},
			},
		},

		// ---- httpProxy disabled, empty host, invalid port → errProxyPortInvalid ----
		{
			name: "httpProxy disabled, empty host, invalid port — errProxyPortInvalid",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: false,
						Host:    "",
						Port:    "abc",
					},
				},
			},
			wantErr:     true,
			sentinelErr: errProxyPortInvalid,
			errContains: "spec.httpProxy.httpProxy",
		},

		// ---- boundary: port "1" ----
		{
			name: "httpProxy port 1 — boundary valid",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "1",
					},
				},
			},
		},

		// ---- boundary: port "65535" ----
		{
			name: "httpProxy port 65535 — boundary valid",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "65535",
					},
				},
			},
		},

		// ---- httpsProxy validation — enabled, empty host ----
		{
			name: "httpsProxy enabled, host empty — errProxyHostRequired, path httpsProxy",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpsProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "",
						Port:    "8443",
					},
				},
			},
			wantErr:     true,
			sentinelErr: errProxyHostRequired,
			errContains: "spec.httpProxy.httpsProxy",
		},

		// ---- httpsProxy validation — invalid port ----
		{
			name: "httpsProxy invalid port — errProxyPortInvalid, path httpsProxy",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpsProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "99999",
					},
				},
			},
			wantErr:     true,
			sentinelErr: errProxyPortInvalid,
			errContains: "spec.httpProxy.httpsProxy",
		},

		// ---- valid httpProxy, nil httpsProxy ----
		{
			name: "valid httpProxy, nil httpsProxy — no error",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "3128",
					},
					HttpsProxy: nil,
				},
			},
		},

		// ---- securityRealms: empty active ----
		{
			name: "securityRealms active empty — errRealmsActiveEmpty",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				SecurityRealms: &nexusv1alpha1.SecurityRealmsSpec{
					Active: []string{},
				},
			},
			wantErr:     true,
			sentinelErr: errRealmsActiveEmpty,
		},

		// ---- securityRealms: nil active (len == 0) ----
		{
			name: "securityRealms active nil — errRealmsActiveEmpty",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				SecurityRealms: &nexusv1alpha1.SecurityRealmsSpec{
					Active: nil,
				},
			},
			wantErr:     true,
			sentinelErr: errRealmsActiveEmpty,
		},

		// ---- securityRealms: non-empty active ----
		{
			name: "securityRealms active non-empty — no error",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				SecurityRealms: &nexusv1alpha1.SecurityRealmsSpec{
					Active: []string{"NexusAuthenticatingRealm", "LdapRealm"},
				},
			},
		},

		// ---- both httpProxy and securityRealms valid ----
		{
			name: "both httpProxy and securityRealms valid — no error",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "8080",
					},
					HttpsProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "proxy.local",
						Port:    "8443",
					},
				},
				SecurityRealms: &nexusv1alpha1.SecurityRealmsSpec{
					Active: []string{"NexusAuthenticatingRealm"},
				},
			},
		},

		// ---- httpProxy error takes precedence over securityRealms error ----
		{
			name: "httpProxy error returned before securityRealms check",
			spec: nexusv1alpha1.NexusConfigurationSpec{
				HttpProxy: &nexusv1alpha1.HttpProxySpec{
					HttpProxy: &nexusv1alpha1.HttpProxyServerConfig{
						Enabled: true,
						Host:    "",
						Port:    "8080",
					},
				},
				SecurityRealms: &nexusv1alpha1.SecurityRealmsSpec{
					Active: []string{},
				},
			},
			wantErr:     true,
			sentinelErr: errProxyHostRequired,
			errContains: "spec.httpProxy.httpProxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &nexusv1alpha1.NexusConfiguration{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
				Spec:       tt.spec,
			}

			err := createAndValidateConfig(t, cr)

			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}

			assert.Error(t, err)

			if tt.sentinelErr != nil {
				assert.True(t, errors.Is(err, tt.sentinelErr),
					"ожидалась sentinel-ошибка %v, получена: %v", tt.sentinelErr, err)
			}

			if tt.errContains != "" {
				assert.ErrorContains(t, err, tt.errContains)
			}
		})
	}
}
