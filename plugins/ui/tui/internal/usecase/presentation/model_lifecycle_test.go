package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestServiceModelEndFinalizesCompleteMessageAcrossStreamPositions verifies terminal content merges deltas
// from distinct stream positions.
func TestServiceModelEndFinalizesCompleteMessageAcrossStreamPositions(t *testing.T) {
	t.Parallel()

	// Arrange model deltas that occupy distinct stream positions.
	service := New()
	state := service.Apply(
		presentationdomain.State{},
		testPresentationEvent(presentationdomain.EventModelDelta, mo.None[string](), mo.Some(0)),
	)
	// Act by applying later deltas and the terminal model response.
	state = service.Apply(
		state,
		testPresentationEvent(presentationdomain.EventModelDelta, mo.Some("complete answer"), mo.Some(1)),
	)
	state = service.Apply(state, testModelEndEvent(presentationdomain.ModelResponseContent{
		Kind: presentationdomain.ModelContentText,
		Text: mo.Some("complete answer"),
	}))

	// Assert the finalized transcript contains one complete ordered model message.
	assert.Equal(t, []presentationdomain.Line{{
		Kind:     presentationdomain.LineModel,
		Text:     mo.Some("complete answer"),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServicePreservesFinalizedRefusalBlocks verifies mixed public model content keeps its semantic kind.
func TestServicePreservesFinalizedRefusalBlocks(t *testing.T) {
	t.Parallel()

	// Arrange a streamed refusal followed by terminal response content.
	service := New()
	state := service.Apply(
		presentationdomain.State{},
		testModelDeltaEvent(0, presentationdomain.ModelContentText, "draft"),
	)
	// Act by applying the model-end event.
	state = service.Apply(state, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{
			{
				Kind: presentationdomain.ModelContentText,
				Text: mo.Some("answer"),
			},
			{
				Kind: presentationdomain.ModelContentRefusal,
				Text: mo.Some("cannot help"),
			},
		},
		Startup:           nil,
		Extensions:        nil,
		Availability:      mo.None[presentationdomain.Availability](),
		Position:          mo.None[int](),
		ModelContentKind:  mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:        mo.None[string](),
		ToolName:          mo.None[string](),
		Status:            mo.None[string](),
		Stream:            mo.None[presentationdomain.OutputStream](),
		Text:              mo.None[string](),
		Contents:          mo.None[[]presentationdomain.Content](),
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		Failure:           mo.None[bool](),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:         mo.None[presentationdomain.TreeEvent](),
	})

	// Assert the transcript retains the refusal kind and text.
	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("answer"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineRefusal,
			Text:     mo.Some("cannot help"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
	}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestServiceEmptyModelEndClearsStaleFragmentsWithoutTranscriptLine verifies tool-only model cleanup.
func TestServiceEmptyModelEndClearsStaleFragmentsWithoutTranscriptLine(t *testing.T) {
	t.Parallel()

	// Arrange an active model fragment with no terminal content.
	service := New()
	state := service.Apply(
		presentationdomain.State{},
		testPresentationEvent(presentationdomain.EventModelDelta, mo.Some("stale fragment"), mo.Some(1)),
	)
	// Act by applying an empty model-end event.
	state = service.Apply(
		state,
		testPresentationEvent(presentationdomain.EventModelEnd, mo.None[string](), mo.None[int]()),
	)

	// Assert stale fragments are cleared without adding a transcript line.
	assert.Empty(t, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}
