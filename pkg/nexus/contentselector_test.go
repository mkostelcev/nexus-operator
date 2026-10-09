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

func TestBuildContentSelectorSpecFromAPI(t *testing.T) {
	cs := ContentSelectorResponse{
		Name:        "test-selector",
		Description: "Test description",
		Expression:  "format == \"maven2\"",
	}

	spec := BuildContentSelectorSpecFromAPI(cs)

	assert.Equal(t, "test-selector", spec.Name)
	assert.Equal(t, "Test description", spec.Description)
	assert.Equal(t, "format == \"maven2\"", spec.Expression)
	assert.IsType(t, v1alpha1.ContentSelectorSpec{}, spec)
}

func TestListContentSelectors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/content-selectors", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := []ContentSelectorResponse{
			{Name: "sel-1", Description: "First", Expression: "format == \"maven2\""},
			{Name: "sel-2", Description: "Second", Expression: "format == \"npm\""},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	selectors, err := client.ListContentSelectors(context.Background())

	require.NoError(t, err)
	require.Len(t, selectors, 2)
	assert.Equal(t, "sel-1", selectors[0].Name)
	assert.Equal(t, "sel-2", selectors[1].Name)
}

func TestGetContentSelector(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/content-selectors/test-sel", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := ContentSelectorResponse{
			Name:        "test-sel",
			Description: "A test selector",
			Expression:  "format == \"raw\"",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	cs, err := client.GetContentSelector(context.Background(), "test-sel")

	require.NoError(t, err)
	require.NotNil(t, cs)
	assert.Equal(t, "test-sel", cs.Name)
	assert.Equal(t, "A test selector", cs.Description)
	assert.Equal(t, "format == \"raw\"", cs.Expression)
}

func TestGetContentSelector_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/content-selectors/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	cs, err := client.GetContentSelector(context.Background(), "missing")

	assert.Nil(t, cs)
	assert.ErrorIs(t, err, ErrContentSelectorNotFound)
}

func TestContentSelectorExists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/content-selectors/exists-sel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/service/rest/v1/security/content-selectors/no-sel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)

	exists, err := client.ContentSelectorExists(context.Background(), "exists-sel")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = client.ContentSelectorExists(context.Background(), "no-sel")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCreateContentSelector(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/content-selectors/new-sel", func(w http.ResponseWriter, r *http.Request) {
		// HEAD check for existence - return 404 so creation proceeds
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/service/rest/v1/security/content-selectors", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]string
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)
			assert.Equal(t, "new-sel", body["name"])
			assert.Equal(t, "New selector", body["description"])
			assert.Equal(t, "format == \"docker\"", body["expression"])
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.CreateContentSelector(context.Background(), "new-sel", "New selector", "format == \"docker\"")

	require.NoError(t, err)
}

func TestUpdateContentSelector(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/content-selectors/upd-sel", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		var body map[string]string
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "Updated desc", body["description"])
		assert.Equal(t, "format == \"npm\"", body["expression"])
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.UpdateContentSelector(context.Background(), "upd-sel", "Updated desc", "format == \"npm\"")

	require.NoError(t, err)
}

func TestDeleteContentSelector(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/content-selectors/del-sel", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeleteContentSelector(context.Background(), "del-sel")

	require.NoError(t, err)
}

func TestDeleteContentSelector_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/service/rest/v1/security/content-selectors/missing", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNotFound)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	err := client.DeleteContentSelector(context.Background(), "missing")

	assert.ErrorIs(t, err, ErrContentSelectorNotFound)
}
