//go:build !integration

package uiv1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestServeRejectsMissingService verifies public startup rejects invalid composition before serving.
func TestServeRejectsMissingService(t *testing.T) {
	t.Parallel()
	// Arrange a missing service at the public startup boundary.
	// Act and assert validation does not terminate the process or start serving.
	require.ErrorContains(t, Serve(nil), "UI service is required")
	service := NewMockService(gomock.NewController(t))
	server, err := newServer(service)
	require.NoError(t, err)
	require.NotNil(t, server)
}

// TestNewServerRejectsMissingService verifies the server validates its supplied dependency.
func TestNewServerRejectsMissingService(t *testing.T) {
	t.Parallel()
	// Arrange a missing service.
	// Act and assert construction returns without panic.
	server, err := newServer(nil)
	require.ErrorContains(t, err, "UI service is required")
	require.Nil(t, server)
	service := NewMockService(gomock.NewController(t))
	server, err = newServer(service)
	require.NoError(t, err)
	require.NotNil(t, server)
}
