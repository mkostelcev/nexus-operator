package controller

import (
	"testing"
)

func TestValidateCSELExpression(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		wantErr    bool
		errContain string
	}{
		{
			name:       "valid simple regex",
			expression: `path =~ "^.*example.*$"`,
			wantErr:    false,
		},
		{
			name:       "valid case-insensitive via character class",
			expression: `path =~ "^.*[Ee][Xx][Aa][Mm][Pp][Ll][Ee].*$"`,
			wantErr:    false,
		},
		{
			name:       "valid multiple conditions",
			expression: `path =~ "^/org/.*" and path =~ ".*-SNAPSHOT.*$"`,
			wantErr:    false,
		},
		{
			name:       "valid single quotes",
			expression: `path =~ '^.*test.*$'`,
			wantErr:    false,
		},
		{
			name:       "invalid lookahead",
			expression: `path =~ "^(?!.*example).*$"`,
			wantErr:    true,
			errContain: "lookahead/lookbehind",
		},
		{
			name:       "invalid lookbehind",
			expression: `path =~ "(?<=prefix).*"`,
			wantErr:    true,
			errContain: "lookahead/lookbehind",
		},
		{
			name:       "invalid negative lookbehind",
			expression: `path =~ "(?<!prefix).*"`,
			wantErr:    true,
			errContain: "lookahead/lookbehind",
		},
		{
			name:       "invalid inline flag (?i)",
			expression: `path =~ "(?i)example"`,
			wantErr:    true,
			errContain: "inline флаги",
		},
		{
			name:       "invalid inline flag (?m)",
			expression: `path =~ "(?m)^line$"`,
			wantErr:    true,
			errContain: "inline флаги",
		},
		{
			name:       "invalid non-capturing group",
			expression: `path =~ "(?:foo|bar)"`,
			wantErr:    true,
			errContain: "non-capturing group",
		},
		{
			name:       "invalid atomic group",
			expression: `path =~ "(?>foo)"`,
			wantErr:    true,
			errContain: "atomic group",
		},
		{
			name:       "invalid possessive quantifier ++",
			expression: `path =~ "a++"`,
			wantErr:    true,
			errContain: "possessive quantifier",
		},
		{
			name:       "invalid possessive quantifier *+",
			expression: `path =~ "a*+"`,
			wantErr:    true,
			errContain: "possessive quantifier",
		},
		{
			name:       "no regex in expression",
			expression: `format == "maven2"`,
			wantErr:    false,
		},
		{
			name:       "empty expression",
			expression: ``,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCSELExpression(tt.expression)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateCSELExpression() expected error containing %q, got nil", tt.errContain)
					return
				}
				if tt.errContain != "" && !containsString(err.Error(), tt.errContain) {
					t.Errorf("validateCSELExpression() error = %v, want error containing %q", err, tt.errContain)
				}
			} else {
				if err != nil {
					t.Errorf("validateCSELExpression() unexpected error = %v", err)
				}
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
