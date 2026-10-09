package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	nexusv1alpha1 "github.com/mkostelcev/nexus-operator/api/v1alpha1"
)

func TestRoutingRuleValidator_ValidateCreate_WrongType(t *testing.T) {
	v := &RoutingRuleValidator{}
	wrongObj := &nexusv1alpha1.RoutingRuleList{}

	warnings, err := v.ValidateCreate(context.Background(), wrongObj)
	assert.Nil(t, warnings)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, errExpectedRoutingRule), "ошибка должна оборачивать errExpectedRoutingRule")
	assert.Contains(t, err.Error(), "ожидался RoutingRule")
}

func TestRoutingRuleValidator_ValidateUpdate_WrongType(t *testing.T) {
	v := &RoutingRuleValidator{}
	wrongObj := &nexusv1alpha1.RoutingRuleList{}
	validRR := &nexusv1alpha1.RoutingRule{
		ObjectMeta: metav1.ObjectMeta{Name: "old"},
		Spec: nexusv1alpha1.RoutingRuleSpec{
			Name:     "old-rule",
			Mode:     "BLOCK",
			Matchers: []string{".*"},
		},
	}

	warnings, err := v.ValidateUpdate(context.Background(), validRR, wrongObj)
	assert.Nil(t, warnings)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, errExpectedRoutingRule), "ошибка должна оборачивать errExpectedRoutingRule")
	assert.Contains(t, err.Error(), "ожидался RoutingRule")
}

func TestRoutingRuleValidator_ValidateDelete(t *testing.T) {
	v := &RoutingRuleValidator{}

	rr := &nexusv1alpha1.RoutingRule{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: nexusv1alpha1.RoutingRuleSpec{
			Name:     "test-rule",
			Mode:     "BLOCK",
			Matchers: []string{".*"},
		},
	}

	warnings, err := v.ValidateDelete(context.Background(), rr)
	assert.Nil(t, warnings)
	assert.NoError(t, err)
}

func TestValidateRoutingRule(t *testing.T) {
	tests := []struct {
		name        string
		rr          *nexusv1alpha1.RoutingRule
		wantErr     bool
		sentinelErr error
		errContains string
	}{
		{
			name: "valid BLOCK with single matcher",
			rr: &nexusv1alpha1.RoutingRule{
				ObjectMeta: metav1.ObjectMeta{Name: "block-rule"},
				Spec: nexusv1alpha1.RoutingRuleSpec{
					Name:     "block-all",
					Mode:     "BLOCK",
					Matchers: []string{".*"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid ALLOW with multiple matchers",
			rr: &nexusv1alpha1.RoutingRule{
				ObjectMeta: metav1.ObjectMeta{Name: "allow-rule"},
				Spec: nexusv1alpha1.RoutingRuleSpec{
					Name:     "allow-some",
					Mode:     "ALLOW",
					Matchers: []string{"^/com/example/.*", "^/org/apache/.*", "^/io/netty/.*"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid mode lowercase block",
			rr: &nexusv1alpha1.RoutingRule{
				ObjectMeta: metav1.ObjectMeta{Name: "lowercase-rule"},
				Spec: nexusv1alpha1.RoutingRuleSpec{
					Name:     "lowercase-mode",
					Mode:     "block",
					Matchers: []string{".*"},
				},
			},
			wantErr:     true,
			sentinelErr: errInvalidMode,
		},
		{
			name: "invalid mode empty string",
			rr: &nexusv1alpha1.RoutingRule{
				ObjectMeta: metav1.ObjectMeta{Name: "empty-mode-rule"},
				Spec: nexusv1alpha1.RoutingRuleSpec{
					Name:     "empty-mode",
					Mode:     "",
					Matchers: []string{".*"},
				},
			},
			wantErr:     true,
			sentinelErr: errInvalidMode,
		},
		{
			name: "invalid mode DENY",
			rr: &nexusv1alpha1.RoutingRule{
				ObjectMeta: metav1.ObjectMeta{Name: "deny-rule"},
				Spec: nexusv1alpha1.RoutingRuleSpec{
					Name:     "deny-mode",
					Mode:     "DENY",
					Matchers: []string{".*"},
				},
			},
			wantErr:     true,
			sentinelErr: errInvalidMode,
		},
		{
			name: "empty matchers slice",
			rr: &nexusv1alpha1.RoutingRule{
				ObjectMeta: metav1.ObjectMeta{Name: "empty-matchers-rule"},
				Spec: nexusv1alpha1.RoutingRuleSpec{
					Name:     "no-matchers",
					Mode:     "BLOCK",
					Matchers: []string{},
				},
			},
			wantErr:     true,
			sentinelErr: errMatchersRequired,
		},
		{
			name: "nil matchers",
			rr: &nexusv1alpha1.RoutingRule{
				ObjectMeta: metav1.ObjectMeta{Name: "nil-matchers-rule"},
				Spec: nexusv1alpha1.RoutingRuleSpec{
					Name:     "nil-matchers",
					Mode:     "ALLOW",
					Matchers: nil,
				},
			},
			wantErr:     true,
			sentinelErr: errMatchersRequired,
		},
		{
			name: "blank matcher",
			rr: &nexusv1alpha1.RoutingRule{
				ObjectMeta: metav1.ObjectMeta{Name: "blank-matcher-rule"},
				Spec: nexusv1alpha1.RoutingRuleSpec{
					Name:     "blank-matcher",
					Mode:     "BLOCK",
					Matchers: []string{".*", "  "},
				},
			},
			wantErr:     true,
			sentinelErr: errMatcherBlank,
			errContains: "matchers[1]",
		},
		{
			name: "complex valid regex",
			rr: &nexusv1alpha1.RoutingRule{
				ObjectMeta: metav1.ObjectMeta{Name: "complex-regex-rule"},
				Spec: nexusv1alpha1.RoutingRuleSpec{
					Name:     "complex-regex",
					Mode:     "BLOCK",
					Matchers: []string{"^/com/example/.*"},
				},
			},
			wantErr: false,
		},
	}

	v := &RoutingRuleValidator{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			warnings, err := v.ValidateCreate(context.Background(), tc.rr)
			assert.Nil(t, warnings)

			if !tc.wantErr {
				assert.NoError(t, err)
				return
			}

			assert.Error(t, err)

			if tc.sentinelErr != nil {
				assert.True(t, errors.Is(err, tc.sentinelErr),
					"expected error to wrap %v, got: %v", tc.sentinelErr, err)
			}

			if tc.errContains != "" {
				assert.Contains(t, err.Error(), tc.errContains)
			}
		})
	}
}

func TestRoutingRuleValidator_NonRE2MatcherIsWarning(t *testing.T) {
	v := &RoutingRuleValidator{}
	// Все выражения принимает java.util.regex.Pattern (Java 17, как в Nexus 3.77) и не принимает RE2.
	// Первое взято из pypi-block-rule nexus-pg. Последние два битые и для Java: их отклонит Nexus
	matchers := []string{
		`(?i).*/sentry_sdk-2[.](4[0-9]|5[0-6])[.][0-9]+(?!.*a9).*`,
		`.*(?<!-SNAPSHOT)[.]jar`,
		`(?>a+)b`,
		`^/(\w+)/\1/.*`,
		`a*+`,
		`\p{javaLowerCase}+`,
		`\R`,
		`(?x) a b`,
		`(`,
		`[invalid`,
	}

	for _, m := range matchers {
		t.Run(m, func(t *testing.T) {
			rr := &nexusv1alpha1.RoutingRule{
				ObjectMeta: metav1.ObjectMeta{Name: "non-re2-rule"},
				Spec: nexusv1alpha1.RoutingRuleSpec{
					Name:     "non-re2",
					Mode:     "BLOCK",
					Matchers: []string{".*", m},
				},
			}

			for _, call := range []func() (admission.Warnings, error){
				func() (admission.Warnings, error) { return v.ValidateCreate(context.Background(), rr) },
				func() (admission.Warnings, error) { return v.ValidateUpdate(context.Background(), rr, rr) },
			} {
				warnings, err := call()
				assert.NoError(t, err)
				assert.Len(t, warnings, 1)
				assert.Contains(t, warnings[0], "spec.matchers[1]")
			}
		})
	}
}
