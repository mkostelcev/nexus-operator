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

func TestBuildRoleConfig(t *testing.T) {
	spec := v1alpha1.RoleSpec{
		RoleID:      "test-role",
		Name:        "Test Role",
		Description: "A test role",
		Privileges:  []string{"nx-repository-view-*-*-read"},
		Roles:       []string{"nx-anonymous"},
	}

	role := BuildRoleConfig(spec)

	assert.Equal(t, "test-role", role.ID)
	assert.Equal(t, "Test Role", role.Name)
	assert.Equal(t, "A test role", role.Description)
	assert.Equal(t, []string{"nx-repository-view-*-*-read"}, role.Privileges)
	assert.Equal(t, []string{"nx-anonymous"}, role.Roles)
}

func TestBuildRoleSpecFromAPI(t *testing.T) {
	t.Run("with populated fields", func(t *testing.T) {
		role := Role{
			ID:          "test-role",
			Name:        "Test Role",
			Description: "A test role",
			Privileges:  []string{"priv1", "priv2"},
			Roles:       []string{"role1"},
		}

		spec := BuildRoleSpecFromAPI(role)

		assert.Equal(t, "test-role", spec.RoleID)
		assert.Equal(t, "Test Role", spec.Name)
		assert.Equal(t, "A test role", spec.Description)
		assert.Equal(t, []string{"priv1", "priv2"}, spec.Privileges)
		assert.Equal(t, []string{"role1"}, spec.Roles)
	})

	t.Run("nil privileges and roles become empty slices", func(t *testing.T) {
		role := Role{
			ID:          "empty-role",
			Name:        "Empty Role",
			Description: "",
			Privileges:  nil,
			Roles:       nil,
		}

		spec := BuildRoleSpecFromAPI(role)

		assert.Equal(t, "empty-role", spec.RoleID)
		assert.Equal(t, "Empty Role", spec.Name)
		assert.NotNil(t, spec.Privileges, "Privileges should not be nil")
		assert.NotNil(t, spec.Roles, "Roles should not be nil")
		assert.Empty(t, spec.Privileges)
		assert.Empty(t, spec.Roles)
	})
}

func TestListRoles(t *testing.T) {
	roles := []Role{
		{ID: "role1", Name: "Role 1", Description: "First role", Privileges: []string{"priv1"}, Roles: []string{}},
		{ID: "role2", Name: "Role 2", Description: "Second role", Privileges: []string{}, Roles: []string{"role1"}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, RoleAPIPath, r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(roles)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	result, err := client.ListRoles(context.Background())

	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "role1", result[0].ID)
	assert.Equal(t, "role2", result[1].ID)
}

func TestGetRole(t *testing.T) {
	expectedRole := Role{
		ID:          "test-role",
		Name:        "Test Role",
		Description: "A role for testing",
		Privileges:  []string{"priv1"},
		Roles:       []string{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, RoleAPIPath+"/test-role", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(expectedRole)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	role, err := client.GetRole(context.Background(), "test-role")

	require.NoError(t, err)
	require.NotNil(t, role)
	assert.Equal(t, "test-role", role.ID)
	assert.Equal(t, "Test Role", role.Name)
	assert.Equal(t, "A role for testing", role.Description)
}

func TestGetRole_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	role, err := client.GetRole(context.Background(), "nonexistent")

	assert.Nil(t, role)
	assert.ErrorIs(t, err, ErrRoleNotFound)
}

func TestRoleExists(t *testing.T) {
	expectedRole := Role{
		ID:   "existing-role",
		Name: "Existing Role",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(expectedRole)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	exists, err := client.RoleExists(context.Background(), "existing-role")

	require.NoError(t, err)
	assert.True(t, exists)
}

func TestRoleExists_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	exists, err := client.RoleExists(context.Background(), "nonexistent")

	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCreateRole(t *testing.T) {
	role := Role{
		ID:          "new-role",
		Name:        "New Role",
		Description: "A new role",
		Privileges:  []string{"priv1"},
		Roles:       []string{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc(RoleAPIPath+"/new-role", func(w http.ResponseWriter, r *http.Request) {
		// GET /roles/{id} - RoleExists check (should return 404 = not found)
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc(RoleAPIPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body Role
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)
			assert.Equal(t, "new-role", body.ID)
			assert.Equal(t, "New Role", body.Name)

			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.CreateRole(context.Background(), role)

	require.NoError(t, err)
}

func TestCreateRole_AlreadyExists(t *testing.T) {
	existingRole := Role{
		ID:   "existing-role",
		Name: "Existing Role",
	}

	mux := http.NewServeMux()
	mux.HandleFunc(RoleAPIPath+"/existing-role", func(w http.ResponseWriter, r *http.Request) {
		// GET /roles/{id} - RoleExists check returns 200 (role exists)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(existingRole)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.CreateRole(context.Background(), existingRole)

	assert.ErrorIs(t, err, ErrRoleAlreadyExists)
}

func TestUpdateRole(t *testing.T) {
	role := Role{
		ID:          "update-role",
		Name:        "Updated Role",
		Description: "Updated description",
		Privileges:  []string{"priv1", "priv2"},
		Roles:       []string{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, RoleAPIPath+"/update-role", r.URL.Path)

		var body Role
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "Updated Role", body.Name)
		assert.Equal(t, "Updated description", body.Description)

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.UpdateRole(context.Background(), "update-role", role)

	require.NoError(t, err)
}

func TestDeleteRole(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, RoleAPIPath+"/delete-role", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeleteRole(context.Background(), "delete-role")

	require.NoError(t, err)
}

func TestDeleteRole_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeleteRole(context.Background(), "nonexistent")

	assert.ErrorIs(t, err, ErrRoleNotFound)
}
