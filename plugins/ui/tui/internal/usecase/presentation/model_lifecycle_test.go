//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
)

// TestStateModelEndFinalizesCompleteMessageAcrossStreamPositions verifies terminal content merges deltas
// from distinct stream positions.
func TestStateModelEndFinalizesCompleteMessageAcrossStreamPositions(t *testing.T) {
	t.Parallel()

	// Arrange model deltas that occupy distinct stream positions.
	state := (projection{}).Apply(
		testPresentationEvent(eventModelDelta, mo.None[string](), mo.Some(0)),
	)
	// Act by applying later deltas and the terminal model response.
	state = state.Apply(
		testPresentationEvent(eventModelDelta, mo.Some("complete answer"), mo.Some(1)),
	)
	state = state.Apply(testModelEndEvent(ModelResponseContent{
		Kind: ModelContentText,
		Text: mo.Some("complete answer"),
	}))

	// Assert the finalized transcript contains one complete ordered model message.
	assert.Equal(t, []Line{{
		Kind:     LineModel,
		Text:     mo.Some("complete answer"),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]Content](),
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestStatePreservesFinalizedRefusalBlocks verifies mixed public model content keeps its semantic kind.
func TestStatePreservesFinalizedRefusalBlocks(t *testing.T) {
	t.Parallel()

	// Arrange a streamed refusal followed by terminal response content.
	state := (projection{}).Apply(
		testModelDeltaEvent(0, ModelContentText, "draft"),
	)
	// Act by applying the model-end event.
	state = state.Apply(event{
		FailureCode:        "",
		RestoredTranscript: nil,
		Kind:               eventModelEnd,
		ModelResponseContent: []ModelResponseContent{
			{
				Kind: ModelContentText,
				Text: mo.Some("answer"),
			},
			{
				Kind: ModelContentRefusal,
				Text: mo.Some("cannot help"),
			},
		},
		Startup:           nil,
		Availability:      mo.None[Availability](),
		Position:          mo.None[int](),
		ModelContentKind:  mo.None[ModelContentKind](),
		ToolCallID:        mo.None[string](),
		ToolName:          mo.None[string](),
		Status:            mo.None[string](),
		Stream:            mo.None[OutputStream](),
		Text:              mo.None[string](),
		Contents:          mo.None[[]Content](),
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		Failure:           mo.None[bool](),
		ToolCall:          mo.None[ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[ModelSelection](),
		SessionInfo:       mo.None[SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[SessionStatistics](),
		treeEvent:         mo.None[treeEvent](),
	})

	// Assert the transcript retains the refusal kind and text.
	assert.Equal(t, []Line{
		{
			Kind:     LineModel,
			Text:     mo.Some("answer"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineRefusal,
			Text:     mo.Some("cannot help"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
	}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestStateEmptyModelEndClearsStaleFragmentsWithoutTranscriptLine verifies tool-only model cleanup.
func TestStateEmptyModelEndClearsStaleFragmentsWithoutTranscriptLine(t *testing.T) {
	t.Parallel()

	// Arrange an active model fragment with no terminal content.
	state := (projection{}).Apply(
		testPresentationEvent(eventModelDelta, mo.Some("stale fragment"), mo.Some(1)),
	)
	// Act by applying an empty model-end event.
	state = state.Apply(
		testPresentationEvent(eventModelEnd, mo.None[string](), mo.None[int]()),
	)

	// Assert stale fragments are cleared without adding a transcript line.
	assert.Empty(t, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}
