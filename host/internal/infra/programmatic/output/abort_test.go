//go:build !integration

package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSettlementClearsActiveCorrelation verifies settlement permits another output reservation.
func TestSettlementClearsActiveCorrelation(t *testing.T) {
	t.Parallel()

	// Arrange one reserved active run.
	delivery := New()
	require.True(t, delivery.Reserve("operation", "run"))

	// Act by reporting Agent Core settlement.
	err := delivery.DeliverSettled(t.Context(), "run")

	// Assert settlement clears active state without a public progress event.
	require.NoError(t, err)
	assert.Empty(t, delivery.ActiveOperation())
	require.True(t, delivery.Reserve("next-operation", "next-run"))
}
