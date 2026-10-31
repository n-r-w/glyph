//go:build !integration

package extensionv1

import (
	"testing"

	"github.com/stretchr/testify/require"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestContextMiddlewareHandlerAdmission accepts only the payload declared by each instruction, context, input or
// finalized-message handler.
func TestContextMiddlewareHandlerAdmission(t *testing.T) {
	t.Parallel()
	// Arrange the system-instructions, model-context, user-input and finalized-message payloads.
	//nolint:exhaustruct_v5 // Each protobuf oneof builder sets only its active payload.
	requests := []*extensionpb.HandleRequest{
		extensionpb.HandleRequest_builder{
			SystemInstructions: extensionpb.SystemInstructionsInvocation_builder{}.Build(),
		}.Build(),
		extensionpb.HandleRequest_builder{ModelContext: extensionpb.ModelContextInvocation_builder{}.Build()}.Build(),
		extensionpb.HandleRequest_builder{UserInput: extensionpb.UserInputInvocation_builder{}.Build()}.Build(),
		extensionpb.HandleRequest_builder{
			FinalizedMessage: extensionpb.FinalizedMessageInvocation_builder{}.Build(),
		}.Build(),
	}
	kinds := []extensionpb.HandlerKind{
		extensionpb.HandlerKind_HANDLER_KIND_SYSTEM_INSTRUCTIONS,
		extensionpb.HandlerKind_HANDLER_KIND_MODEL_CONTEXT,
		extensionpb.HandlerKind_HANDLER_KIND_USER_INPUT,
		extensionpb.HandlerKind_HANDLER_KIND_FINALIZED_MESSAGE,
	}
	// Act by checking every kind against every payload.
	matches := make([][]bool, len(kinds))
	for i, kind := range kinds {
		for _, request := range requests {
			matches[i] = append(matches[i], handlerKindMatches(kind, request))
		}
	}

	// Assert matching payload admission and mismatched payload rejection.
	require.Equal(t, [][]bool{
		{
			true,
			false,
			false,
			false,
		},
		{false, true, false, false},
		{false, false, true, false},
		{false, false, false, true},
	}, matches)
}
