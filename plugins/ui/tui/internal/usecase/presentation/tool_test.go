package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestServiceCopiesTypedToolResultImage verifies presentation state owns image bytes.
func TestServiceCopiesTypedToolResultImage(t *testing.T) {
	t.Parallel()

	// Arrange typed tool-result image content with caller-owned bytes.
	service := New()
	content := presentationdomain.Content{
		MediaType: mo.Some("image/png"),
		Data:      mo.Some([]byte{1, 2, 3}),
		Text:      mo.None[string](),
	}
	// Act by applying the result and mutating the caller-owned bytes.
	state := service.Apply(presentationdomain.State{}, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventToolResult,
		ToolName:             mo.Some("read"),
		Contents:             mo.Some([]presentationdomain.Content{content}),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		ErrorText:            mo.None[string](),

		ExitCode:          mo.None[int](),
		Failure:           mo.Some(false),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
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

// TestServiceClonesContentsAcrossStateSnapshots verifies image content cannot alias earlier presentation state.
func TestServiceClonesContentsAcrossStateSnapshots(t *testing.T) {
	t.Parallel()

	// Arrange startup and transcript lines that share one image payload.
	line := presentationdomain.Line{
		Kind:     presentationdomain.LineToolDone,
		ToolName: mo.Some("read"),
		Status:   mo.None[string](),
		Text:     mo.Some("[image: image/png]"),
		Contents: mo.Some([]presentationdomain.Content{{
			Text:      mo.None[string](),
			MediaType: mo.Some("image/png"),
			Data:      mo.Some([]byte{1, 2, 3}),
		}}),
	}
	previous := presentationdomain.State{
		Startup:          []presentationdomain.Line{line},
		Transcript:       []presentationdomain.Line{line},
		Models:           nil,
		ActiveModel:      nil,
		ActiveToolCalls:  nil,
		ActiveTools:      nil,
		Availability:     mo.None[presentationdomain.Availability](),
		AuthorizationURL: mo.None[string](),
		Settled:          mo.None[bool](),
		ModelSelection:   mo.None[presentationdomain.ModelSelection](),
		SessionInfo:      mo.None[presentationdomain.SessionInfo](),
		Sessions:         nil,
	}
	// Act by applying an event and mutating both image copies in the next state.
	next := New().Apply(previous, testPresentationEvent(presentationdomain.EventTurnStarted, mo.None[string](), mo.None[int]()))

	for _, lines := range [][]presentationdomain.Line{next.Startup, next.Transcript} {
		contents, ok := lines[0].Contents.Get()
		require.True(t, ok)
		contents[0].MediaType = mo.Some("image/jpeg")
		data, ok := contents[0].Data.Get()
		require.True(t, ok)
		data[0] = 9
	}

	// Assert the previous state retains its original media type and bytes.
	for _, lines := range [][]presentationdomain.Line{previous.Startup, previous.Transcript} {
		contents, ok := lines[0].Contents.Get()
		require.True(t, ok)
		assert.Equal(t, mo.Some("image/png"), contents[0].MediaType)
		data, ok := contents[0].Data.Get()
		require.True(t, ok)
		assert.Equal(t, []byte{1, 2, 3}, data)
	}
}

// TestServiceProjectsTypedToolResultTextInOrder verifies readable ordered terminal output.
func TestServiceProjectsTypedToolResultTextInOrder(t *testing.T) {
	t.Parallel()

	// Arrange an event with ordered text and image tool-result content.
	event := presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventToolResult,
		ToolName:           mo.Some("read"),
		Contents: mo.Some([]presentationdomain.Content{
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
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.Some(false),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	}

	// Act by applying the typed tool result.
	state := New().Apply(presentationdomain.State{}, event)

	// Assert the transcript text projection preserves content order.
	require.Len(t, state.Transcript, 1)
	assert.Equal(t, mo.Some("first\n[image: image/png]\nlast"), state.Transcript[0].Text)
}
