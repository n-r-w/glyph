package codex

import (
	"encoding/json"

	"net/http"
	"net/http/httptest"

	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestDriverStreamStreamsReasoningInOutputOrder verifies Codex-owned mixed-content assembly.
func TestDriverStreamStreamsReasoningInOutputOrder(t *testing.T) {
	t.Parallel()

	accountID := "account-reasoning"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(
			t,
			accessToken,
			"refresh",
			accountID,
			time.Now().Add(time.Hour),
		), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writeSSE(
				writer,
				`{"type":"response.output_item.added","output_index":0,"item":{"id":"r","type":"reasoning","encrypted_content":"","summary":[]}}`,
				`{"type":"response.reasoning_summary_text.delta","output_index":0,"summary_index":0,"delta":"why"}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"id":"r","type":"reasoning","encrypted_content":"enc","summary":[{"type":"summary_text","text":"why"}]}}`,
				`{"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"answer"}`,
				`{"type":"response.output_item.done","output_index":1,"item":{"id":"m","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer","annotations":[],"logprobs":[]}]}}`,
				completedEvent(`[]`),
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	events := make([]run.StreamEvent, 0, 7)

	err := service.Stream(t.Context(), run.ModelRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History: []agent.HistoryEntry{
			{
				Model:      mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
				Kind:       agent.HistoryEntryUser,
				User:       mo.Some(model.TextMessage("request")),
			},
		},
		Tools: nil,
	}, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []run.StreamEventKind{
		run.StreamEventContentStart, run.StreamEventTextDelta, run.StreamEventContentEnd,
		run.StreamEventContentStart, run.StreamEventTextDelta, run.StreamEventContentEnd,
		run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, model.ContentReasoning, events[0].Content.OrEmpty().Kind)
	assert.Equal(t, 0, events[0].Position.OrEmpty())
	assert.Equal(t, "why", events[1].Delta.OrEmpty())
	assert.Equal(t, model.ContentText, events[3].Content.OrEmpty().Kind)
	assert.Equal(t, 2, events[3].Position.OrEmpty())
	terminal := events[len(events)-1].Response.OrEmpty()
	require.Len(t, terminal.Content, 2)
	assert.Equal(t, model.ContentReasoning, terminal.Content[0].Kind)
	assert.Equal(t, "why", terminal.Content[0].Text.OrEmpty())
	assert.NotEmpty(t, terminal.Content[0].ProviderContext.OrEmpty().Payload)
	assert.Equal(t, model.ContentText, terminal.Content[1].Kind)
}

// TestDriverStreamKeepsVisibleReasoningWithoutReplayContext verifies optional context and assistant-text fallback.
func TestDriverStreamKeepsVisibleReasoningWithoutReplayContext(t *testing.T) {
	t.Parallel()

	accountID := "account-visible-reasoning"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(
			t,
			accessToken,
			"refresh",
			accountID,
			time.Now().Add(time.Hour),
		), true, nil,
	).Times(2)
	interaction := NewMockInteraction(gomock.NewController(t))
	var requests atomic.Int32
	var secondBody map[string]any
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			current := requests.Add(1)
			if current == 2 {
				assert.NoError(t, json.NewDecoder(request.Body).Decode(&secondBody))
				writeSSE(writer, completedEvent(`[]`))
				return
			}
			writeSSE(
				writer,
				completedEvent(
					`[{"id":"r-visible","type":"reasoning","encrypted_content":"","summary":[{"type":"summary_text","text":"visible summary"}]}]`,
				),
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	request := run.ModelRequest{
		Tools:           nil,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		ReasoningChoice: model.ReasoningChoiceOn,
		History: []agent.HistoryEntry{
			{
				Model:      mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
				Kind:       agent.HistoryEntryUser,
				User:       mo.Some(model.TextMessage("request")),
			},
		},
	}

	firstEvents, err := collectStreamEvents(
		service,
		t.Context(),
		request,
		func(run.StreamEvent) error { return nil },
	)
	require.NoError(t, err)
	firstResponse := terminalResponse(firstEvents)
	require.Len(t, firstResponse.Content, 1)
	assert.Equal(t, model.ContentReasoning, firstResponse.Content[0].Kind)
	assert.Equal(t, "visible summary", firstResponse.Content[0].Text.OrEmpty())
	assert.Empty(t, firstResponse.Content[0].ProviderContext.OrEmpty().Payload)

	request.History = append(
		request.History,
		agent.HistoryEntry{
			User:       mo.None[model.Message](),
			ToolResult: mo.None[agent.ToolResult](),
			Kind:       agent.HistoryEntryModel,
			Model:      mo.Some(firstResponse),
		},
	)
	secondEvents, err := collectStreamEvents(
		service,
		t.Context(),
		request,
		func(run.StreamEvent) error { return nil },
	)
	require.NoError(t, err)
	assert.Equal(t, model.OutcomeStop, terminalResponse(secondEvents).Outcome.OrEmpty())
	require.Equal(t, int32(2), requests.Load())
	encoded, err := json.Marshal(secondBody["input"])
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"role":"assistant"`)
	assert.Contains(t, string(encoded), "visible summary")
	assert.NotContains(t, string(encoded), `"type":"reasoning"`)
}

// TestDriverStreamStreamsRefusalDeltas preserves incremental and finalized refusal text.
func TestDriverStreamStreamsRefusalDeltas(t *testing.T) {
	t.Parallel()

	accountID := "account-refusal"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(
			t,
			accessToken,
			"refresh",
			accountID,
			time.Now().Add(time.Hour),
		), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writeSSE(
				writer,
				`{"type":"response.refusal.delta","output_index":1,"content_index":0,"delta":"I can"}`,
				`{"type":"response.refusal.delta","output_index":1,"content_index":0,"delta":"not help"}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"id":"r-refusal","type":"reasoning","encrypted_content":"enc-refusal","summary":[]}}`,
				`{"type":"response.output_item.done","output_index":1,"item":{"id":"m-refusal","type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"I cannot help"}]}}`,
				completedEvent(`[]`),
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	events := make([]run.StreamEvent, 0, 5)

	err := service.Stream(t.Context(), run.ModelRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		History: []agent.HistoryEntry{
			{
				Model:      mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
				Kind:       agent.HistoryEntryUser,
				User:       mo.Some(model.TextMessage("request")),
			},
		},
		Tools: nil,
	}, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	require.NoError(t, err)
	require.Len(t, events, 5)
	assert.Equal(t, []run.StreamEventKind{
		run.StreamEventContentStart,
		run.StreamEventTextDelta,
		run.StreamEventTextDelta,
		run.StreamEventContentEnd,
		run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(
		t,
		run.StreamEvent{
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     run.StreamEventTextDelta,
			Position: mo.Some(1),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentRefusal,
				Text:            mo.Some("I can"),
			}),
			Delta: mo.Some("I can"),
		},
		events[1],
	)
	assert.Equal(
		t,
		run.StreamEvent{
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
			Kind:     run.StreamEventTextDelta,
			Position: mo.Some(1),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentRefusal,
				Text:            mo.Some("not help"),
			}),
			Delta: mo.Some("not help"),
		},
		events[2],
	)
	response := events[4].Response.OrEmpty()
	require.Len(t, response.Content, 2)
	assert.Equal(t, model.ContentReasoning, response.Content[0].Kind)
	assert.NotEmpty(t, response.Content[0].ProviderContext.OrEmpty().Payload)
	assert.Equal(t, model.ContentRefusal, response.Content[1].Kind)
	assert.Equal(t, "I cannot help", response.Content[1].Text.OrEmpty())
}

// TestDriverStreamRejectsMissingEncryptedReasoning verifies stateless replay fails before HTTP.
func TestDriverStreamRejectsMissingEncryptedReasoning(t *testing.T) {
	t.Parallel()

	accountID := "account"
	accessToken := testJWT(
		t,
		map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		},
	)
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	var requests atomic.Int32
	server := httptest.NewServer(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	history := []agent.HistoryEntry{
		{
			User:       mo.None[model.Message](),
			ToolResult: mo.None[agent.ToolResult](),
			Kind:       agent.HistoryEntryModel,
			Model: mo.Some(model.Response{
				Outcome:       mo.None[model.Outcome](),
				ErrorMessage:  mo.None[string](),
				Provider:      mo.None[model.ProviderID](),
				Model:         mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](),
				ResponseID:    mo.None[string](),
				Usage:         mo.None[model.Usage](),
				Diagnostics:   nil,
				Content: []model.Content{
					{
						Text:     mo.None[string](),
						Final:    false,
						ToolCall: mo.None[model.ToolCall](),
						Kind:     model.ContentReasoning,
						ProviderContext: mo.Some(
							model.ProviderContext{
								Source: model.ProviderContextSource{
									CompatibilityKey: mo.None[string](),
									ProviderID:       ProviderID,
									API:              "responses",
									Model:            "gpt-test",
								},
								Payload: []byte(`{"id":"r","encrypted_content":"","summary":[]}`),
							},
						),
					},
				},
			}),
		},
	}

	events, err := collectStreamEvents(
		service,
		t.Context(),
		run.ModelRequest{
			ReasoningChoice: model.ReasoningChoiceOn,
			Instructions:    "instructions",
			History:         history,
			Tools:           nil,
			Model:           testModelDescriptor("gpt-test"),
		},
		func(run.StreamEvent) error { return nil },
	)
	response := terminalResponse(events)

	require.Error(t, err)
	assert.Equal(t, model.OutcomeFailed, response.Outcome.OrEmpty())
	assert.Contains(t, response.ErrorMessage.OrEmpty(), "encrypted reasoning")
	assert.Zero(t, requests.Load())
}

// TestDriverStreamMapsOffReasoning verifies off uses the Responses none effort.
func TestDriverStreamMapsOffReasoning(t *testing.T) {
	t.Parallel()

	accountID := "account"
	accessToken := testJWT(
		t,
		map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		},
	)
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var body map[string]any
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, "gpt-request", body["model"])
			reasoning := body["reasoning"].(map[string]any)
			assert.Equal(t, "none", reasoning["effort"])
			input := body["input"].([]any)
			assert.Equal(t, []string{"message"}, inputTypes(input))
			writeSSE(
				writer,
				completedEvent(
					`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[],"logprobs":[]}]}]`,
				),
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))

	events, err := collectStreamEvents(service, t.Context(), run.ModelRequest{
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-request"),
		ReasoningChoice: model.ReasoningChoiceOff,
		History: []agent.HistoryEntry{
			{
				Model:      mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
				Kind:       agent.HistoryEntryUser,
				User:       mo.Some(model.TextMessage("hello")),
			},
		},
		Tools: nil,
	}, func(run.StreamEvent) error { return nil })
	response := terminalResponse(events)

	require.NoError(t, err)
	assert.Equal(t, model.OutcomeStop, response.Outcome.OrEmpty())
}
