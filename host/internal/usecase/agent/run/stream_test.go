//go:build !integration

package run

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"
)

// testStreamEvent creates a content stream event with no tool or terminal payload.
func testStreamEvent(
	kind StreamEventKind,
	position mo.Option[int],
	content mo.Option[model.Content],
	delta mo.Option[string],
) StreamEvent {
	return StreamEvent{
		Kind: kind, Position: position, Content: content, Delta: delta,
		Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
	}
}

// testTextStreamEvent creates one text-like content lifecycle event.
func testTextStreamEvent(
	kind StreamEventKind,
	position int,
	contentKind model.ContentKind,
	text string,
	delta mo.Option[string],
) StreamEvent {
	return testStreamEvent(kind, mo.Some(position), mo.Some(model.Content{
		Kind:            contentKind,
		Text:            mo.Some(text),
		Final:           false,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.None[model.ToolCall](),
	}), delta)
}

// TestApplyStreamEventBuildsOrderedTextState verifies semantic text assembly and order validation.
func TestApplyStreamEventBuildsOrderedTextState(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	for _, event := range []StreamEvent{
		testTextStreamEvent(StreamEventContentStart, 1, model.ContentText, "", mo.None[string]()),
		testTextStreamEvent(StreamEventTextDelta, 1, model.ContentText, "Hel", mo.Some("Hel")),
		testTextStreamEvent(StreamEventTextDelta, 1, model.ContentText, "lo", mo.Some("lo")),
		testTextStreamEvent(StreamEventContentEnd, 1, model.ContentText, "", mo.None[string]()),
	} {
		require.NoError(t, event.applyTo(&partial))
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
		testTextStreamEvent(StreamEventContentStart, 0, model.ContentReasoning, "", mo.None[string]()),
		testTextStreamEvent(StreamEventTextDelta, 0, model.ContentReasoning, "why", mo.Some("why")),
		testTextStreamEvent(StreamEventContentEnd, 0, model.ContentReasoning, "", mo.None[string]()),
		testTextStreamEvent(StreamEventContentStart, 1, model.ContentText, "", mo.None[string]()),
		testTextStreamEvent(StreamEventTextDelta, 1, model.ContentText, "answer", mo.Some("answer")),
		testTextStreamEvent(StreamEventContentEnd, 1, model.ContentText, "", mo.None[string]()),
	} {
		require.NoError(t, event.applyTo(&partial))
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
	require.Error(t, missingPosition.applyToolCallTo(previews))
	require.Empty(t, previews)
	require.NoError(t, start.applyToolCallTo(previews))
	require.Equal(t, start.Preview.OrEmpty(), previews["call-1"])

	delta := start
	delta.Kind = StreamEventToolCallDelta
	deltaPreview := delta.Preview.OrEmpty()
	deltaPreview.Fields = []model.ToolCallPreviewField{{
		Name:   "path",
		Kind:   model.ToolCallPreviewFieldPrefix,
		Value:  mo.None[any](),
		Prefix: mo.Some(""),
	}}
	delta.Preview = mo.Some(deltaPreview)
	require.NoError(t, delta.applyToolCallTo(previews))
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
	require.NoError(t, end.applyToolCallTo(previews))
	require.NotContains(t, previews, "call-1")
}

// TestApplyToolCallStreamEventChecksPayloadPresenceFirst verifies absent payloads are rejected before identity lookup.
func TestApplyToolCallStreamEventChecksPayloadPresenceFirst(t *testing.T) {
	t.Parallel()

	active := model.ToolCallPreview{
		CallID:      "",
		Name:        "sentinel",
		Position:    0,
		Provisional: true,
		Fields:      nil,
	}
	testCases := []struct {
		name          string
		event         StreamEvent
		errorContains string
	}{
		{
			name: "delta without preview",
			event: testStreamEvent(
				StreamEventToolCallDelta,
				mo.Some(0),
				mo.None[model.Content](),
				mo.None[string](),
			),
			errorContains: "tool-call delta requires preview",
		},
		{
			name: "end without tool call",
			event: testStreamEvent(
				StreamEventToolCallEnd,
				mo.Some(0),
				mo.None[model.Content](),
				mo.None[string](),
			),
			errorContains: "tool-call end requires tool call",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			previews := map[string]model.ToolCallPreview{"": active}

			require.ErrorContains(t, testCase.event.applyToolCallTo(previews), testCase.errorContains)
			assert.Equal(t, map[string]model.ToolCallPreview{"": active}, previews)
		})
	}
}

func TestApplyToolCallStreamEventRejectsMissingPreviewFieldPayload(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		kind model.ToolCallPreviewFieldKind
	}{
		{name: "complete value", kind: model.ToolCallPreviewFieldComplete},
		{name: "prefix", kind: model.ToolCallPreviewFieldPrefix},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			previews := make(map[string]model.ToolCallPreview)
			event := StreamEvent{
				Kind:     StreamEventToolCallStart,
				Position: mo.Some(0),
				Content:  mo.None[model.Content](),
				Delta:    mo.None[string](),
				ToolCall: mo.None[model.ToolCall](),
				Response: mo.None[model.Response](),
				Preview: mo.Some(model.ToolCallPreview{
					CallID:      "call-1",
					Name:        "read",
					Position:    0,
					Provisional: true,
					Fields: []model.ToolCallPreviewField{{
						Name:   "path",
						Kind:   testCase.kind,
						Value:  mo.None[any](),
						Prefix: mo.None[string](),
					}},
				}),
			}

			require.Error(t, event.applyToolCallTo(previews))
			require.Empty(t, previews)
		})
	}
}

func TestValidateToolCallPreviewFieldsPreservesNullAndEmptyPrefix(t *testing.T) {
	t.Parallel()

	fields := []model.ToolCallPreviewField{
		{
			Name:   "nullable",
			Kind:   model.ToolCallPreviewFieldComplete,
			Value:  mo.Some[any](nil),
			Prefix: mo.None[string](),
		},
		{
			Name:   "partial",
			Kind:   model.ToolCallPreviewFieldPrefix,
			Value:  mo.None[any](),
			Prefix: mo.Some(""),
		},
	}

	require.NoError(t, validateToolCallPreviewFields(fields))
	nullValue, ok := fields[0].Value.Get()
	require.True(t, ok)
	require.Nil(t, nullValue)
	assert.True(t, fields[0].Prefix.IsNone())
	assert.True(t, fields[1].Value.IsNone())
	assert.Equal(t, mo.Some(""), fields[1].Prefix)
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
				Value:  mo.Some[any](map[string]any{"paths": []any{"first"}}),
				Prefix: mo.None[string](),
			}},
		},
	}

	snapshot := service.State()
	snapshotValue, ok := snapshot.ToolPreviews["call-1"].Fields[0].Value.Get()
	require.True(t, ok)
	snapshotValue.(map[string]any)["paths"].([]any)[0] = "changed"
	storedValue, ok := service.State().ToolPreviews["call-1"].Fields[0].Value.Get()
	require.True(t, ok)
	require.Equal(t, "first", storedValue.(map[string]any)["paths"].([]any)[0])
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
	require.NoError(t, service.applyStreamEvent(testTerminalEvent(StreamEventDone, model.OutcomeLength)))
	require.Empty(t, service.State().ToolPreviews)
}

// TestApplyStreamEventRequiresNonzeroTerminalOutcome preserves zero-outcome validation.
func TestApplyStreamEventRequiresTextDeltaPresence(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	start := testStreamEvent(StreamEventContentStart, mo.Some(0), mo.Some(model.Content{
		Kind:            model.ContentText,
		Text:            mo.Some(""),
		Final:           false,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.None[model.ToolCall](),
	}), mo.None[string]())
	require.NoError(t, start.applyTo(&partial))

	missingDelta := start
	missingDelta.Kind = StreamEventTextDelta
	require.Error(t, missingDelta.applyTo(&partial))
	assert.Empty(t, partial.Content[0].Text.OrEmpty())
}

func TestApplyStreamEventRequiresNonzeroTerminalOutcome(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	err := testStreamEvent(
		StreamEventDone,
		mo.None[int](),
		mo.None[model.Content](),
		mo.None[string](),
	).applyTo(&partial)

	require.ErrorContains(t, err, "requires an outcome")
	assert.True(t, partial.Outcome.IsNone())
}

// TestValidateStreamEventShapeCoversEveryKind verifies active fields and one inactive field per variant.
func TestValidateStreamEventShapeCoversEveryKind(t *testing.T) {
	t.Parallel()

	content := model.Content{
		Kind:            model.ContentText,
		Text:            mo.Some(""),
		Final:           false,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.None[model.ToolCall](),
	}
	preview := model.ToolCallPreview{
		CallID: "call", Name: "tool", Position: 0, Provisional: true, Fields: nil,
	}
	call := model.ToolCall{
		ID:        "call",
		Name:      "tool",
		Arguments: map[string]any{"zero": float64(0), "empty": "", "false": false, "null": nil},
	}
	response := model.Response{
		Content: nil, ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](),
		Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
		Usage: mo.None[model.Usage](), Diagnostics: nil, Outcome: mo.Some(model.OutcomeStop),
	}
	tests := []struct {
		name  string
		valid StreamEvent
	}{
		{
			name:  "content start",
			valid: testStreamEvent(StreamEventContentStart, mo.Some(0), mo.Some(content), mo.None[string]()),
		},
		{name: "text delta", valid: testStreamEvent(StreamEventTextDelta, mo.Some(0), mo.Some(content), mo.Some(""))},
		{
			name:  "content end wildcard",
			valid: testStreamEvent(StreamEventContentEnd, mo.Some(0), mo.Some(model.Content{}), mo.None[string]()),
		},
		{
			name: "tool call start",
			valid: StreamEvent{
				Kind:     StreamEventToolCallStart,
				Position: mo.Some(0),
				Content:  mo.None[model.Content](),
				Delta:    mo.None[string](),
				Preview:  mo.Some(preview),
				ToolCall: mo.None[model.ToolCall](),
				Response: mo.None[model.Response](),
			},
		},
		{
			name: "tool call delta",
			valid: StreamEvent{
				Kind:     StreamEventToolCallDelta,
				Position: mo.Some(0),
				Content:  mo.None[model.Content](),
				Delta:    mo.None[string](),
				Preview:  mo.Some(preview),
				ToolCall: mo.None[model.ToolCall](),
				Response: mo.None[model.Response](),
			},
		},
		{
			name: "tool call end",
			valid: StreamEvent{
				Kind:     StreamEventToolCallEnd,
				Position: mo.Some(0),
				Content:  mo.None[model.Content](),
				Delta:    mo.None[string](),
				Preview:  mo.None[model.ToolCallPreview](),
				ToolCall: mo.Some(call),
				Response: mo.None[model.Response](),
			},
		},
		{
			name: "done",
			valid: StreamEvent{
				Kind:     StreamEventDone,
				Position: mo.None[int](),
				Content:  mo.None[model.Content](),
				Delta:    mo.None[string](),
				Preview:  mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](),
				Response: mo.Some(response),
			},
		},
		{
			name: "error",
			valid: StreamEvent{
				Kind:     StreamEventError,
				Position: mo.None[int](),
				Content:  mo.None[model.Content](),
				Delta:    mo.None[string](),
				Preview:  mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](),
				Response: mo.Some(response),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, test.valid.validateShape())
			malformed := test.valid
			if malformed.Response.IsSome() {
				malformed.Position = mo.Some(0)
			} else {
				malformed.Response = mo.Some(response)
			}
			require.Error(t, malformed.validateShape())
		})
	}
	for _, kind := range []StreamEventKind{0, 99} {
		require.Error(
			t,
			testStreamEvent(kind, mo.None[int](), mo.None[model.Content](), mo.None[string]()).validateShape(),
		)
	}
}

// TestValidateTerminalContentPreservesValidOptionalValues verifies every terminal content shape.
func TestValidateTerminalContentPreservesValidOptionalValues(t *testing.T) {
	t.Parallel()

	providerContext := model.ProviderContext{
		Source: model.ProviderContextSource{
			ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.Some(""),
		},
		Payload: []byte{0},
	}
	call := model.ToolCall{
		ID:        "call",
		Name:      "tool",
		Arguments: map[string]any{"zero": float64(0), "empty": "", "false": false, "null": nil},
	}
	tests := []struct {
		name    string
		content model.Content
	}{
		{
			name: "empty text",
			content: model.Content{
				Kind:            model.ContentText,
				Text:            mo.Some(""),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
		},
		{
			name: "empty refusal",
			content: model.Content{
				Kind:            model.ContentRefusal,
				Text:            mo.Some(""),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
		},
		{
			name: "reasoning text",
			content: model.Content{
				Kind:            model.ContentReasoning,
				Text:            mo.Some(""),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
		},
		{
			name: "reasoning provider context",
			content: model.Content{
				Kind:            model.ContentReasoning,
				Text:            mo.None[string](),
				Final:           true,
				ProviderContext: mo.Some(providerContext),
				ToolCall:        mo.None[model.ToolCall](),
			},
		},
		{
			name: "reasoning text and provider context",
			content: model.Content{
				Kind:            model.ContentReasoning,
				Text:            mo.Some(""),
				Final:           true,
				ProviderContext: mo.Some(providerContext),
				ToolCall:        mo.None[model.ToolCall](),
			},
		},
		{
			name: "tool call null arguments",
			content: model.Content{
				Kind:            model.ContentToolCall,
				Text:            mo.None[string](),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.Some(call),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, testTerminalContentResponse([]model.Content{test.content}).ValidateTerminalContent())
		})
	}

	invalid := []model.Content{
		{
			Kind:            model.ContentText,
			Text:            mo.None[string](),
			Final:           true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
		{
			Kind:            model.ContentRefusal,
			Text:            mo.Some(""),
			Final:           true,
			ProviderContext: mo.Some(providerContext),
			ToolCall:        mo.None[model.ToolCall](),
		},
		{
			Kind:            model.ContentReasoning,
			Text:            mo.None[string](),
			Final:           true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
		{
			Kind:            model.ContentReasoning,
			Text:            mo.Some(""),
			Final:           true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.Some(call),
		},
		{
			Kind:            model.ContentToolCall,
			Text:            mo.Some(""),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.Some(call),
		},
		{
			Kind:            model.ContentKind(99),
			Text:            mo.None[string](),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
		{
			Kind:            model.ContentText,
			Text:            mo.Some(""),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
	}
	for _, content := range invalid {
		require.Error(t, testTerminalContentResponse([]model.Content{content}).ValidateTerminalContent())
	}
}

// TestApplyStreamEventRejectsMalformedTerminalContentBeforeMutation verifies the terminal response boundary.
func TestApplyStreamEventRejectsMalformedTerminalContentBeforeMutation(t *testing.T) {
	t.Parallel()

	partial := testTerminalContentResponse([]model.Content{{
		Kind:            model.ContentText,
		Text:            mo.Some("streamed"),
		Final:           true,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.None[model.ToolCall](),
	}})
	err := (StreamEvent{
		Kind:     StreamEventDone,
		Position: mo.None[int](),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.Some(model.Response{
			Content: []model.Content{{
				Kind:            model.ContentText,
				Text:            mo.None[string](),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			}},
			ErrorMessage:  mo.None[string](),
			Provider:      mo.None[model.ProviderID](),
			Model:         mo.None[model.ID](),
			ResponseModel: mo.None[model.ID](),
			ResponseID:    mo.None[string](),
			Usage:         mo.None[model.Usage](),
			Diagnostics:   nil,
			Outcome:       mo.Some(model.OutcomeStop),
		}),
	}).applyTo(&partial)

	require.Error(t, err)
	assert.Equal(t, "streamed", partial.Content[0].Text.OrEmpty())
}

// TestApplyStreamEventRejectsInactiveTerminalPosition verifies stale stream fields are rejected.
func TestApplyStreamEventRejectsInactiveTerminalPosition(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	err := (StreamEvent{
		Kind:     StreamEventDone,
		Position: mo.Some(0),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
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
	}).applyTo(&partial)

	require.Error(t, err)
	assert.True(t, partial.Outcome.IsNone())
}

// testTerminalEvent creates a terminal stream event without response content.
func testTerminalEvent(kind StreamEventKind, outcome model.Outcome) StreamEvent {
	return StreamEvent{
		Position: mo.None[int](),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Kind:     kind,
		Response: mo.Some(emptyModelResponse(outcome)),
	}
}

// testTerminalContentResponse builds a response for content-only validation tests.
func testTerminalContentResponse(content []model.Content) model.Response {
	return model.Response{
		Content: content, Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
}

func TestApplyStreamEventRejectsEventsAfterTerminal(t *testing.T) {
	t.Parallel()

	partial := model.Response{}
	require.NoError(t, testTerminalEvent(StreamEventDone, model.OutcomeStop).applyTo(&partial))

	err := (StreamEvent{
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
	}).applyTo(&partial)

	require.ErrorContains(t, err, "already terminated")
	assert.Equal(t, model.OutcomeStop, partial.Outcome.OrEmpty())
}
