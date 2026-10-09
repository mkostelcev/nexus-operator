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

func TestBuildRoutingRuleConfig(t *testing.T) {
	spec := v1alpha1.RoutingRuleSpec{
		Name:        "block-snapshots",
		Description: "Block snapshot versions",
		Mode:        "BLOCK",
		Matchers:    []string{".*-SNAPSHOT/.*", ".*SNAPSHOT.*"},
	}

	rule := BuildRoutingRuleConfig(spec)

	assert.Equal(t, "block-snapshots", rule.Name)
	assert.Equal(t, "Block snapshot versions", rule.Description)
	assert.Equal(t, "BLOCK", rule.Mode)
	assert.Equal(t, []string{".*-SNAPSHOT/.*", ".*SNAPSHOT.*"}, rule.Matchers)
}

func TestBuildRoutingRuleSpecFromAPI(t *testing.T) {
	rule := RoutingRuleXO{
		Name:        "allow-releases",
		Description: "Allow release versions",
		Mode:        "ALLOW",
		Matchers:    []string{".*-release/.*"},
	}

	spec := BuildRoutingRuleSpecFromAPI(rule)

	assert.Equal(t, "allow-releases", spec.Name)
	assert.Equal(t, "Allow release versions", spec.Description)
	assert.Equal(t, "ALLOW", spec.Mode)
	assert.Equal(t, []string{".*-release/.*"}, spec.Matchers)
	assert.IsType(t, v1alpha1.RoutingRuleSpec{}, spec)
}

func TestBuildRoutingRuleSpecFromAPI_NilMatchers(t *testing.T) {
	rule := RoutingRuleXO{
		Name:        "empty-matchers",
		Description: "Rule with nil matchers",
		Mode:        "BLOCK",
		Matchers:    nil,
	}

	spec := BuildRoutingRuleSpecFromAPI(rule)

	require.NotNil(t, spec.Matchers)
	assert.Empty(t, spec.Matchers)
	assert.Equal(t, []string{}, spec.Matchers)
}

func TestListRoutingRules(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(RoutingRuleAPIPath, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		rules := []RoutingRuleXO{
			{Name: "rule-1", Description: "First", Mode: "BLOCK", Matchers: []string{".*"}},
			{Name: "rule-2", Description: "Second", Mode: "ALLOW", Matchers: []string{"/api/.*"}},
		}
		_ = json.NewEncoder(w).Encode(rules)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	rules, err := client.ListRoutingRules(context.Background())

	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, "rule-1", rules[0].Name)
	assert.Equal(t, "rule-2", rules[1].Name)
}

func TestGetRoutingRule(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(RoutingRuleAPIPath+"/test-rule", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		rule := RoutingRuleXO{
			Name:        "test-rule",
			Description: "Test routing rule",
			Mode:        "BLOCK",
			Matchers:    []string{".*SNAPSHOT.*"},
		}
		_ = json.NewEncoder(w).Encode(rule)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	rule, err := client.GetRoutingRule(context.Background(), "test-rule")

	require.NoError(t, err)
	require.NotNil(t, rule)
	assert.Equal(t, "test-rule", rule.Name)
	assert.Equal(t, "BLOCK", rule.Mode)
	assert.Equal(t, []string{".*SNAPSHOT.*"}, rule.Matchers)
}

func TestGetRoutingRule_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(RoutingRuleAPIPath+"/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	rule, err := client.GetRoutingRule(context.Background(), "missing")

	assert.Nil(t, rule)
	assert.ErrorIs(t, err, ErrRoutingRuleNotFound)
}

func TestCreateRoutingRule(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(RoutingRuleAPIPath, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		var body RoutingRuleXO
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "new-rule", body.Name)
		assert.Equal(t, "BLOCK", body.Mode)
		assert.Equal(t, []string{".*SNAPSHOT.*"}, body.Matchers)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.CreateRoutingRule(context.Background(), RoutingRuleXO{
		Name:        "new-rule",
		Description: "New routing rule",
		Mode:        "BLOCK",
		Matchers:    []string{".*SNAPSHOT.*"},
	})

	require.NoError(t, err)
}

func TestUpdateRoutingRule(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(RoutingRuleAPIPath+"/upd-rule", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		var body RoutingRuleXO
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "ALLOW", body.Mode)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.UpdateRoutingRule(context.Background(), "upd-rule", RoutingRuleXO{
		Name:        "upd-rule",
		Description: "Updated rule",
		Mode:        "ALLOW",
		Matchers:    []string{"/releases/.*"},
	})

	require.NoError(t, err)
}

func TestDeleteRoutingRule(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(RoutingRuleAPIPath+"/del-rule", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeleteRoutingRule(context.Background(), "del-rule")

	require.NoError(t, err)
}

func TestDeleteRoutingRule_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(RoutingRuleAPIPath+"/missing", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeleteRoutingRule(context.Background(), "missing")

	assert.ErrorIs(t, err, ErrRoutingRuleNotFound)
}
