//nolint:exhaustruct // Tests set only fields used by each projection event and line kind.
package presentation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestServiceAppliesInitializationAndLifecycleWithoutOwningHostState verifies ordered projection only.
func TestServiceAppliesInitializationAndLifecycleWithoutOwningHostState(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{
			{Kind: presentationdomain.LineInformation, Text: "Glyph session initialized."},
			{Kind: presentationdomain.LineError, Text: "Optional extension is unavailable."},
		},
		Availability: presentationdomain.AvailabilityIdle,
		Extensions: []presentationdomain.Extension{
			{ID: "tools", Tools: []string{"read", "edit"}},
		},
	})

	require.Len(t, state.Startup, 2)
	assert.Equal(t, presentationdomain.Line{Kind: presentationdomain.LineInformation, Text: "Glyph session initialized."}, state.Startup[0])
	assert.Equal(t, presentationdomain.LineError, state.Startup[1].Kind)
	assert.Equal(t, presentationdomain.AvailabilityIdle, state.Availability)

	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventModelDelta, Position: 1, ModelContentKind: presentationdomain.ModelContentText, Text: "Hel"})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventModelDelta, Position: 1, ModelContentKind: presentationdomain.ModelContentText, Text: "lo"})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventModelDelta, Position: 0, ModelContentKind: presentationdomain.ModelContentText, Text: "First"})
	assert.Equal(t, map[int]presentationdomain.ActiveModelContent{
		0: {Kind: presentationdomain.ModelContentText, Text: "First"},
		1: {Kind: presentationdomain.ModelContentText, Text: "Hello"},
	}, state.ActiveModel)

	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{Kind: presentationdomain.ModelContentText, Text: "Hello"}},
	})
	assert.Equal(t, []presentationdomain.Line{{Kind: presentationdomain.LineModel, Text: "Hello"}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)

	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventToolStarted, ToolCallID: "call-1", ToolName: "read", Status: "thinking", Text: "reading"})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventToolProgress, Status: "in_progress", Text: "working"})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventToolOutput, Stream: presentationdomain.OutputStdout, Text: "content"})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventToolOutput, Stream: presentationdomain.OutputStderr, Text: "warning"})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventToolEnded, ToolName: "read", Status: "completed", Text: "done"})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventToolResult, ToolName: "read", Text: "result", ExitCode: 0})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventToolResult, ToolName: "edit", Text: "denied", Failure: true})

	assert.Equal(t, []presentationdomain.Line{
		{Kind: presentationdomain.LineModel, Text: "Hello"},
		{Kind: presentationdomain.LineToolStatus, ToolName: "read", Status: "thinking", Text: "reading"},
		{Kind: presentationdomain.LineToolStatus, ToolName: "read", Status: "in_progress", Text: "working"},
		{Kind: presentationdomain.LineToolStdout, ToolName: "read", Text: "content"},
		{Kind: presentationdomain.LineToolStderr, ToolName: "read", Text: "warning"},
		{Kind: presentationdomain.LineToolDone, ToolName: "read", Status: "completed"},
		{Kind: presentationdomain.LineToolDone, ToolName: "read", Text: "result"},
		{Kind: presentationdomain.LineToolError, ToolName: "edit", Text: "denied"},
	}, state.Transcript)
}

// TestServiceModelEndFinalizesCompleteMessageAcrossStreamPositions verifies one complete terminal model line.
func TestServiceReplacesProvisionalToolCallBeforeExecutionStart(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventToolCallPreview,
		ToolCall: presentationdomain.ToolCallState{
			CallID: "call-1", Name: "read", Position: 1, Provisional: true,
			Fields: []presentationdomain.ToolCallField{{Name: "path", Prefix: "fi"}},
		},
	})
	require.True(t, state.ActiveToolCalls["call-1"].Provisional)
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventToolCallFinal,
		ToolCall: presentationdomain.ToolCallState{
			CallID: "call-1", Name: "read", Position: 1, Provisional: false,
			Arguments: map[string]any{"path": "file.txt"},
		},
	})
	require.False(t, state.ActiveToolCalls["call-1"].Provisional)
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd, Status: "tool_use",
	})
	require.Contains(t, state.ActiveToolCalls, "call-1")
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventToolStarted, ToolCallID: "call-1", ToolName: "read", Status: "started",
	})
	require.Len(t, state.Transcript, 2)
	require.Contains(t, state.Transcript[0].Text, "file.txt")
	require.Equal(t, "started", state.Transcript[1].Status)
}

func TestServiceModelEndFinalizesCompleteMessageAcrossStreamPositions(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventModelDelta, Position: 0,
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelDelta, Position: 1, Text: "complete answer",
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd, Position: 0,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{Kind: presentationdomain.ModelContentText, Text: "complete answer"}},
	})

	assert.Equal(t, []presentationdomain.Line{{Kind: presentationdomain.LineModel, Text: "complete answer"}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServicePreservesFinalizedRefusalBlocks verifies mixed public model content keeps its semantic kind.
func TestServicePreservesFinalizedRefusalBlocks(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventModelDelta, Position: 0,
		ModelContentKind: presentationdomain.ModelContentText, Text: "draft",
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{
			{Kind: presentationdomain.ModelContentText, Text: "answer"},
			{Kind: presentationdomain.ModelContentRefusal, Text: "cannot help"},
		},
	})

	assert.Equal(t, []presentationdomain.Line{
		{Kind: presentationdomain.LineModel, Text: "answer"},
		{Kind: presentationdomain.LineRefusal, Text: "cannot help"},
	}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServiceEmptyModelEndClearsStaleFragmentsWithoutTranscriptLine verifies tool-only model cleanup.
func TestServiceEmptyModelEndClearsStaleFragmentsWithoutTranscriptLine(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventModelDelta, Position: 1, Text: "stale fragment",
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd, Position: 0,
	})

	assert.Empty(t, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServiceAssignsToolCompletionStatusAndResultContentOnce verifies distinct terminal payload owners.
func TestServiceAssignsToolCompletionStatusAndResultContentOnce(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventToolEnded, ToolName: "read", Status: "completed", Text: "result",
	})
	state = service.Apply(state, presentationdomain.Event{
		Kind: presentationdomain.EventToolResult, ToolName: "read", Text: "result",
	})

	assert.Equal(t, []presentationdomain.Line{
		{Kind: presentationdomain.LineToolDone, ToolName: "read", Status: "completed"},
		{Kind: presentationdomain.LineToolDone, ToolName: "read", Text: "result"},
	}, state.Transcript)
}

// TestServiceRendersOneSafeErrorAcrossTerminalLifecycleEvents verifies layered failures are not duplicated.
func TestServiceRendersOneSafeErrorAcrossTerminalLifecycleEvents(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventModelDelta, Position: 1, Text: "partial",
	})
	for _, event := range []presentationdomain.Event{
		{Kind: presentationdomain.EventModelEnd, Failure: true, ErrorText: "Provider failed."},
		{Kind: presentationdomain.EventTurnEnded, Failure: true, ErrorText: "Provider failed."},
		{Kind: presentationdomain.EventAgentSettled, Failure: true, Text: "Provider failed."},
		{Kind: presentationdomain.EventError, Text: "Provider failed."},
	} {
		state = service.Apply(state, event)
	}

	assert.Equal(t, []presentationdomain.Line{{Kind: presentationdomain.LineError, Text: "Provider failed."}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
	assert.True(t, state.Settled)
}

// TestServiceRetainsTranscriptAcrossSettlementAndSecondTurn verifies multi-turn transcript continuity.
func TestServiceRetainsTranscriptAcrossSettlementAndSecondTurn(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{Kind: presentationdomain.EventInitialization, Availability: presentationdomain.AvailabilityIdle})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventAvailability, Availability: presentationdomain.AvailabilityRunning})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{Kind: presentationdomain.ModelContentText, Text: "first response"}},
	})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventAgentSettled, Text: "completed"})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventAvailability, Availability: presentationdomain.AvailabilityIdle})
	state = service.Apply(state, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{Kind: presentationdomain.ModelContentText, Text: "second response"}},
	})

	assert.Equal(t, presentationdomain.AvailabilityIdle, state.Availability)
	assert.True(t, state.Settled)
	assert.Equal(t, []presentationdomain.Line{
		{Kind: presentationdomain.LineModel, Text: "first response"},
		{Kind: presentationdomain.LineModel, Text: "second response"},
	}, state.Transcript)
}

// TestServiceCopiesTypedToolResultImage verifies presentation state owns image bytes.
func TestServiceCopiesTypedToolResultImage(t *testing.T) {
	t.Parallel()

	service := New()
	content := presentationdomain.ToolResultContent{MediaType: "image/png", Data: []byte{1, 2, 3}}
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventToolResult, ToolName: "read", ToolResultContents: []presentationdomain.ToolResultContent{content},
	})
	content.Data[0] = 9

	require.Len(t, state.Transcript, 1)
	assert.Equal(t, []byte{1, 2, 3}, state.Transcript[0].ToolResultContents[0].Data)
}

// TestServiceProjectsTypedToolResultTextInOrder verifies readable ordered terminal output.
func TestServiceProjectsTypedToolResultTextInOrder(t *testing.T) {
	t.Parallel()

	state := New().Apply(presentationdomain.State{}, presentationdomain.Event{
		Kind: presentationdomain.EventToolResult, ToolName: "read",
		ToolResultContents: []presentationdomain.ToolResultContent{
			{Text: "first"},
			{MediaType: "image/png", Data: []byte{1, 2, 3}},
			{Text: "last"},
		},
	})

	require.Len(t, state.Transcript, 1)
	assert.Equal(t, "first\n[image: image/png]\nlast", state.Transcript[0].Text)
}

// TestServiceProjectsAuthorizationInformationAndSafeErrors verifies standalone Host frames remain visible.
func TestServiceProjectsAuthorizationInformationAndSafeErrors(t *testing.T) {
	t.Parallel()

	service := New()
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{Kind: presentationdomain.EventAuthorization, Text: "https://example.test/oauth"})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventInformation, Text: "Open the authorization URL."})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventError, Text: "Authentication failed."})
	state = service.Apply(state, presentationdomain.Event{Kind: presentationdomain.EventAvailability, Availability: presentationdomain.AvailabilityAuthenticationFailed})

	assert.Equal(t, "https://example.test/oauth", state.AuthorizationURL)
	assert.Equal(t, presentationdomain.AvailabilityAuthenticationFailed, state.Availability)
	assert.Equal(t, []presentationdomain.Line{
		{Kind: presentationdomain.LineInformation, Text: "Open the authorization URL."},
		{Kind: presentationdomain.LineError, Text: "Authentication failed."},
	}, state.Transcript)
}
