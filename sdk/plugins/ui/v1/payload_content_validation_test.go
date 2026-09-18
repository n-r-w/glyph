//go:build !integration

package uiv1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestValidateFinalToolCallPreservesArgumentShapeCause verifies public validation retains JSON decoder detail.
func TestValidateFinalToolCallPreservesArgumentShapeCause(t *testing.T) {
	t.Parallel()

	// Arrange a complete public tool call with a top-level array argument value.
	call := uiv1.FinalToolCall_builder{
		CallId: new("call"), Name: new("tool"), Position: new(int64(0)), ArgumentsJson: []byte(`[]`),
	}.Build()

	// Act by validating the finalized public payload.
	err := validateFinalToolCall(call)

	// Assert the complete map-shape decoder cause remains available.
	require.Error(t, err)
	assert.ErrorContains(t, err, "unmarshal JSON array")
	assert.ErrorContains(t, err, "into Go map[string]interface {}")
}
