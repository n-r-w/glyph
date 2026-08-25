package run

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"
)

// TestApplyStreamEventBuildsOrderedTextState verifies semantic text assembly and order validation.
func TestApplyStreamEventBuildsOrderedTextState(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	for _, event := range []StreamEvent{
		{
			Delta:    mo.None[string](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     StreamEventContentStart,
			Position: mo.Some(1),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentText,
				Text:            mo.Some(""),
			}),
		},
		{
			Content: mo.Some(model.Content{
				Kind:            model.ContentText,
				Text:            mo.Some("Hel"),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			}),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     StreamEventTextDelta,
			Position: mo.Some(1),
			Delta:    mo.Some("Hel"),
		},
		{
			Content: mo.Some(model.Content{
				Kind:            model.ContentText,
				Text:            mo.Some("lo"),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			}),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     StreamEventTextDelta,
			Position: mo.Some(1),
			Delta:    mo.Some("lo"),
		},
		{
			Content: mo.Some(model.Content{
				Kind:            model.ContentText,
				Text:            mo.Some(""),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			}),
			Delta:    mo.None[string](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     StreamEventContentEnd,
			Position: mo.Some(1),
		},
	} {
		require.NoError(t, applyStreamEvent(&partial, event))
	}

	require.Len(t, partial.Content, 2)
	assert.Equal(t, model.ContentText, partial.Content[1].Kind)
	assert.Equal(t, "Hello", partial.Content[1].Text.OrEmpty())
	assert.True(t, partial.Content[1].Text.IsSome())
	assert.True(t, partial.Content[1].ProviderContext.IsNone())
	assert.True(t, partial.Content[1].ToolCall.IsNone())
	assert.True(t, partial.Outcome.IsNone())
	assert.True(t, partial.ErrorMessage.IsNone())
	assert.True(t, partial.Provider.IsNone())
	assert.True(t, partial.Model.IsNone())
	assert.True(t, partial.ResponseModel.IsNone())
	assert.True(t, partial.ResponseID.IsNone())
	assert.True(t, partial.Usage.IsNone())
}

// TestApplyStreamEventBuildsOrderedReasoningState verifies mixed typed content and terminal accounting.
func TestApplyStreamEventBuildsOrderedReasoningState(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	for _, event := range []StreamEvent{
		{
			Delta:    mo.None[string](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     StreamEventContentStart,
			Position: mo.Some(0),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentReasoning,
				Text:            mo.Some(""),
			}),
		},
		{
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     StreamEventTextDelta,
			Position: mo.Some(0),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentReasoning,
				Text:            mo.Some("why"),
			}),
			Delta: mo.Some("why"),
		},
		{
			Delta:    mo.None[string](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     StreamEventContentEnd,
			Position: mo.Some(0),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentReasoning,
				Text:            mo.Some(""),
			}),
		},
		{
			Delta:    mo.None[string](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     StreamEventContentStart,
			Position: mo.Some(1),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentText,
				Text:            mo.Some(""),
			}),
		},
		{
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     StreamEventTextDelta,
			Position: mo.Some(1),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentText,
				Text:            mo.Some("answer"),
			}),
			Delta: mo.Some("answer"),
		},
		{
			Delta:    mo.None[string](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     StreamEventContentEnd,
			Position: mo.Some(1),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentText,
				Text:            mo.Some(""),
			}),
		},
	} {
		require.NoError(t, applyStreamEvent(&partial, event))
	}

	require.Len(t, partial.Content, 2)
	assert.Equal(t, model.ContentReasoning, partial.Content[0].Kind)
	assert.Equal(t, "why", partial.Content[0].Text.OrEmpty())
	assert.True(t, partial.Content[0].Final)
	assert.Equal(t, model.ContentText, partial.Content[1].Kind)
	assert.Equal(t, "answer", partial.Content[1].Text.OrEmpty())
}

// TestApplyStreamEventRejectsEventsAfterTerminal verifies one terminal event closes the semantic stream.
func TestApplyToolCallStreamEventReplacesPreviewWithFinalCall(t *testing.T) {
	t.Parallel()

	previews := make(map[string]model.ToolCallPreview)
	start := StreamEvent{
		Position: mo.Some(0),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
		Kind:     StreamEventToolCallStart,
		Preview: mo.Some(model.ToolCallPreview{
			CallID:      "call-1",
			Name:        "read",
			Position:    0,
			Provisional: true,
			Fields:      nil,
		}),
	}
	missingPosition := start
	missingPosition.Position = mo.None[int]()
	require.Error(t, applyToolCallStreamEvent(previews, missingPosition))
	require.Empty(t, previews)
	require.NoError(t, applyToolCallStreamEvent(previews, start))
	require.Equal(t, start.Preview.OrEmpty(), previews["call-1"])

	delta := start
	delta.Kind = StreamEventToolCallDelta
	deltaPreview := delta.Preview.OrEmpty()
	deltaPreview.Fields = []model.ToolCallPreviewField{{
		Name:   "path",
		Kind:   model.ToolCallPreviewFieldPrefix,
		Value:  nil,
		Prefix: "fi",
	}}
	delta.Preview = mo.Some(deltaPreview)
	require.NoError(t, applyToolCallStreamEvent(previews, delta))
	require.Equal(t, delta.Preview.OrEmpty(), previews["call-1"])

	end := StreamEvent{
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		Response: mo.None[model.Response](),
		Kind:     StreamEventToolCallEnd,
		Position: mo.Some(0),
		ToolCall: mo.Some(model.ToolCall{
			ID:        "call-1",
			Name:      "read",
			Arguments: map[string]any{"path": "file.txt"},
		}),
	}
	require.NoError(t, applyToolCallStreamEvent(previews, end))
	require.NotContains(t, previews, "call-1")
}

func TestServiceStateIsolatesNestedToolPreviewValues(t *testing.T) {
	t.Parallel()

	service := newTestService(
		t,
		"test",
		model.Descriptor{},
		model.ReasoningChoiceOff,
		nil,
		hookrunner.New(nil, nil, nil),
		nil,
		nil,
	)
	service.state.ToolPreviews = map[string]model.ToolCallPreview{
		"call-1": {
			CallID:      "call-1",
			Name:        "read",
			Position:    0,
			Provisional: true,
			Fields: []model.ToolCallPreviewField{{
				Name:   "options",
				Kind:   model.ToolCallPreviewFieldComplete,
				Value:  map[string]any{"paths": []any{"first"}},
				Prefix: "",
			}},
		},
	}

	snapshot := service.State()
	snapshotValue := snapshot.ToolPreviews["call-1"].Fields[0].Value.(map[string]any)
	snapshotValue["paths"].([]any)[0] = "changed"
	storedValue := service.State().ToolPreviews["call-1"].Fields[0].Value.(map[string]any)
	require.Equal(t, "first", storedValue["paths"].([]any)[0])
}

func TestServiceTerminalStreamEventClearsToolCallPreview(t *testing.T) {
	t.Parallel()

	service := newTestService(
		t,
		"test",
		model.Descriptor{},
		model.ReasoningChoiceOff,
		nil,
		hookrunner.New(nil, nil, nil),
		nil,
		nil,
	)
	service.state.Status = StatusRunning
	service.state.ToolPreviews = make(map[string]model.ToolCallPreview)
	preview := model.ToolCallPreview{
		CallID:      "call-1",
		Name:        "read",
		Position:    0,
		Provisional: true,
		Fields:      nil,
	}
	require.NoError(t, service.applyStreamEvent(StreamEvent{
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
		Kind:     StreamEventToolCallStart,
		Position: mo.Some(0),
		Preview:  mo.Some(preview),
	}))
	require.NoError(t, service.applyStreamEvent(StreamEvent{
		Position: mo.None[int](),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Kind:     StreamEventDone,
		Response: mo.Some(model.Response{
			Content:       nil,
			ErrorMessage:  mo.None[string](),
			Provider:      mo.None[model.ProviderID](),
			Model:         mo.None[model.ID](),
			ResponseModel: mo.None[model.ID](),
			ResponseID:    mo.None[string](),
			Usage:         mo.None[model.Usage](),
			Diagnostics:   nil,
			Outcome:       mo.Some(model.OutcomeLength),
		}),
	}))
	require.Empty(t, service.State().ToolPreviews)
}

// TestApplyStreamEventRequiresNonzeroTerminalOutcome preserves zero-outcome validation.
func TestApplyStreamEventRequiresTextDeltaPresence(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	start := StreamEvent{
		Kind:     StreamEventContentStart,
		Position: mo.Some(0),
		Content: mo.Some(model.Content{
			Kind:            model.ContentText,
			Text:            mo.Some(""),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		}),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
	}
	require.NoError(t, applyStreamEvent(&partial, start))

	missingDelta := start
	missingDelta.Kind = StreamEventTextDelta
	require.Error(t, applyStreamEvent(&partial, missingDelta))
	assert.Empty(t, partial.Content[0].Text.OrEmpty())
}

func TestApplyStreamEventRequiresNonzeroTerminalOutcome(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	err := applyStreamEvent(&partial, StreamEvent{
		Position: mo.None[int](),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Kind:     StreamEventDone,
		Response: mo.None[model.Response](),
	})

	require.ErrorContains(t, err, "requires an outcome")
	assert.True(t, partial.Outcome.IsNone())
}

func TestApplyStreamEventRejectsEventsAfterTerminal(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	require.NoError(t, applyStreamEvent(&partial, StreamEvent{
		Position: mo.None[int](),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Kind:     StreamEventDone,
		Response: mo.Some(model.Response{
			Content:       nil,
			ErrorMessage:  mo.None[string](),
			Provider:      mo.None[model.ProviderID](),
			Model:         mo.None[model.ID](),
			ResponseModel: mo.None[model.ID](),
			ResponseID:    mo.None[string](),
			Usage:         mo.None[model.Usage](),
			Diagnostics:   nil,
			Outcome:       mo.Some(model.OutcomeStop),
		}),
	}))

	err := applyStreamEvent(&partial, StreamEvent{
		Position: mo.None[int](),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Kind:     StreamEventError,
		Response: mo.Some(model.Response{
			Content:       nil,
			Provider:      mo.None[model.ProviderID](),
			Model:         mo.None[model.ID](),
			ResponseModel: mo.None[model.ID](),
			ResponseID:    mo.None[string](),
			Usage:         mo.None[model.Usage](),
			Diagnostics:   nil,
			Outcome:       mo.Some(model.OutcomeFailed),
			ErrorMessage:  mo.Some("failed"),
		}),
	})

	require.ErrorContains(t, err, "already terminated")
	assert.Equal(t, model.OutcomeStop, partial.Outcome.OrEmpty())
}
