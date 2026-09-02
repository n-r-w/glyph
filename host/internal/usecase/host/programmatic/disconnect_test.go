//go:build !integration

package programmatic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
)

// TestPrepareRejectsMissingOperationIdentifier verifies bounded identifier validation.
func TestPrepareRejectsMissingOperationIdentifier(t *testing.T) {
	t.Parallel()

	// Arrange a service and command without an operation identifier.
	service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
	command := testProgrammaticCommand("", controller.CommandGetRunState)

	// Act by preparing the operation.
	_, err := service.Prepare(t.Context(), command)

	// Assert preparation fails before domain work starts.
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOperationIDRequired)
}
