//nolint:exhaustruct // Tests set only fields relevant to each semantic event.
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
		{Kind: StreamEventContentStart, Position: 1, Content: model.Content{Kind: model.ContentText, Text: mo.Some("")}},
		{Kind: StreamEventTextDelta, Position: 1, Delta: "Hel"},
		{Kind: StreamEventTextDelta, Position: 1, Delta: "lo"},
		{Kind: StreamEventContentEnd, Position: 1},
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
		{Kind: StreamEventContentStart, Position: 0, Content: model.Content{Kind: model.ContentReasoning, Text: mo.Some("")}},
		{Kind: StreamEventTextDelta, Position: 0, Content: model.Content{Kind: model.ContentReasoning, Text: mo.Some("why")}, Delta: "why"},
		{Kind: StreamEventContentEnd, Position: 0, Content: model.Content{Kind: model.ContentReasoning, Text: mo.Some("")}},
		{Kind: StreamEventContentStart, Position: 1, Content: model.Content{Kind: model.ContentText, Text: mo.Some("")}},
		{Kind: StreamEventTextDelta, Position: 1, Content: model.Content{Kind: model.ContentText, Text: mo.Some("answer")}, Delta: "answer"},
		{Kind: StreamEventContentEnd, Position: 1, Content: model.Content{Kind: model.ContentText, Text: mo.Some("")}},
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
		Kind: StreamEventToolCallStart,
		Preview: model.ToolCallPreview{
			CallID: "call-1", Name: "read", Position: 2, Provisional: true, Fields: nil,
		},
	}
	require.NoError(t, applyToolCallStreamEvent(previews, start))
	require.Equal(t, start.Preview, previews["call-1"])

	delta := start
	delta.Kind = StreamEventToolCallDelta
	delta.Preview.Fields = []model.ToolCallPreviewField{{
		Name: "path", Kind: model.ToolCallPreviewFieldPrefix, Value: nil, Prefix: "fi",
	}}
	require.NoError(t, applyToolCallStreamEvent(previews, delta))
	require.Equal(t, delta.Preview, previews["call-1"])

	end := StreamEvent{
		Kind:     StreamEventToolCallEnd,
		Position: 2,
		ToolCall: model.ToolCall{
			ID: "call-1", Name: "read", Arguments: map[string]any{"path": "file.txt"},
		},
	}
	require.NoError(t, applyToolCallStreamEvent(previews, end))
	require.NotContains(t, previews, "call-1")
}

func TestServiceStateIsolatesNestedToolPreviewValues(t *testing.T) {
	t.Parallel()

	service := newTestService(t, "test", model.Descriptor{}, model.ReasoningChoiceOff, nil, hookrunner.New(nil, nil, nil), nil, nil)
	service.state.ToolPreviews = map[string]model.ToolCallPreview{
		"call-1": {
			CallID: "call-1", Name: "read", Position: 0, Provisional: true,
			Fields: []model.ToolCallPreviewField{{
				Name: "options", Kind: model.ToolCallPreviewFieldComplete,
				Value: map[string]any{"paths": []any{"first"}}, Prefix: "",
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

	service := newTestService(t, "test", model.Descriptor{}, model.ReasoningChoiceOff, nil, hookrunner.New(nil, nil, nil), nil, nil)
	service.state.Status = StatusRunning
	service.state.ToolPreviews = make(map[string]model.ToolCallPreview)
	preview := model.ToolCallPreview{
		CallID: "call-1", Name: "read", Position: 0, Provisional: true, Fields: nil,
	}
	require.NoError(t, service.applyStreamEvent(StreamEvent{
		Kind: StreamEventToolCallStart, Position: 0, Preview: preview,
	}))
	require.NoError(t, service.applyStreamEvent(StreamEvent{
		Kind: StreamEventDone, Response: model.Response{Outcome: mo.Some(model.OutcomeLength)},
	}))
	require.Empty(t, service.State().ToolPreviews)
}

// TestApplyStreamEventRequiresNonzeroTerminalOutcome preserves zero-outcome validation.
func TestApplyStreamEventRequiresNonzeroTerminalOutcome(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	err := applyStreamEvent(&partial, StreamEvent{
		Kind: StreamEventDone, Response: model.Response{},
	})

	require.ErrorContains(t, err, "requires an outcome")
	assert.True(t, partial.Outcome.IsNone())
}

func TestApplyStreamEventRejectsEventsAfterTerminal(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	require.NoError(t, applyStreamEvent(&partial, StreamEvent{
		Kind:     StreamEventDone,
		Response: model.Response{Outcome: mo.Some(model.OutcomeStop)},
	}))

	err := applyStreamEvent(&partial, StreamEvent{
		Kind:     StreamEventError,
		Response: model.Response{Outcome: mo.Some(model.OutcomeFailed), ErrorMessage: mo.Some("failed")},
	})

	require.ErrorContains(t, err, "already terminated")
	assert.Equal(t, model.OutcomeStop, partial.Outcome.OrEmpty())
}
