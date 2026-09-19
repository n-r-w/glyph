//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
)

// TestResponseResetClearsOnlyUnfinishedModelState verifies replacement attempts retain transcript and completed tools.
func TestResponseResetClearsOnlyUnfinishedModelState(t *testing.T) {
	t.Parallel()

	// Arrange completed transcript and tool execution beside unfinished response state.
	state := projection{
		Startup:    nil,
		Transcript: []Line{NewTextLine(LineUser, mo.Some("question"))},
		ActiveModel: map[int]ActiveModelContent{0: {
			Kind: mo.Some(ModelContentText), Text: mo.Some("partial"),
		}},
		ActiveToolCalls: map[string]ToolCallState{"pending": {
			CallID: "pending", Name: "read", Position: 0, Provisional: true,
			Fields: nil, ArgumentsJSON: nil,
		}},
		ActiveTools:  map[string]string{"completed-tool": "read"},
		RetryStatus:  "",
		Availability: mo.Some(AvailabilityRunning), AuthorizationURL: mo.None[string](),
		AuthorizationCode: mo.None[string](), Settled: mo.Some(false), Models: nil,
		ModelSelection: mo.None[ModelSelection](), SessionInfo: mo.None[SessionInfo](), Sessions: nil,
	}

	// Act by applying the semantic reset delivered before a replacement attempt.
	actual := state.Apply(newEvent(eventResponseReset))

	// Assert only unfinished response content and provisional calls are removed.
	assert.Empty(t, actual.ActiveModel)
	assert.Empty(t, actual.ActiveToolCalls)
	assert.Equal(t, map[string]string{"completed-tool": "read"}, actual.ActiveTools)
	assert.Equal(t, state.Transcript, actual.Transcript)
}

// TestRetryProgressPublishesActiveStatus verifies retry progress remains visible until replacement content starts.
func TestRetryProgressPublishesActiveStatus(t *testing.T) {
	t.Parallel()

	// Arrange an empty responsive projection.
	state := projection{
		Startup: nil, Transcript: nil, ActiveModel: nil, ActiveToolCalls: nil, ActiveTools: nil,
		RetryStatus: "", Availability: mo.Some(AvailabilityRunning), AuthorizationURL: mo.None[string](),
		AuthorizationCode: mo.None[string](), Settled: mo.Some(false), Models: nil,
		ModelSelection: mo.None[ModelSelection](), SessionInfo: mo.None[SessionInfo](), Sessions: nil,
	}
	progress := newEvent(eventRetryProgress)
	progress.Status = mo.Some("Retry 2/4 in 1s")

	// Act by applying retry progress and then replacement model content.
	withProgress := state.Apply(progress)
	delta := newEvent(eventModelDelta)
	delta.Position = mo.Some(0)
	delta.ModelContentKind = mo.Some(ModelContentText)
	delta.Text = mo.Some("replacement")
	withReplacement := withProgress.Apply(delta)
	toolCall := newEvent(eventToolCallPreview)
	toolCall.ToolCall = mo.Some(ToolCallState{
		CallID: "replacement-tool", Name: "read", Position: 0, Provisional: true,
		Fields: nil, ArgumentsJSON: nil,
	})
	withToolReplacement := withProgress.Apply(toolCall)

	// Assert progress clears when replacement text or a replacement tool call starts.
	assert.Equal(t, "Retry 2/4 in 1s", withProgress.RetryStatus)
	assert.Empty(t, withReplacement.RetryStatus)
	assert.Empty(t, withToolReplacement.RetryStatus)
}
