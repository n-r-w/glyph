package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	internalhooks "github.com/n-r-w/glyph/host/internal/hooks"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestDriverStreamSendsOrderedStrictRequestAndPreservesOutput verifies the complete Responses translation.
func TestDriverStreamSendsOrderedStrictRequestAndPreservesOutput(t *testing.T) {
	t.Parallel()

	accountID := "account-ordered"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			assert.Equal(t, "/responses", request.URL.Path)
			assert.Equal(t, "Bearer "+accessToken, request.Header.Get("Authorization"))
			assert.Equal(t, accountID, request.Header.Get("chatgpt-account-id"))
			assert.Equal(t, "responses=experimental", request.Header.Get("OpenAI-Beta"))
			assert.Equal(t, "glyph", request.Header.Get("originator"))
			assert.Equal(t, "glyph", request.Header.Get("User-Agent"))
			var body map[string]any
			assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			assert.Equal(t, "gpt-request", body["model"])
			assert.Equal(t, false, body["store"])
			assert.Equal(t, "request instructions", body["instructions"])
			assert.Equal(t, true, body["stream"])
			assert.Contains(t, body["include"], "reasoning.encrypted_content")
			reasoning := body["reasoning"].(map[string]any)
			assert.Equal(t, "medium", reasoning["effort"])
			assert.Equal(t, "auto", reasoning["summary"])
			tools := body["tools"].([]any)
			if !assert.Len(t, tools, 1) {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			functionTool := tools[0].(map[string]any)
			assert.Equal(t, "function", functionTool["type"])
			assert.Equal(t, true, functionTool["strict"])
			assert.Equal(t, "read", functionTool["name"])
			input := body["input"].([]any)
			if !assert.Len(t, input, 6) {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, []string{
				"message",
				"reasoning",
				"message",
				"function_call",
				"function_call_output",
				"message",
			}, inputTypes(input))
			assert.Equal(t, "user", input[0].(map[string]any)["role"])
			assert.Equal(t, "r-old", input[1].(map[string]any)["id"])
			assert.Equal(t, "assistant", input[2].(map[string]any)["role"])
			assert.Equal(t, "call-old", input[3].(map[string]any)["call_id"])
			assert.Equal(t, "call-old", input[4].(map[string]any)["call_id"])
			assert.Equal(t, "user", input[5].(map[string]any)["role"])
			writeSSE(
				writer,
				`{"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"ans"}`,
				`{"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"wer"}`,
				completedEvent(`[
				{"id":"r-new","type":"reasoning","encrypted_content":"enc-new","summary":[{"type":"summary_text","text":"summary"}]},
				{"id":"m-new","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer","annotations":[],"logprobs":[]}]},
				{"id":"fc-new","type":"function_call","call_id":"call-new","name":"read","arguments":"{\"path\":\"file.txt\"}","status":"completed"}
			]`),
			)
		}),
	)
	t.Cleanup(server.Close)
	options := testProviderOptions(server)
	service := newDriver(testConfig(), credentials, interaction, options)
	updates := make([]run.StreamEvent, 0)
	history := []agent.HistoryEntry{
		{
			Model:      model.Response{},
			ToolResult: agent.ToolResult{},
			Kind:       agent.HistoryEntryUser,
			User:       model.TextMessage("first"),
		},
		{
			User:       model.Message{},
			ToolResult: agent.ToolResult{},
			Kind:       agent.HistoryEntryModel,
			Model: model.Response{
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
									Model:            "gpt-request",
								},
								Payload: []byte(
									`{ "summary" : ["old"], "encrypted_content" : "enc-old", "id" : "r-old" }`,
								),
							},
						),
					},
					{
						Final:           false,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall:        mo.None[model.ToolCall](),
						Kind:            model.ContentText,
						Text:            mo.Some("prior"),
					},
					{
						Text:            mo.None[string](),
						Final:           false,
						ProviderContext: mo.None[model.ProviderContext](),
						Kind:            model.ContentToolCall,
						ToolCall: mo.Some(
							model.ToolCall{
								ID:        "call-old",
								Name:      "read",
								Arguments: map[string]any{"path": "old.txt"},
							},
						),
					},
				},
				Outcome: mo.Some(model.OutcomeToolUse),
			},
		},
		{
			User:  model.Message{},
			Model: model.Response{},
			Kind:  agent.HistoryEntryToolResult,
			ToolResult: agent.ToolResult{
				CallID:   "call-old",
				ToolName: "read",
				Contents: tool.TextContents("old data"),
				IsError:  false,
			},
		},
		{
			Model:      model.Response{},
			ToolResult: agent.ToolResult{},
			Kind:       agent.HistoryEntryUser,
			User:       model.TextMessage("next"),
		},
	}

	events, err := collectStreamEvents(service, t.Context(), run.ModelRequest{
		Instructions:    "request instructions",
		Model:           testModelDescriptor("gpt-request"),
		ReasoningChoice: model.ReasoningChoiceMedium,
		History:         history,
		Tools: []tool.Descriptor{
			{
				ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
				Name:                "read",
				Description:         "Read a file.",
				InputSchemaJSON: []byte(
					`{"type":"object","properties":{"path":{"type":"string","description":"File path."}},"required":["path"],"additionalProperties":false}`,
				),
			},
		},
	}, func(update run.StreamEvent) error {
		if update.Kind == run.StreamEventTextDelta {
			updates = append(updates, update)
		}
		return nil
	})
	response := terminalResponse(events)

	require.NoError(t, err)
	assert.Equal(
		t,
		[]run.StreamEvent{
			{
				Preview:  model.ToolCallPreview{},
				ToolCall: model.ToolCall{},
				Response: model.Response{},
				Kind:     run.StreamEventTextDelta,
				Position: 1,
				Content: model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentText,
					Text:            mo.Some("ans"),
				},
				Delta: "ans",
			},
			{
				Preview:  model.ToolCallPreview{},
				ToolCall: model.ToolCall{},
				Response: model.Response{},
				Kind:     run.StreamEventTextDelta,
				Position: 1,
				Content: model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentText,
					Text:            mo.Some("wer"),
				},
				Delta: "wer",
			},
		},
		updates,
	)
	assert.Equal(t, model.OutcomeToolUse, response.Outcome.OrEmpty())
	assert.True(t, response.Outcome.IsSome())
	assert.Equal(t, model.ProviderID(ProviderID), response.Provider.OrEmpty())
	assert.Equal(t, model.ID("gpt-request"), response.Model.OrEmpty())
	assert.True(t, response.ResponseModel.IsNone())
	assert.Equal(t, "resp", response.ResponseID.OrEmpty())
	assert.True(t, response.ResponseID.IsSome())
	assert.True(t, response.Usage.IsNone())
	require.Len(t, response.Content, 3)
	assert.Equal(t, model.ContentReasoning, response.Content[0].Kind)
	assert.Equal(t, "summary", response.Content[0].Text.OrEmpty())
	assert.Equal(
		t,
		model.ProviderID(ProviderID),
		response.Content[0].ProviderContext.OrEmpty().Source.ProviderID,
	)
	assert.Equal(t, "responses", response.Content[0].ProviderContext.OrEmpty().Source.API)
	assert.Equal(
		t,
		model.ID("gpt-request"),
		response.Content[0].ProviderContext.OrEmpty().Source.Model,
	)
	assert.Equal(t, model.ContentText, response.Content[1].Kind)
	assert.Equal(t, "answer", response.Content[1].Text.OrEmpty())
	assert.Equal(t, model.ContentToolCall, response.Content[2].Kind)
	assert.Equal(
		t,
		map[string]any{"path": "file.txt"},
		response.Content[2].ToolCall.OrEmpty().Arguments,
	)
}

// TestDriverStreamSerializesImageAndMapsTerminalAccounting verifies rich input and terminal values.
func TestDriverStreamSerializesImageAndMapsTerminalAccounting(t *testing.T) {
	t.Parallel()

	accountID := "account-image"
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
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var body map[string]any
			if !assert.NoError(t, json.NewDecoder(request.Body).Decode(&body)) {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			input := body["input"].([]any)
			if !assert.Len(t, input, 1) {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			content := input[0].(map[string]any)["content"].([]any)
			if !assert.Len(t, content, 2) {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, "input_text", content[0].(map[string]any)["type"])
			assert.Equal(t, "input_image", content[1].(map[string]any)["type"])
			assert.Equal(t, "data:image/png;base64,AQID", content[1].(map[string]any)["image_url"])
			writeSSE(
				writer,
				`{"type":"response.completed","response":{"id":"resp-rich","model":"gpt-actual","status":"completed","service_tier":"default","metadata":{"region":"test"},"usage":{"input_tokens":10,"output_tokens":7,"total_tokens":17,"input_tokens_details":{"cached_tokens":4,"cache_write_tokens":1},"output_tokens_details":{"reasoning_tokens":3}},"output":[]}}`,
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))

	events, err := collectStreamEvents(
		service,
		t.Context(),
		run.ModelRequest{
			ReasoningChoice: model.ReasoningChoiceOn,
			Instructions:    "instructions",
			Model: model.Descriptor{
				ReasoningCapabilities: model.ReasoningCapabilities{},
				ToolCapabilities:      model.ToolCapabilities{},
				Provider:              ProviderID,
				Model:                 "gpt-selected",
			},
			History: []agent.HistoryEntry{
				{
					Model:      model.Response{},
					ToolResult: agent.ToolResult{},
					Kind:       agent.HistoryEntryUser,
					User: model.Message{
						Content: []model.InputContent{
							{
								MediaType: mo.None[string](),
								Data:      mo.None[[]byte](),
								Kind:      model.InputContentText,
								Text:      mo.Some("inspect"),
							},
							{
								Text:      mo.None[string](),
								Kind:      model.InputContentImage,
								MediaType: mo.Some("image/png"),
								Data:      mo.Some([]byte{1, 2, 3}),
							},
						},
					},
				},
			},
			Tools: nil,
		},
		func(run.StreamEvent) error { return nil },
	)
	response := terminalResponse(events)

	require.NoError(t, err)
	assert.Equal(t, model.ProviderID(ProviderID), response.Provider.OrEmpty())
	assert.Equal(t, model.ID("gpt-selected"), response.Model.OrEmpty())
	require.True(t, response.Provider.IsSome())
	require.True(t, response.Model.IsSome())
	require.True(t, response.ResponseModel.IsSome())
	assert.Equal(t, model.ID("gpt-actual"), response.ResponseModel.OrEmpty())
	assert.Equal(t, "resp-rich", response.ResponseID.OrEmpty())
	assert.True(t, response.ResponseID.IsSome())
	assert.True(t, response.Usage.IsSome())
	assert.Equal(t, model.Usage{
		InputTokens:       10,
		OutputTokens:      7,
		CachedInputTokens: 4,
		CacheWriteTokens:  1,
		ReasoningTokens:   3,
		TotalTokens:       17,
	}, response.Usage.OrEmpty())
}

// TestDriverStreamUsesFinalizedOutputItemsWhenCompletedOutputIsEmpty preserves terminal streamed output.
func TestDriverStreamEmitsProvisionalAndFinalFunctionCall(t *testing.T) {
	t.Parallel()

	accountID := "account-tool-preview"
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
				`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"","status":"in_progress"}}`,
				`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc-1","delta":"{\"path\":\"file.txt\",\"query\":\"hel"}`,
				`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc-1","delta":"lo\"}"}`,
				`{"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc-1","name":"read","arguments":"{\"path\":\"file.txt\",\"query\":\"hello\"}"}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"{\"path\":\"file.txt\",\"query\":\"hello\"}","status":"completed"}}`,
				completedEvent(`[]`),
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))

	events := make([]run.StreamEvent, 0)
	err := service.Stream(t.Context(), run.ModelRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "test",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil,
	}, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallDelta,
		run.StreamEventToolCallDelta, run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	require.Equal(t, "read", events[0].Preview.Name)
	require.True(t, events[0].Preview.Provisional)
	require.Equal(t, "hel", events[1].Preview.Fields[1].Prefix)
	require.Equal(
		t,
		map[string]any{"path": "file.txt", "query": "hello"},
		events[3].ToolCall.Arguments,
	)
}

// TestDriverStreamRecoversFunctionCallWithoutAddedEvent verifies authoritative item completion creates the lifecycle.
func TestDriverStreamRecoversFunctionCallWithoutAddedEvent(t *testing.T) {
	t.Parallel()

	accountID := "account-tool-missing-added"
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
				`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"path\":\"file.txt\"}"}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"{\"path\":\"file.txt\"}","status":"completed"}}`,
				completedEvent(`[]`),
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))

	events := make([]run.StreamEvent, 0)
	err := service.Stream(
		t.Context(),
		run.ModelRequest{
			History:         nil,
			Tools:           nil,
			ReasoningChoice: model.ReasoningChoiceOn,
			Instructions:    "test",
			Model:           testModelDescriptor("gpt-test"),
		},
		func(event run.StreamEvent) error {
			events = append(events, event)
			return nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, "call-1", events[0].Preview.CallID)
	assert.Equal(t, "read", events[0].Preview.Name)
	assert.Equal(t, model.ToolCall{
		ID:        "call-1",
		Name:      "read",
		Arguments: map[string]any{"path": "file.txt"},
	}, events[1].ToolCall)
}

func TestDriverStreamRejectsInvalidFinalFunctionArguments(t *testing.T) {
	t.Parallel()

	accountID := "account-invalid-tool"
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
				`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"","status":"in_progress"}}`,
				`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc-1","delta":"{\"path\":"}`,
				`{"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc-1","name":"read","arguments":"{\"path\":"}`,
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))

	events := make([]run.StreamEvent, 0)
	err := service.Stream(t.Context(), run.ModelRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "test",
		Model:           testModelDescriptor("gpt-test"),
		History:         nil,
		Tools:           nil,
	}, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	require.Error(t, err)
	require.Equal(t, model.OutcomeFailed, events[len(events)-1].Response.Outcome.OrEmpty())
	require.NotContains(t, streamEventKinds(events), run.StreamEventToolCallEnd)
}

func TestDriverStreamRecoversOmittedCompletedOutputItems(t *testing.T) {
	t.Parallel()

	accountID := "account-finalized-text"
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
				`{"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"final "}`,
				`{"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"answer"}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"id":"r-final","type":"reasoning","encrypted_content":"enc-final","summary":[]}}`,
				`{"type":"response.output_item.done","output_index":1,"item":{"id":"m-final","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"final answer","annotations":[],"logprobs":[]}]}}`,
				`{"type":"response.output_item.done","output_index":2,"item":{"id":"fc-final","type":"function_call","call_id":"call-final","name":"read","arguments":"{\"path\":\"file.txt\"}","status":"completed"}}`,
				completedEvent(
					`[{"id":"r-final","type":"reasoning","encrypted_content":"enc-final","summary":[]}]`,
				),
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	updates := make([]run.StreamEvent, 0, 2)

	events, err := collectStreamEvents(
		service,
		t.Context(),
		run.ModelRequest{
			ReasoningChoice: model.ReasoningChoiceOn,
			Instructions:    "instructions",
			Model:           testModelDescriptor("gpt-test"),
			History: []agent.HistoryEntry{
				{
					Model:      model.Response{},
					ToolResult: agent.ToolResult{},
					Kind:       agent.HistoryEntryUser,
					User:       model.TextMessage("request"),
				},
			},
			Tools: nil,
		},
		func(update run.StreamEvent) error {
			if update.Kind == run.StreamEventTextDelta {
				updates = append(updates, update)
			}
			return nil
		},
	)
	response := terminalResponse(events)

	require.NoError(t, err)
	assert.Equal(t, []run.StreamEvent{
		{
			Preview:  model.ToolCallPreview{},
			ToolCall: model.ToolCall{},
			Response: model.Response{},
			Kind:     run.StreamEventTextDelta,
			Position: 1,
			Content: model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentText,
				Text:            mo.Some("final "),
			},
			Delta: "final ",
		},
		{
			Preview:  model.ToolCallPreview{},
			ToolCall: model.ToolCall{},
			Response: model.Response{},
			Kind:     run.StreamEventTextDelta,
			Position: 1,
			Content: model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentText,
				Text:            mo.Some("answer"),
			},
			Delta: "answer",
		},
	}, updates)
	assert.Equal(t, model.OutcomeToolUse, response.Outcome.OrEmpty())
	require.Len(t, response.Content, 3)
	assert.Equal(t, model.ContentReasoning, response.Content[0].Kind)
	assert.NotEmpty(t, response.Content[0].ProviderContext.OrEmpty().Payload)
	assert.Equal(t, model.ContentText, response.Content[1].Kind)
	assert.Equal(t, "final answer", response.Content[1].Text.OrEmpty())
	assert.Equal(t, model.ContentToolCall, response.Content[2].Kind)
	assert.Equal(t, "read", response.Content[2].ToolCall.OrEmpty().Name)
	assert.Equal(
		t,
		map[string]any{"path": "file.txt"},
		response.Content[2].ToolCall.OrEmpty().Arguments,
	)
}

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
				Model:      model.Response{},
				ToolResult: agent.ToolResult{},
				Kind:       agent.HistoryEntryUser,
				User:       model.TextMessage("request"),
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
	assert.Equal(t, model.ContentReasoning, events[0].Content.Kind)
	assert.Equal(t, 0, events[0].Position)
	assert.Equal(t, "why", events[1].Delta)
	assert.Equal(t, model.ContentText, events[3].Content.Kind)
	assert.Equal(t, 2, events[3].Position)
	terminal := events[len(events)-1].Response
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
				Model:      model.Response{},
				ToolResult: agent.ToolResult{},
				Kind:       agent.HistoryEntryUser,
				User:       model.TextMessage("request"),
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
			User:       model.Message{},
			ToolResult: agent.ToolResult{},
			Kind:       agent.HistoryEntryModel,
			Model:      firstResponse,
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
				Model:      model.Response{},
				ToolResult: agent.ToolResult{},
				Kind:       agent.HistoryEntryUser,
				User:       model.TextMessage("request"),
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
			Preview:  model.ToolCallPreview{},
			ToolCall: model.ToolCall{},
			Response: model.Response{},
			Kind:     run.StreamEventTextDelta,
			Position: 1,
			Content: model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentRefusal,
				Text:            mo.Some("I can"),
			},
			Delta: "I can",
		},
		events[1],
	)
	assert.Equal(
		t,
		run.StreamEvent{
			Preview:  model.ToolCallPreview{},
			ToolCall: model.ToolCall{},
			Response: model.Response{},
			Kind:     run.StreamEventTextDelta,
			Position: 1,
			Content: model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentRefusal,
				Text:            mo.Some("not help"),
			},
			Delta: "not help",
		},
		events[2],
	)
	response := events[4].Response
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
			User:       model.Message{},
			ToolResult: agent.ToolResult{},
			Kind:       agent.HistoryEntryModel,
			Model: model.Response{
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
			},
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
				Model:      model.Response{},
				ToolResult: agent.ToolResult{},
				Kind:       agent.HistoryEntryUser,
				User:       model.TextMessage("hello"),
			},
		},
		Tools: nil,
	}, func(run.StreamEvent) error { return nil })
	response := terminalResponse(events)

	require.NoError(t, err)
	assert.Equal(t, model.OutcomeStop, response.Outcome.OrEmpty())
}

// TestDriverStreamRefreshesAtThresholdAndPersistsRotation verifies fresh request authorization.
func TestDriverStreamRefreshesAtThresholdAndPersistsRotation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	accountID := "account-refresh"
	oldAccess := testJWT(
		t,
		map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		},
	)
	newAccess := testJWT(
		t,
		map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
			"rotated":                     true,
		},
	)
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, oldAccess, "refresh-old", accountID, now.Add(5*time.Minute)), true, nil)
	credentials.EXPECT().Save(gomock.Any()).DoAndReturn(func(payload []byte) error {
		var rotated oauthCredentials
		require.NoError(t, json.Unmarshal(payload, &rotated))
		assert.Equal(t, newAccess, rotated.AccessToken)
		assert.Equal(t, "refresh-new", rotated.RefreshToken)
		assert.Equal(t, accountID, rotated.AccountID)
		return nil
	})
	interaction := NewMockInteraction(gomock.NewController(t))
	var tokenRequests atomic.Int32
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/token":
				tokenRequests.Add(1)
				var body map[string]string
				assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				assert.Equal(t, "refresh_token", body["grant_type"])
				assert.Equal(t, "refresh-old", body["refresh_token"])
				writer.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(
					writer,
					`{"access_token":%q,"refresh_token":"refresh-new","expires_in":3600}`,
					newAccess,
				)
			case "/responses":
				assert.Equal(t, "Bearer "+newAccess, request.Header.Get("Authorization"))
				assert.Equal(t, accountID, request.Header.Get("chatgpt-account-id"))
				writeSSE(
					writer,
					completedEvent(
						`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[],"logprobs":[]}]}]`,
					),
				)
			default:
				http.NotFound(writer, request)
			}
		}),
	)
	t.Cleanup(server.Close)
	options := testProviderOptions(server)
	options.tokenURL = server.URL + "/token"
	options.now = func() time.Time { return now }
	service := newDriver(testConfig(), credentials, interaction, options)

	events, err := collectStreamEvents(
		service,
		t.Context(),
		run.ModelRequest{
			ReasoningChoice: model.ReasoningChoiceOn,
			Instructions:    "instructions",
			Model:           testModelDescriptor("gpt-test"),
			History: []agent.HistoryEntry{
				{
					Model:      model.Response{},
					ToolResult: agent.ToolResult{},
					Kind:       agent.HistoryEntryUser,
					User:       model.TextMessage("hello"),
				},
			},
			Tools: nil,
		},
		func(run.StreamEvent) error { return nil },
	)
	response := terminalResponse(events)

	require.NoError(t, err)
	assert.Equal(t, model.OutcomeStop, response.Outcome.OrEmpty())
	assert.Equal(t, int32(1), tokenRequests.Load())
}

// TestDriverStreamSkipsRefreshOutsideThreshold verifies six remaining minutes use loaded credentials.
func TestDriverStreamSkipsRefreshOutsideThreshold(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	accountID := "account-fresh"
	accessToken := testJWT(
		t,
		map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		},
	)
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(testCredentialPayload(t, accessToken, "refresh", accountID, now.Add(6*time.Minute)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			assert.Equal(t, "Bearer "+accessToken, request.Header.Get("Authorization"))
			writeSSE(
				writer,
				completedEvent(
					`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[],"logprobs":[]}]}]`,
				),
			)
		}),
	)
	t.Cleanup(server.Close)
	options := testProviderOptions(server)
	options.now = func() time.Time { return now }
	service := newDriver(testConfig(), credentials, interaction, options)

	_, err := collectStreamEvents(
		service,
		t.Context(),
		run.ModelRequest{
			ReasoningChoice: model.ReasoningChoiceOn,
			Instructions:    "instructions",
			Model:           testModelDescriptor("gpt-test"),
			History: []agent.HistoryEntry{
				{
					Model:      model.Response{},
					ToolResult: agent.ToolResult{},
					Kind:       agent.HistoryEntryUser,
					User:       model.TextMessage("hello"),
				},
			},
			Tools: nil,
		},
		func(run.StreamEvent) error { return nil },
	)

	require.NoError(t, err)
}

// TestDriverStreamMissingCredentialsDoesNotStartOAuth verifies headless failure requires no interaction.
func TestDriverStreamMissingCredentialsDoesNotStartOAuth(t *testing.T) {
	t.Parallel()

	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(nil, false, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	service := New(testConfig(), credentials, interaction)

	events, err := collectStreamEvents(service,
		t.Context(),
		run.ModelRequest{
			ReasoningChoice: model.ReasoningChoiceOn,
			Instructions:    "instructions",
			Model:           testModelDescriptor("gpt-test"),
			History: []agent.HistoryEntry{{
				Model:      model.Response{},
				ToolResult: agent.ToolResult{},
				Kind:       agent.HistoryEntryUser,
				User:       model.TextMessage("hello"),
			}},
			Tools: nil,
		},
		func(run.StreamEvent) error { return nil },
	)
	response := terminalResponse(events)

	require.ErrorIs(t, err, ErrSignInRequired)
	assert.Equal(t, model.OutcomeFailed, response.Outcome.OrEmpty())
	assert.Equal(t, signInRequiredMessage, response.ErrorMessage.OrEmpty())
}

// TestDriverStreamLoadsCredentialsForEveryRequest verifies access data is never cached across model calls.
func TestDriverStreamLoadsCredentialsForEveryRequest(t *testing.T) {
	t.Parallel()

	accountID := "account-fresh-load"
	firstAccess := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		"request":                     1,
	})
	secondAccess := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		"request":                     2,
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	gomock.InOrder(
		credentials.EXPECT().Load().Return(
			testCredentialPayload(
				t,
				firstAccess,
				"refresh",
				accountID,
				time.Now().Add(time.Hour),
			), true, nil,
		),
		credentials.EXPECT().Load().Return(
			testCredentialPayload(
				t,
				secondAccess,
				"refresh",
				accountID,
				time.Now().Add(time.Hour),
			), true, nil,
		),
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	authorizations := make(chan string, 2)
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			authorizations <- request.Header.Get("Authorization")
			writeSSE(writer, completedEvent(
				`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[]}]`,
			))
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(
		testConfig(), credentials, interaction, testProviderOptions(server),
	)
	request := run.ModelRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("model"),
		History: []agent.HistoryEntry{
			{
				Model:      model.Response{},
				ToolResult: agent.ToolResult{},
				Kind:       agent.HistoryEntryUser,
				User:       model.TextMessage("hello"),
			},
		},
		Tools: nil,
	}

	_, firstErr := collectStreamEvents(
		service,
		t.Context(),
		request,
		func(run.StreamEvent) error { return nil },
	)
	_, secondErr := collectStreamEvents(
		service,
		t.Context(),
		request,
		func(run.StreamEvent) error { return nil },
	)

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, "Bearer "+firstAccess, <-authorizations)
	assert.Equal(t, "Bearer "+secondAccess, <-authorizations)
}

// TestDriverStreamHTTPFailuresDoNotRetry verifies safe 401 and one-attempt provider errors.
func TestDriverStreamHTTPFailuresDoNotRetry(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		status       int
		body         string
		expectedText string
	}{
		"unauthorized": {
			status:       http.StatusUnauthorized,
			body:         `{"detail":"expired token"}`,
			expectedText: signInRequiredMessage,
		},
		"server error": {
			status:       http.StatusInternalServerError,
			body:         `{"error":{"message":"backend unavailable"}}`,
			expectedText: "backend unavailable",
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
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
				http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					requests.Add(1)
					writer.Header().Set("Content-Type", "application/json")
					writer.WriteHeader(testCase.status)
					_, _ = writer.Write([]byte(testCase.body))
				}),
			)
			t.Cleanup(server.Close)
			service := newDriver(
				testConfig(),
				credentials,
				interaction,
				testProviderOptions(server),
			)

			events, err := collectStreamEvents(
				service,
				t.Context(),
				run.ModelRequest{
					ReasoningChoice: model.ReasoningChoiceOn,
					Instructions:    "instructions",
					Model:           testModelDescriptor("gpt-test"),
					History: []agent.HistoryEntry{
						{
							Model:      model.Response{},
							ToolResult: agent.ToolResult{},
							Kind:       agent.HistoryEntryUser,
							User:       model.TextMessage("hello"),
						},
					},
					Tools: nil,
				},
				func(run.StreamEvent) error { return nil },
			)
			response := terminalResponse(events)

			require.Error(t, err)
			assert.Equal(t, model.OutcomeFailed, response.Outcome.OrEmpty())
			assert.Contains(t, response.ErrorMessage.OrEmpty(), testCase.expectedText)
			assert.Equal(t, int32(1), requests.Load())
		})
	}
}

// TestDriverStreamMapsIncompleteAndFailedOutcomes verifies terminal SSE status mapping.
func TestDriverStreamMapsIncompleteAndFailedOutcomes(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		event           string
		expectedOutcome model.Outcome
		expectsError    bool
	}{
		"length": {
			event:           `{"type":"response.incomplete","response":{"id":"resp","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}}`,
			expectedOutcome: model.OutcomeLength,
			expectsError:    false,
		},
		"failure": {
			event:           `{"type":"response.failed","response":{"id":"resp","status":"failed","error":{"code":"server_error","message":"safe failure"},"output":[]}}`,
			expectedOutcome: model.OutcomeFailed,
			expectsError:    true,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
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
				http.HandlerFunc(
					func(writer http.ResponseWriter, _ *http.Request) { writeSSE(writer, testCase.event) },
				),
			)
			t.Cleanup(server.Close)
			service := newDriver(
				testConfig(),
				credentials,
				interaction,
				testProviderOptions(server),
			)

			events, err := collectStreamEvents(
				service,
				t.Context(),
				run.ModelRequest{
					ReasoningChoice: model.ReasoningChoiceOn,
					Instructions:    "instructions",
					Model:           testModelDescriptor("gpt-test"),
					History: []agent.HistoryEntry{
						{
							Model:      model.Response{},
							ToolResult: agent.ToolResult{},
							Kind:       agent.HistoryEntryUser,
							User:       model.TextMessage("hello"),
						},
					},
					Tools: nil,
				},
				func(run.StreamEvent) error { return nil },
			)
			response := terminalResponse(events)

			if testCase.expectsError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, testCase.expectedOutcome, response.Outcome.OrEmpty())
		})
	}
}

// TestDriverStreamCancellationMapsAborted verifies request cancellation terminates the SSE stream.
func TestDriverStreamCancellationMapsAborted(t *testing.T) {
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
	started := make(chan struct{})
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/event-stream")
			writer.WriteHeader(http.StatusOK)
			writer.(http.Flusher).Flush()
			close(started)
			<-request.Context().Done()
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan struct {
		response model.Response
		err      error
	}, 1)
	go func() {
		events, err := collectStreamEvents(
			service,
			ctx,
			run.ModelRequest{
				ReasoningChoice: model.ReasoningChoiceOn,
				Instructions:    "instructions",
				Model:           testModelDescriptor("gpt-test"),
				History: []agent.HistoryEntry{
					{
						Model:      model.Response{},
						ToolResult: agent.ToolResult{},
						Kind:       agent.HistoryEntryUser,
						User:       model.TextMessage("hello"),
					},
				},
				Tools: nil,
			},
			func(run.StreamEvent) error { return nil },
		)
		response := terminalResponse(events)
		result <- struct {
			response model.Response
			err      error
		}{
			response: response,
			err:      err,
		}
	}()
	select {
	case <-started:
		cancel()
		terminal := <-result
		require.ErrorIs(t, terminal.err, context.Canceled)
		assert.Equal(t, model.OutcomeAborted, terminal.response.Outcome.OrEmpty())
	case terminal := <-result:
		cancel()
		require.ErrorIs(t, terminal.err, context.Canceled)
		assert.Equal(t, model.OutcomeAborted, terminal.response.Outcome.OrEmpty())
	}
}

// TestDriverCheckAuthenticationUsesProviderOwnedClassification verifies Host preflight ownership.
func TestDriverCheckAuthenticationUsesProviderOwnedClassification(t *testing.T) {
	t.Parallel()

	t.Run("usable credentials", func(t *testing.T) {
		t.Parallel()
		accountID := "account"
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
		service := newDriver(testConfig(), credentials, interaction, defaultDriverOptions())

		err := service.CheckProviderAuthentication(t.Context())

		require.NoError(t, err)
	})

	t.Run("missing credentials", func(t *testing.T) {
		t.Parallel()
		credentials := NewMockCredentials(gomock.NewController(t))
		credentials.EXPECT().Load().Return(nil, false, nil)
		interaction := NewMockInteraction(gomock.NewController(t))
		service := newDriver(testConfig(), credentials, interaction, defaultDriverOptions())

		err := service.CheckProviderAuthentication(t.Context())

		require.ErrorIs(t, err, ErrSignInRequired)
		assert.True(t, service.IsProviderSignInRequired(err))
	})

	t.Run("malformed credentials", func(t *testing.T) {
		t.Parallel()
		credentials := NewMockCredentials(gomock.NewController(t))
		credentials.EXPECT().Load().Return([]byte("not-json"), true, nil)
		interaction := NewMockInteraction(gomock.NewController(t))
		service := newDriver(testConfig(), credentials, interaction, defaultDriverOptions())

		err := service.CheckProviderAuthentication(t.Context())

		require.ErrorIs(t, err, ErrSignInRequired)
		assert.True(t, service.IsProviderSignInRequired(err))
	})
}

// testConfig creates one provider-owned configuration fixture.
func testConfig() Config {
	return Config{
		ReasoningCompatibilityKeys: nil,
		Hooks:                      testProviderHookRunner(),
		Models: []model.ID{
			"gpt-request", "gpt-test", "gpt-selected", "gpt-unknown", "model", "gpt-5.6-luna",
		},
	}
}

// testModelDescriptor creates an explicitly capable model fixture for adapter tests.
func testModelDescriptor(modelID string) model.Descriptor {
	return model.Descriptor{
		ReasoningCapabilities: model.ReasoningCapabilities{},
		Provider:              ProviderID,
		Model:                 model.ID(modelID),
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: true,
			Grammar: model.GrammarCapabilities{
				Lark:  true,
				Regex: true,
			},
		},
	}
}

func testProviderHookRunner() internalhooks.ProviderRunner {
	return hookrunner.New(nil, nil, nil)
}

// testProviderOptions points both SDK and token HTTP calls at one test server.
func testProviderOptions(server *httptest.Server) driverOptions {
	options := defaultDriverOptions()
	options.modelBaseURL = server.URL
	options.httpClient = server.Client()
	return options
}

// testCredentialPayload encodes one provider-owned credential fixture.
func testCredentialPayload(
	t *testing.T,
	accessToken, refreshToken, accountID string,
	expiresAt time.Time,
) []byte {
	t.Helper()
	payload, err := json.Marshal(oauthCredentials{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		AccountID:    accountID,
		ExpiresAt:    expiresAt,
	})
	require.NoError(t, err)
	return payload
}

// inputTypes returns the ordered Responses item discriminators from a captured request.
func inputTypes(input []any) []string {
	return lo.Map(input, func(item any, _ int) string {
		value, _ := item.(map[string]any)["type"].(string)
		return value
	})
}

// completedEvent constructs one successful terminal Responses SSE event.
func completedEvent(output string) string {
	return `{"type":"response.completed","response":{"id":"resp","status":"completed","output":` + output + `}}`
}

// writeSSE writes typed SDK events without a custom parser in production.
func writeSSE(writer http.ResponseWriter, events ...string) {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.WriteHeader(http.StatusOK)
	for _, event := range events {
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(event)); err != nil {
			panic("invalid SSE test fixture: " + event)
		}
		_, _ = fmt.Fprintf(writer, "data: %s\n\n", compact.String())
	}
	_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
}

// streamEventKinds returns semantic event identities in delivery order.
func streamEventKinds(events []run.StreamEvent) []run.StreamEventKind {
	return lo.Map(events, func(event run.StreamEvent, _ int) run.StreamEventKind {
		return event.Kind
	})
}

// collectStreamEvents returns every provider event in delivery order.
func collectStreamEvents(
	service *Driver,
	ctx context.Context,
	request run.ModelRequest,
	handleEvent func(run.StreamEvent) error,
) ([]run.StreamEvent, error) {
	events := make([]run.StreamEvent, 0)
	err := service.Stream(ctx, request, func(event run.StreamEvent) error {
		events = append(events, event)
		if handleEvent != nil {
			return handleEvent(event)
		}
		return nil
	})
	return events, err
}

func terminalResponse(events []run.StreamEvent) model.Response {
	if len(events) == 0 {
		return model.Response{}
	}
	return events[len(events)-1].Response
}
