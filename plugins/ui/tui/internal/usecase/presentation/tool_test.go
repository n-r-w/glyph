//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStateCopiesTypedToolResultImage verifies presentation state owns image bytes.
func TestStateCopiesTypedToolResultImage(t *testing.T) {
	t.Parallel()

	// Arrange typed tool-result image content with caller-owned bytes.
	content := Content{
		MediaType: mo.Some("image/png"),
		Data:      mo.Some([]byte{1, 2, 3}),
		Text:      mo.None[string](),
	}
	// Act by applying the result and mutating the caller-owned bytes.
	state := (projection{}).Apply(event{
		RestoredTranscript:   nil,
		Kind:                 eventToolResult,
		ToolName:             mo.Some("read"),
		Contents:             mo.Some([]Content{content}),
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 mo.None[string](),
		ErrorText:            mo.None[string](),

		ExitCode:          mo.None[int](),
		Failure:           mo.Some(false),
		ToolCall:          mo.None[ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[ModelSelection](),
		SessionInfo:       mo.None[SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[SessionStatistics](),
		treeEvent:         mo.None[treeEvent](),
	})
	data, ok := content.Data.Get()
	require.True(t, ok)
	data[0] = 9

	// Assert the stored transcript retains independently owned image bytes.
	require.Len(t, state.Transcript, 1)
	contents, ok := state.Transcript[0].Contents.Get()
	require.True(t, ok)
	clonedData, ok := contents[0].Data.Get()
	require.True(t, ok)
	assert.Equal(t, []byte{1, 2, 3}, clonedData)
}

// TestStateClonesContentsAcrossStateSnapshots verifies image content cannot alias earlier presentation state.
func TestStateClonesContentsAcrossStateSnapshots(t *testing.T) {
	t.Parallel()

	// Arrange startup and transcript lines that share one image payload.
	line := Line{
		Kind:     LineToolDone,
		ToolName: mo.Some("read"),
		Status:   mo.None[string](),
		Text:     mo.Some("[image: image/png]"),
		Contents: mo.Some([]Content{{
			Text:      mo.None[string](),
			MediaType: mo.Some("image/png"),
			Data:      mo.Some([]byte{1, 2, 3}),
		}}),
	}
	previous := projection{
		Startup:          []Line{line},
		Transcript:       []Line{line},
		Models:           nil,
		ActiveModel:      nil,
		ActiveToolCalls:  nil,
		ActiveTools:      nil,
		Availability:     mo.None[Availability](),
		AuthorizationURL: mo.None[string](),
		Settled:          mo.None[bool](),
		ModelSelection:   mo.None[ModelSelection](),
		SessionInfo:      mo.None[SessionInfo](),
		Sessions:         nil,
	}
	// Act by applying an event and mutating both image copies in the next state.
	next := previous.Apply(
		testPresentationEvent(eventTurnStarted, mo.None[string](), mo.None[int]()),
	)

	for _, lines := range [][]Line{next.Startup, next.Transcript} {
		contents, ok := lines[0].Contents.Get()
		require.True(t, ok)
		contents[0].MediaType = mo.Some("image/jpeg")
		data, ok := contents[0].Data.Get()
		require.True(t, ok)
		data[0] = 9
	}

	// Assert the previous state retains its original media type and bytes.
	for _, lines := range [][]Line{previous.Startup, previous.Transcript} {
		contents, ok := lines[0].Contents.Get()
		require.True(t, ok)
		assert.Equal(t, mo.Some("image/png"), contents[0].MediaType)
		data, ok := contents[0].Data.Get()
		require.True(t, ok)
		assert.Equal(t, []byte{1, 2, 3}, data)
	}
}

// TestStateProjectsTypedToolResultTextInOrder verifies readable ordered terminal output.
func TestStateProjectsTypedToolResultTextInOrder(t *testing.T) {
	t.Parallel()

	// Arrange an event with ordered text and image tool-result content.
	event := event{
		RestoredTranscript: nil,
		Kind:               eventToolResult,
		ToolName:           mo.Some("read"),
		Contents: mo.Some([]Content{
			{
				Text:      mo.Some("first"),
				MediaType: mo.None[string](),
				Data:      mo.None[[]byte](),
			},
			{
				MediaType: mo.Some("image/png"),
				Data:      mo.Some([]byte{1, 2, 3}),
				Text:      mo.None[string](),
			},
			{
				Text:      mo.Some("last"),
				MediaType: mo.None[string](),
				Data:      mo.None[[]byte](),
			},
		}),
		Startup:              nil,
		Availability:         mo.None[Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 mo.None[string](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.Some(false),
		ToolCall:             mo.None[ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	}

	// Act by applying the typed tool result.
	state := (projection{}).Apply(event)

	// Assert the transcript text projection preserves content order.
	require.Len(t, state.Transcript, 1)
	assert.Equal(t, mo.Some("first\n[image: image/png]\nlast"), state.Transcript[0].Text)
}
