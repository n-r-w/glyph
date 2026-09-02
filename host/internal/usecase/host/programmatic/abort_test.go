//go:build !integration

package programmatic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
)

// TestSettlementDoesNotEmitPublicProgress verifies operation terminal delivery replaces a settlement progress event.
func TestSettlementDoesNotEmitPublicProgress(t *testing.T) {
	t.Parallel()

	// Arrange one reserved active run.
	delivery := NewDelivery()
	active := newTestActiveRun(t.Context(), delivery, "operation", "run")
	require.True(t, delivery.reserve(active))

	// Act by reporting Agent Core settlement.
	err := delivery.DeliverSettled(t.Context(), "run")

	// Assert settlement clears active state without a public progress event.
	require.NoError(t, err)
	assert.Nil(t, delivery.activeSnapshot())
	select {
	case event := <-active.Events():
		assert.Equal(t, controller.AgentEventUnspecified, event.Type)
	default:
	}
}
