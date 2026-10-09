package nexus

import (
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(server.URL, "admin", "admin123")
	if err != nil {
		t.Fatalf("failed to create test client: %v", err)
	}
	return c
}
