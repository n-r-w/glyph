//go:build !integration

package plugin

import (
	"testing"

	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapHostProgressRejectsMalformedLifecycle verifies malformed lifecycle payloads remain visible.
func TestMapHostProgressRejectsMalformedLifecycle(t *testing.T) {
	t.Parallel()

	// Arrange an agent-start event with a forbidden model response.
	malformed := uiv1.AgentEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START), RunId: new("run"), Text: nil,
		ToolCallId: nil, ToolName: nil, ProgressChannel: nil, IsError: nil, Outcome: nil,
		ErrorMessage: nil, Availability: nil, ModelContent: nil,
		ModelResponse: uiv1.ModelResponse_builder{
			Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
			ResponseId: nil, Usage: nil, Diagnostics: nil, Content: nil, ResponseModel: nil,
		}.Build(),
		ToolCallPreview: nil, FinalToolCall: nil, ToolResultContents: nil,
	}.Build()
	progress := new(uiv1.HostProgress)
	progress.SetAgentEvent(malformed)

	// Act through the operation-stream progress mapper.
	_, err := mapHostProgress(progress)

	// Assert the malformed retained payload is rejected with its exact reason.
	require.EqualError(t, err, "lifecycle type 1 has inactive fields 0x800")
}
