package utils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainsString(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		s        string
		expected bool
	}{
		{
			name:     "string exists in slice",
			slice:    []string{"foo", "bar", "baz"},
			s:        "bar",
			expected: true,
		},
		{
			name:     "string does not exist in slice",
			slice:    []string{"foo", "bar", "baz"},
			s:        "qux",
			expected: false,
		},
		{
			name:     "empty string in slice",
			slice:    []string{"foo", "", "bar"},
			s:        "",
			expected: true,
		},
		{
			name:     "empty string not in slice",
			slice:    []string{"foo", "bar"},
			s:        "",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			s:        "foo",
			expected: false,
		},
		{
			name:     "nil slice",
			slice:    nil,
			s:        "foo",
			expected: false,
		},
		{
			name:     "single element match",
			slice:    []string{"foo"},
			s:        "foo",
			expected: true,
		},
		{
			name:     "single element no match",
			slice:    []string{"foo"},
			s:        "bar",
			expected: false,
		},
		{
			name:     "case sensitive - lowercase",
			slice:    []string{"Foo", "Bar"},
			s:        "foo",
			expected: false,
		},
		{
			name:     "case sensitive - uppercase",
			slice:    []string{"foo", "bar"},
			s:        "FOO",
			expected: false,
		},
		{
			name:     "duplicate values in slice",
			slice:    []string{"foo", "bar", "foo"},
			s:        "foo",
			expected: true,
		},
		{
			name:     "special characters",
			slice:    []string{"foo-bar", "baz_qux", "test.name"},
			s:        "baz_qux",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsString(tt.slice, tt.s)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRemoveString(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		s        string
		expected []string
	}{
		{
			name:     "remove existing string",
			slice:    []string{"foo", "bar", "baz"},
			s:        "bar",
			expected: []string{"foo", "baz"},
		},
		{
			name:     "remove non-existing string",
			slice:    []string{"foo", "bar", "baz"},
			s:        "qux",
			expected: []string{"foo", "bar", "baz"},
		},
		{
			name:     "remove from empty slice",
			slice:    []string{},
			s:        "foo",
			expected: []string{},
		},
		{
			name:     "remove from nil slice",
			slice:    nil,
			s:        "foo",
			expected: []string{},
		},
		{
			name:     "remove empty string",
			slice:    []string{"foo", "", "bar"},
			s:        "",
			expected: []string{"foo", "bar"},
		},
		{
			name:     "remove all occurrences",
			slice:    []string{"foo", "bar", "foo", "baz", "foo"},
			s:        "foo",
			expected: []string{"bar", "baz"},
		},
		{
			name:     "remove only element",
			slice:    []string{"foo"},
			s:        "foo",
			expected: []string{},
		},
		{
			name:     "remove from single element - no match",
			slice:    []string{"foo"},
			s:        "bar",
			expected: []string{"foo"},
		},
		{
			name:     "case sensitive removal",
			slice:    []string{"Foo", "foo", "FOO"},
			s:        "foo",
			expected: []string{"Foo", "FOO"},
		},
		{
			name:     "special characters",
			slice:    []string{"foo-bar", "baz_qux", "test.name"},
			s:        "baz_qux",
			expected: []string{"foo-bar", "test.name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RemoveString(tt.slice, tt.s)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeK8sName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase name",
			input:    "myapp",
			expected: "myapp",
		},
		{
			name:     "uppercase to lowercase",
			input:    "MyApp",
			expected: "myapp",
		},
		{
			name:     "replace underscores",
			input:    "my_app_name",
			expected: "my-app-name",
		},
		{
			name:     "replace dots",
			input:    "my.app.name",
			expected: "my-app-name",
		},
		{
			name:     "mixed underscores and dots",
			input:    "my_app.name",
			expected: "my-app-name",
		},
		{
			name:     "remove invalid characters",
			input:    "my@app#name$",
			expected: "my-app-name",
		},
		{
			name:     "collapse multiple dashes",
			input:    "my---app--name",
			expected: "my-app-name",
		},
		{
			name:     "trim leading dashes",
			input:    "---myapp",
			expected: "myapp",
		},
		{
			name:     "trim trailing dashes",
			input:    "myapp---",
			expected: "myapp",
		},
		{
			name:     "trim leading and trailing dashes",
			input:    "---myapp---",
			expected: "myapp",
		},
		{
			name:     "leading dots converted to dashes and trimmed",
			input:    "...myapp",
			expected: "myapp",
		},
		{
			name:     "trailing dots converted to dashes and trimmed",
			input:    "myapp...",
			expected: "myapp",
		},
		{
			name:     "leading underscores converted to dashes and trimmed",
			input:    "___myapp",
			expected: "myapp",
		},
		{
			name:     "trailing underscores converted to dashes and trimmed",
			input:    "myapp___",
			expected: "myapp",
		},
		{
			name:     "complex name with multiple issues",
			input:    "__My_App.Name@2024__",
			expected: "my-app-name-2024",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only dashes",
			input:    "-----",
			expected: "",
		},
		{
			name:     "only invalid characters",
			input:    "@#$%^&*",
			expected: "",
		},
		{
			name:     "alphanumeric with dashes",
			input:    "app-123-test",
			expected: "app-123-test",
		},
		{
			name:     "numbers only",
			input:    "12345",
			expected: "12345",
		},
		{
			name:     "max length exactly 253",
			input:    strings.Repeat("a", 253),
			expected: strings.Repeat("a", 253),
		},
		{
			name:     "exceeds max length 253",
			input:    strings.Repeat("a", 300),
			expected: strings.Repeat("a", 253),
		},
		{
			name:     "exceeds max length with trailing dash",
			input:    strings.Repeat("a", 252) + "-" + strings.Repeat("b", 50),
			expected: strings.Repeat("a", 252),
		},
		{
			name:     "exceeds max length complex",
			input:    strings.Repeat("test_", 60) + "end",
			expected: strings.TrimRight(strings.ReplaceAll(strings.Repeat("test_", 60)+"end", "_", "-")[:253], "-"),
		},
		{
			name:     "unicode characters removed",
			input:    "my-app-тест",
			expected: "my-app",
		},
		{
			name:     "spaces converted",
			input:    "my app name",
			expected: "my-app-name",
		},
		{
			name:     "mixed case with special chars",
			input:    "MyApp_2024.v1@prod",
			expected: "myapp-2024-v1-prod",
		},
		{
			name:     "nexus repository name example",
			input:    "docker_proxy.internal",
			expected: "docker-proxy-internal",
		},
		{
			name:     "camelCase",
			input:    "camelCaseAppName",
			expected: "camelcaseappname",
		},
		{
			name:     "SCREAMING_SNAKE_CASE",
			input:    "NEXUS_REPOSITORY_NAME",
			expected: "nexus-repository-name",
		},
		{
			name:     "kebab-case preserved",
			input:    "my-kebab-case-name",
			expected: "my-kebab-case-name",
		},
		{
			name:     "multiple consecutive special chars",
			input:    "app@@##$$name",
			expected: "app-name",
		},
		{
			name:     "dots and underscores mixed with dashes",
			input:    "my._-_.app",
			expected: "my-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeK8sName(tt.input)
			assert.Equal(t, tt.expected, result)

			// Additional validation: result should be valid k8s name
			if result != "" {
				assert.LessOrEqual(t, len(result), 253, "result should be at most 253 characters")
				assert.NotContains(t, result, "_", "result should not contain underscores")
				assert.NotContains(t, result, ".", "result should not contain dots")
				assert.NotContains(t, result, "--", "result should not contain consecutive dashes")
				assert.NotRegexp(t, "^-", result, "result should not start with dash")
				assert.NotRegexp(t, "-$", result, "result should not end with dash")
				assert.Regexp(t, "^[a-z0-9-]*$", result, "result should only contain lowercase alphanumeric and dashes")
			}
		})
	}
}
