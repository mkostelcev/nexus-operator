package nexus

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	client, err := NewClient("http://localhost:8081", "admin", "admin123")
	require.NoError(t, err)
	assert.NotNil(t, client)
	assert.NotNil(t, client.Resty)
	assert.NotNil(t, client.Logger)
	assert.Equal(t, "http://localhost:8081", client.Resty.BaseURL)
}

func TestNewUnexpectedResponseError(t *testing.T) {
	err := NewUnexpectedResponseError(500, "Internal Server Error")

	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnexpectedResponse), "error should wrap ErrUnexpectedResponse")
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, err.Error(), "Internal Server Error")
}
