//go:build !integration

package plugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestOperationMappersRejectUnknownLifecycleAndMapSafeError verifies malformed progress and safe errors.
func TestOperationMappersRejectUnknownLifecycleAndMapSafeError(t *testing.T) {
	t.Parallel()

	// Arrange unknown lifecycle progress and one classified connection error.
	unknownProgress := new(uiv1.HostProgress)
	unknownProgress.SetAgentEvent(new(uiv1.AgentEvent))
	errorPayload := uiv1.Error_builder{Code: new("INTERNAL"), Text: new("safe error")}.Build()
	connection := new(uiv1.HostConnectionEvent)
	connection.SetError(errorPayload)

	// Act through the operation-stream mappers.
	_, unknownErr := mapHostProgress(unknownProgress)
	event, err := DecodeConnectionEvent(connection)

	// Assert malformed lifecycle fails while safe error text remains visible.
	require.Error(t, unknownErr)
	require.NoError(t, err)
	assert.Equal(t, TextPayload(TextUpdate{FailureCode: "INTERNAL", Kind: TextError, Text: "safe error"}), event)
}

// TestMapLifecycleRejectsEmptyToolResultContents verifies missing terminal output fails at the UI boundary.
func TestMapLifecycleRejectsEmptyToolResultContents(t *testing.T) {
	t.Parallel()
	// Arrange the inline payload for DecodeLifecycle to verify missing terminal output fails at the UI boundary.

	// Act by invoking DecodeLifecycle to exercise missing terminal output fails at the UI boundary.
	_, err := DecodeLifecycle(uiv1.AgentEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		RunId:              new("run"),
		Text:               nil,
		ToolCallId:         new("call"),
		ToolName:           new("tool"),
		ProgressChannel:    nil,
		IsError:            new(false),
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	// Assert missing terminal output fails at the UI boundary.
	require.ErrorContains(t, err, "tool result contents are empty")
}

// TestMapLifecycleRejectsMissingToolResultContent verifies malformed blocks fail at the UI boundary.
func TestMapLifecycleRejectsMissingToolResultContent(t *testing.T) {
	t.Parallel()
	// Arrange the inline payload for DecodeLifecycle to verify malformed blocks fail at the UI boundary.

	// Act by invoking DecodeLifecycle to exercise malformed blocks fail at the UI boundary.
	_, err := DecodeLifecycle(uiv1.AgentEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		ToolResultContents: []*uiv1.ToolResultContent{
			uiv1.ToolResultContent_builder{}.Build(),
		},
		RunId:           new("run"),
		Text:            nil,
		ToolCallId:      new("call"),
		ToolName:        new("tool"),
		ProgressChannel: nil,
		IsError:         new(false),
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent:    nil,
		ModelResponse:   nil,
		ToolCallPreview: nil,
		FinalToolCall:   nil,
	}.Build())
	// Assert malformed blocks fail at the UI boundary.
	require.ErrorContains(t, err, "tool result content 0 is missing")
}

// TestMapLifecycleRejectsEmptyToolResultImage prevents empty image payloads from reaching presentation.
func TestMapLifecycleRejectsEmptyToolResultImage(t *testing.T) {
	t.Parallel()
	// Arrange a tool result image without its required media type or data.
	// Act by passing the malformed lifecycle event to DecodeLifecycle.
	// Assert mapping fails before the empty image reaches presentation state.

	_, err := DecodeLifecycle(uiv1.AgentEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		ToolResultContents: []*uiv1.ToolResultContent{
			//nolint:exhaustruct_v5 // uiv1.ToolResultContent_builder sets only the active Image field.
			uiv1.ToolResultContent_builder{
				Image: uiv1.ToolResultImage_builder{
					MediaType: new("image/png"),
					Data:      nil,
				}.Build(),
			}.Build(),
		},
		RunId:           new("run"),
		Text:            nil,
		ToolCallId:      new("call"),
		ToolName:        new("tool"),
		ProgressChannel: nil,
		IsError:         new(false),
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent:    nil,
		ModelResponse:   nil,
		ToolCallPreview: nil,
		FinalToolCall:   nil,
	}.Build())
	require.ErrorContains(t, err, "tool result image 0 is invalid")
}
