//go:build !integration

package extensionwire

import (
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestToolShapeDistinguishesDefinitionBoundaries checks whether the input mappings preserve tool-definition boundaries.
func TestToolShapeDistinguishesDefinitionBoundaries(t *testing.T) {
	t.Parallel()
	// Arrange tool definitions with identical concatenated text and different fields.
	first := new(pb.ToolDescriptor)
	first.SetName("a\nb")
	first.SetDescription("c")
	second := new(pb.ToolDescriptor)
	second.SetName("a")
	second.SetDescription("b\nc")
	left := new(pb.CompactionToolDefinition)
	left.SetDescriptor(first)
	right := new(pb.CompactionToolDefinition)
	right.SetDescriptor(second)
	require.Equal(
		t,
		decodeToolText([]*pb.CompactionToolDefinition{left}),
		decodeToolText([]*pb.CompactionToolDefinition{right}),
	)
	// Act through both production input mappings.
	invocation := new(pb.CompactionPolicyInvocation)
	invocation.SetTools([]*pb.CompactionToolDefinition{left})
	observation := new(pb.CompletedConversationObservation)
	observation.SetTools([]*pb.CompactionToolDefinition{right})
	captured := DecodeInput(invocation)
	observed := DecodeObservation(observation, DecodeBinding(nil))
	// Assert the observation cannot match a changed definition with the same content estimate.
	require.NotEqual(t, captured.ToolShape, observed.Input.ToolShape)
	observation.SetTools([]*pb.CompactionToolDefinition{left})
	require.Equal(t, captured.ToolShape, DecodeObservation(observation, DecodeBinding(nil)).Input.ToolShape)
}
