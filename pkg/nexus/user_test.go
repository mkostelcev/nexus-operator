package nexus

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildUserSpecFromAPI(t *testing.T) {
	t.Run("with populated fields", func(t *testing.T) {
		user := User{
			UserId:       "testuser",
			FirstName:    "Test",
			LastName:     "User",
			EmailAddress: "test@example.com",
			Source:       "default",
			Status:       "active",
			Roles:        []string{"role1", "role2"},
		}

		spec := BuildUserSpecFromAPI(user)

		assert.Equal(t, "testuser", spec.UserId)
		assert.Equal(t, "Test", spec.FirstName)
		assert.Equal(t, "User", spec.LastName)
		assert.Equal(t, "test@example.com", spec.EmailAddress)
		assert.Equal(t, "active", spec.Status)
		assert.Equal(t, []string{"role1", "role2"}, spec.Roles)
	})

	t.Run("nil roles become empty slice", func(t *testing.T) {
		user := User{
			UserId:       "testuser",
			FirstName:    "Test",
			LastName:     "User",
			EmailAddress: "test@example.com",
			Source:       "default",
			Status:       "active",
			Roles:        nil,
		}

		spec := BuildUserSpecFromAPI(user)

		assert.NotNil(t, spec.Roles, "Roles should not be nil")
		assert.Empty(t, spec.Roles)
	})
}

func TestListUsers(t *testing.T) {
	users := []User{
		{UserId: "user1", FirstName: "First", LastName: "User", EmailAddress: "user1@example.com",
			Source: "default", Status: "active", Roles: []string{"role1"}},
		{UserId: "user2", FirstName: "Second", LastName: "User", EmailAddress: "user2@example.com",
			Source: "default", Status: "active", Roles: []string{"role2"}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, UserAPIPath, r.URL.Path)
		assert.Equal(t, "default", r.URL.Query().Get("source"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(users)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	result, err := client.ListUsers(context.Background())

	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "user1", result[0].UserId)
	assert.Equal(t, "user2", result[1].UserId)
}

func TestGetUser(t *testing.T) {
	users := []User{
		{UserId: "testuser", FirstName: "Test", LastName: "User", EmailAddress: "test@example.com",
			Source: "default", Status: "active", Roles: []string{"role1"}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, UserAPIPath, r.URL.Path)
		assert.Equal(t, "testuser", r.URL.Query().Get("userId"))
		assert.Equal(t, "default", r.URL.Query().Get("source"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(users)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	user, err := client.GetUser(context.Background(), "testuser")

	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, "testuser", user.UserId)
	assert.Equal(t, "default", user.Source)
	assert.Equal(t, "Test", user.FirstName)
}

func TestGetUser_NotFound(t *testing.T) {
	// Return an empty array - user not found
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]User{})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	user, err := client.GetUser(context.Background(), "nonexistent")

	assert.Nil(t, user)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

func TestUserExists(t *testing.T) {
	users := []User{
		{UserId: "existing", FirstName: "Existing", LastName: "User", EmailAddress: "e@example.com",
			Source: "default", Status: "active", Roles: []string{}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(users)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	exists, err := client.UserExists(context.Background(), "existing")

	require.NoError(t, err)
	assert.True(t, exists)
}

func TestUserExists_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]User{})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	exists, err := client.UserExists(context.Background(), "nonexistent")

	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCreateUser(t *testing.T) {
	createReq := UserCreateRequest{
		UserId:       "newuser",
		FirstName:    "New",
		LastName:     "User",
		EmailAddress: "new@example.com",
		Password:     "password123",
		Status:       "active",
		Roles:        []string{"role1"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, UserAPIPath, r.URL.Path)

		var body UserCreateRequest
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "newuser", body.UserId)
		assert.Equal(t, "New", body.FirstName)
		assert.Equal(t, "password123", body.Password)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.CreateUser(context.Background(), createReq)

	require.NoError(t, err)
}

func TestUpdateUser(t *testing.T) {
	updateReq := UserUpdateRequest{
		UserId:       "testuser",
		FirstName:    "Updated",
		LastName:     "User",
		EmailAddress: "updated@example.com",
		Status:       "active",
		Roles:        []string{"role1", "role2"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, UserAPIPath+"/testuser", r.URL.Path)

		var body UserUpdateRequest
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "Updated", body.FirstName)
		assert.Equal(t, []string{"role1", "role2"}, body.Roles)

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.UpdateUser(context.Background(), "testuser", updateReq)

	require.NoError(t, err)
}

func TestDeleteUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, UserAPIPath+"/testuser", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeleteUser(context.Background(), "testuser")

	require.NoError(t, err)
}

func TestDeleteUser_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeleteUser(context.Background(), "nonexistent")

	assert.ErrorIs(t, err, ErrUserNotFound)
}

func TestChangeUserPassword(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, UserAPIPath+"/testuser/change-password", r.URL.Path)
		assert.Equal(t, "text/plain", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "newpassword123", string(body))

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.ChangeUserPassword(context.Background(), "testuser", "newpassword123")

	require.NoError(t, err)
}

func TestCheckUserAuth_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/service/rest/v1/status/check", r.URL.Path)

		// Verify the Basic Auth header contains the expected credentials
		authHeader := r.Header.Get("Authorization")
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("checkuser:checkpass"))
		assert.Equal(t, expectedAuth, authHeader)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	result := client.CheckUserAuth(context.Background(), "checkuser", "checkpass")

	assert.True(t, result)
}

func TestCheckUserAuth_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/service/rest/v1/status/check", r.URL.Path)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	result := client.CheckUserAuth(context.Background(), "baduser", "badpass")

	assert.False(t, result)
}
