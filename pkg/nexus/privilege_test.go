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

func TestBuildPrivilegeConfig_Wildcard(t *testing.T) {
	spec := v1alpha1.PrivilegeSpec{
		Name:        "nx-all",
		Type:        "wildcard",
		Description: "All permissions",
		Wildcard: &v1alpha1.WildcardConfig{
			Pattern: "nexus:*",
		},
	}

	config, err := BuildPrivilegeConfig(spec)

	require.NoError(t, err)
	assert.Equal(t, "nx-all", config["name"])
	assert.Equal(t, "wildcard", config["type"])
	assert.Equal(t, "All permissions", config["description"])
	assert.Equal(t, "nexus:*", config["pattern"])
}

func TestBuildPrivilegeConfig_Application(t *testing.T) {
	spec := v1alpha1.PrivilegeSpec{
		Name:        "app-priv",
		Type:        "application",
		Description: "Application privilege",
		Application: &v1alpha1.ApplicationConfig{
			Domain:  "my-domain",
			Actions: []string{"READ", "BROWSE"},
		},
	}

	config, err := BuildPrivilegeConfig(spec)

	require.NoError(t, err)
	assert.Equal(t, "app-priv", config["name"])
	assert.Equal(t, "application", config["type"])
	assert.Equal(t, "my-domain", config["domain"])
	assert.Equal(t, []string{"READ", "BROWSE"}, config["actions"])
}

func TestBuildPrivilegeConfig_RepositoryView(t *testing.T) {
	spec := v1alpha1.PrivilegeSpec{
		Name:        "repo-view-priv",
		Type:        "repository-view",
		Description: "View maven repo",
		RepositoryView: &v1alpha1.RepositoryViewConfig{
			Format:     "maven2",
			Repository: "maven-central",
			Actions:    []string{"READ", "BROWSE"},
		},
	}

	config, err := BuildPrivilegeConfig(spec)

	require.NoError(t, err)
	assert.Equal(t, "repo-view-priv", config["name"])
	assert.Equal(t, "repository-view", config["type"])
	assert.Equal(t, "maven-central", config["repository"])
	assert.Equal(t, []string{"READ", "BROWSE"}, config["actions"])
	assert.Equal(t, "maven2", config["format"])
}

func TestBuildPrivilegeConfig_RepositoryView_DefaultFormat(t *testing.T) {
	spec := v1alpha1.PrivilegeSpec{
		Name: "repo-view-priv",
		Type: "repository-view",
		RepositoryView: &v1alpha1.RepositoryViewConfig{
			Repository: "maven-central",
			Actions:    []string{"READ"},
		},
	}

	config, err := BuildPrivilegeConfig(spec)

	require.NoError(t, err)
	assert.Equal(t, "*", config["format"])
}

func TestBuildPrivilegeConfig_RepositoryAdmin(t *testing.T) {
	spec := v1alpha1.PrivilegeSpec{
		Name:        "repo-admin-priv",
		Type:        "repository-admin",
		Description: "Admin maven repo",
		RepositoryAdmin: &v1alpha1.RepositoryAdminConfig{
			Format:     "docker",
			Repository: "docker-hosted",
			Actions:    []string{"READ", "BROWSE", "EDIT", "ADD", "DELETE"},
		},
	}

	config, err := BuildPrivilegeConfig(spec)

	require.NoError(t, err)
	assert.Equal(t, "repo-admin-priv", config["name"])
	assert.Equal(t, "repository-admin", config["type"])
	assert.Equal(t, "docker-hosted", config["repository"])
	assert.Equal(t, []string{"READ", "BROWSE", "EDIT", "ADD", "DELETE"}, config["actions"])
	assert.Equal(t, "docker", config["format"])
}

func TestBuildPrivilegeConfig_RepositoryAdmin_DefaultFormat(t *testing.T) {
	spec := v1alpha1.PrivilegeSpec{
		Name: "repo-admin-priv",
		Type: "repository-admin",
		RepositoryAdmin: &v1alpha1.RepositoryAdminConfig{
			Repository: "docker-hosted",
			Actions:    []string{"ALL"},
		},
	}

	config, err := BuildPrivilegeConfig(spec)

	require.NoError(t, err)
	assert.Equal(t, "*", config["format"])
}

func TestBuildPrivilegeConfig_MissingConfig(t *testing.T) {
	tests := []struct {
		name        string
		spec        v1alpha1.PrivilegeSpec
		expectedErr error
	}{
		{
			name:        "wildcard without config",
			spec:        v1alpha1.PrivilegeSpec{Name: "p1", Type: "wildcard"},
			expectedErr: ErrWildcardConfigRequired,
		},
		{
			name:        "application without config",
			spec:        v1alpha1.PrivilegeSpec{Name: "p2", Type: "application"},
			expectedErr: ErrApplicationConfigRequired,
		},
		{
			name:        "repository-view without config",
			spec:        v1alpha1.PrivilegeSpec{Name: "p3", Type: "repository-view"},
			expectedErr: ErrRepoViewConfigRequired,
		},
		{
			name:        "repository-admin without config",
			spec:        v1alpha1.PrivilegeSpec{Name: "p4", Type: "repository-admin"},
			expectedErr: ErrRepoAdminConfigRequired,
		},
		{
			name:        "repository-content-selector without config",
			spec:        v1alpha1.PrivilegeSpec{Name: "p5", Type: "repository-content-selector"},
			expectedErr: ErrRepoContentSelConfigRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := BuildPrivilegeConfig(tt.spec)
			assert.Nil(t, config)
			assert.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestBuildPrivilegeSpecFromAPI_Wildcard(t *testing.T) {
	data := map[string]interface{}{
		"name":        "nx-all",
		"type":        "wildcard",
		"description": "All permissions",
		"pattern":     "nexus:*",
	}

	spec := BuildPrivilegeSpecFromAPI(data)

	assert.Equal(t, "nx-all", spec.Name)
	assert.Equal(t, "wildcard", spec.Type)
	assert.Equal(t, "All permissions", spec.Description)
	require.NotNil(t, spec.Wildcard)
	assert.Equal(t, "nexus:*", spec.Wildcard.Pattern)
	assert.Nil(t, spec.Application)
	assert.Nil(t, spec.RepositoryView)
}

func TestBuildPrivilegeSpecFromAPI_RepositoryView(t *testing.T) {
	data := map[string]interface{}{
		"name":        "repo-view",
		"type":        "repository-view",
		"description": "View repo",
		"repository":  "maven-central",
		"format":      "maven2",
		"actions":     []interface{}{"READ", "BROWSE"},
	}

	spec := BuildPrivilegeSpecFromAPI(data)

	assert.Equal(t, "repo-view", spec.Name)
	assert.Equal(t, "repository-view", spec.Type)
	require.NotNil(t, spec.RepositoryView)
	assert.Equal(t, "maven-central", spec.RepositoryView.Repository)
	assert.Equal(t, "maven2", spec.RepositoryView.Format)
	assert.Equal(t, []string{"READ", "BROWSE"}, spec.RepositoryView.Actions)
	assert.Nil(t, spec.Wildcard)
}

func TestBuildPrivilegeSpecFromAPI_RepositoryView_DefaultFormat(t *testing.T) {
	data := map[string]interface{}{
		"name":       "repo-view",
		"type":       "repository-view",
		"repository": "maven-central",
		"actions":    []interface{}{"READ"},
	}

	spec := BuildPrivilegeSpecFromAPI(data)

	require.NotNil(t, spec.RepositoryView)
	assert.Equal(t, "*", spec.RepositoryView.Format)
}

func TestBuildPrivilegeSpecFromAPI_RepositoryAdmin(t *testing.T) {
	data := map[string]interface{}{
		"name":        "repo-admin",
		"type":        "repository-admin",
		"description": "Admin repo",
		"repository":  "docker-hosted",
		"format":      "docker",
		"actions":     []interface{}{"READ", "BROWSE", "EDIT", "ADD", "DELETE"},
	}

	spec := BuildPrivilegeSpecFromAPI(data)

	assert.Equal(t, "repo-admin", spec.Name)
	assert.Equal(t, "repository-admin", spec.Type)
	require.NotNil(t, spec.RepositoryAdmin)
	assert.Equal(t, "docker-hosted", spec.RepositoryAdmin.Repository)
	assert.Equal(t, "docker", spec.RepositoryAdmin.Format)
	assert.Equal(t, []string{"READ", "BROWSE", "EDIT", "ADD", "DELETE"}, spec.RepositoryAdmin.Actions)
}

func TestBuildPrivilegeSpecFromAPI_RepositoryAdmin_DefaultFormat(t *testing.T) {
	data := map[string]interface{}{
		"name":       "repo-admin",
		"type":       "repository-admin",
		"repository": "docker-hosted",
	}

	spec := BuildPrivilegeSpecFromAPI(data)

	require.NotNil(t, spec.RepositoryAdmin)
	assert.Equal(t, "*", spec.RepositoryAdmin.Format)
}

func TestListPrivileges(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/privileges", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		items := []PrivilegeListItem{
			{Type: "wildcard", Name: "nx-all", Description: "All", ReadOnly: false},
			{Type: "repository-view", Name: "repo-view", Description: "View", ReadOnly: true},
		}
		_ = json.NewEncoder(w).Encode(items)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	privs, err := client.ListPrivileges(context.Background())

	require.NoError(t, err)
	require.Len(t, privs, 2)
	assert.Equal(t, "nx-all", privs[0].Name)
	assert.Equal(t, "wildcard", privs[0].Type)
	assert.Equal(t, "repo-view", privs[1].Name)
	assert.True(t, privs[1].ReadOnly)
}

func TestGetPrivilege(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/privileges/nx-all", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		data := map[string]interface{}{
			"name":        "nx-all",
			"type":        "wildcard",
			"description": "All permissions",
			"pattern":     "nexus:*",
			"readOnly":    false,
		}
		_ = json.NewEncoder(w).Encode(data)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	priv, err := client.GetPrivilege(context.Background(), "nx-all")

	require.NoError(t, err)
	require.NotNil(t, priv)
	assert.Equal(t, "nx-all", priv["name"])
	assert.Equal(t, "wildcard", priv["type"])
	assert.Equal(t, "nexus:*", priv["pattern"])
}

func TestGetPrivilege_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/privileges/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	priv, err := client.GetPrivilege(context.Background(), "missing")

	assert.Nil(t, priv)
	assert.ErrorIs(t, err, ErrPrivilegeNotFound)
}

func TestCreatePrivilege_Wildcard(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/privileges/wildcard", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		var body map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "nx-all", body["name"])
		assert.Equal(t, "wildcard", body["type"])
		assert.Equal(t, "nexus:*", body["pattern"])
		w.WriteHeader(http.StatusCreated)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	config := map[string]interface{}{
		"name":        "nx-all",
		"type":        "wildcard",
		"description": "All permissions",
		"pattern":     "nexus:*",
	}
	err := client.CreatePrivilege(context.Background(), config)

	require.NoError(t, err)
}

func TestUpdatePrivilege(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/privileges/wildcard/nx-all", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		var body map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "Updated description", body["description"])
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	config := map[string]interface{}{
		"name":        "nx-all",
		"type":        "wildcard",
		"description": "Updated description",
		"pattern":     "nexus:*",
	}
	err := client.UpdatePrivilege(context.Background(), "nx-all", config)

	require.NoError(t, err)
}

func TestDeletePrivilege(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/privileges/del-priv", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeletePrivilege(context.Background(), "del-priv")

	require.NoError(t, err)
}

func TestDeletePrivilege_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/privileges/missing", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeletePrivilege(context.Background(), "missing")

	assert.ErrorIs(t, err, ErrPrivilegeNotFound)
}
