package nexus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSecurityRealmsSpecFromAPI(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		spec := BuildSecurityRealmsSpecFromAPI(nil)
		assert.Nil(t, spec)
	})

	t.Run("empty returns nil", func(t *testing.T) {
		spec := BuildSecurityRealmsSpecFromAPI([]string{})
		assert.Nil(t, spec)
	})

	t.Run("non-empty returns spec with copy", func(t *testing.T) {
		input := []string{"NexusAuthenticatingRealm", "LdapRealm", "DockerToken"}
		spec := BuildSecurityRealmsSpecFromAPI(input)

		require.NotNil(t, spec)
		assert.Equal(t, []string{"NexusAuthenticatingRealm", "LdapRealm", "DockerToken"}, spec.Active)

		// Verify it is a copy, not the same underlying array
		input[0] = "modified"
		assert.Equal(t, "NexusAuthenticatingRealm", spec.Active[0])
	})
}

func TestSecurityRealmsNeedUpdate(t *testing.T) {
	t.Run("same returns false", func(t *testing.T) {
		current := []string{"NexusAuthenticatingRealm", "LdapRealm"}
		desired := []string{"NexusAuthenticatingRealm", "LdapRealm"}

		assert.False(t, SecurityRealmsNeedUpdate(current, desired))
	})

	t.Run("different returns true", func(t *testing.T) {
		current := []string{"NexusAuthenticatingRealm"}
		desired := []string{"NexusAuthenticatingRealm", "LdapRealm"}

		assert.True(t, SecurityRealmsNeedUpdate(current, desired))
	})

	t.Run("different order returns true", func(t *testing.T) {
		current := []string{"LdapRealm", "NexusAuthenticatingRealm"}
		desired := []string{"NexusAuthenticatingRealm", "LdapRealm"}

		assert.True(t, SecurityRealmsNeedUpdate(current, desired))
	})
}

func TestGetActiveRealms(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(SecurityRealmsActiveAPIPath, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		realms := []string{"NexusAuthenticatingRealm", "NexusAuthorizingRealm", "LdapRealm"}
		_ = json.NewEncoder(w).Encode(realms)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	realms, err := client.GetActiveRealms(context.Background())

	require.NoError(t, err)
	require.Len(t, realms, 3)
	assert.Equal(t, "NexusAuthenticatingRealm", realms[0])
	assert.Equal(t, "NexusAuthorizingRealm", realms[1])
	assert.Equal(t, "LdapRealm", realms[2])
}

func TestSetActiveRealms(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(SecurityRealmsActiveAPIPath, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		var body []string
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, []string{"NexusAuthenticatingRealm", "DockerToken"}, body)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.SetActiveRealms(context.Background(), []string{"NexusAuthenticatingRealm", "DockerToken"})

	require.NoError(t, err)
}

func TestGetAvailableRealms(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(SecurityRealmsAvailableAPIPath, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		realms := []RealmInfo{
			{ID: "NexusAuthenticatingRealm", Name: "Local Authenticating Realm"},
			{ID: "LdapRealm", Name: "LDAP Realm"},
			{ID: "DockerToken", Name: "Docker Bearer Token Realm"},
		}
		_ = json.NewEncoder(w).Encode(realms)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	realms, err := client.GetAvailableRealms(context.Background())

	require.NoError(t, err)
	require.Len(t, realms, 3)
	assert.Equal(t, "NexusAuthenticatingRealm", realms[0].ID)
	assert.Equal(t, "Local Authenticating Realm", realms[0].Name)
	assert.Equal(t, "LdapRealm", realms[1].ID)
	assert.Equal(t, "DockerToken", realms[2].ID)
	assert.Equal(t, "Docker Bearer Token Realm", realms[2].Name)
}
