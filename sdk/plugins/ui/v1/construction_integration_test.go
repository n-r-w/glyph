//go:build integration

package uiv1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestServer constructs a validated server for stream contract tests.
func newTestServer(t *testing.T, service Service) *server {
	t.Helper()
	server, err := newServer(service)
	require.NoError(t, err)
	return server
}
