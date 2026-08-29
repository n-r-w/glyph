package codex

import (
	"encoding/json"

	"net/http"
	"net/http/httptest"

	"testing"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

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
			Model:      mo.None[model.Response](),
			ToolResult: mo.None[agent.ToolResult](),
			Kind:       agent.HistoryEntryUser,
			User:       mo.Some(model.TextMessage("first")),
		},
		{
			User:       mo.None[model.Message](),
			ToolResult: mo.None[agent.ToolResult](),
			Kind:       agent.HistoryEntryModel,
			Model: mo.Some(model.Response{
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
			}),
		},
		{
			User:  mo.None[model.Message](),
			Model: mo.None[model.Response](),
			Kind:  agent.HistoryEntryToolResult,
			ToolResult: mo.Some(agent.ToolResult{
				CallID:   "call-old",
				ToolName: "read",
				Contents: tool.TextContents("old data"),
				IsError:  false,
			}),
		},
		{
			Model:      mo.None[model.Response](),
			ToolResult: mo.None[agent.ToolResult](),
			Kind:       agent.HistoryEntryUser,
			User:       mo.Some(model.TextMessage("next")),
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
				Preview:  mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](),
				Response: mo.None[model.Response](),
				Kind:     run.StreamEventTextDelta,
				Position: mo.Some(1),
				Content: mo.Some(model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentText,
					Text:            mo.Some("ans"),
				}),
				Delta: mo.Some("ans"),
			},
			{
				Preview:  mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](),
				Response: mo.None[model.Response](),
				Kind:     run.StreamEventTextDelta,
				Position: mo.Some(1),
				Content: mo.Some(model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentText,
					Text:            mo.Some("wer"),
				}),
				Delta: mo.Some("wer"),
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

// TestDriverStreamSerializesImageAndMapsTerminalAccounting verifies terminal Codex usage is normalized into disjoint buckets.
func TestDriverStreamSerializesImageAndMapsTerminalAccounting(t *testing.T) {
	t.Parallel()

	// Arrange a Codex response with overlapping input, cache buckets, and reasoning above output.
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
				`{"type":"response.completed","response":{"id":"resp-rich","model":"gpt-actual","status":"completed","service_tier":"default","metadata":{"region":"test"},"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":99,"input_tokens_details":{"cached_tokens":4,"cache_write_tokens":1},"output_tokens_details":{"reasoning_tokens":3}},"output":[]}}`,
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))

	// Act by collecting the terminal adapter response.
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
				Input:                 nil, ContextWindow: 0, MaxTokens: 0, Pricing: mo.None[model.Pricing](),
			},
			History: []agent.HistoryEntry{
				{
					Model:      mo.None[model.Response](),
					ToolResult: mo.None[agent.ToolResult](),
					Kind:       agent.HistoryEntryUser,
					User: mo.Some(model.Message{
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
					}),
				},
			},
			Tools: nil,
		},
		func(run.StreamEvent) error { return nil },
	)
	response := terminalResponse(events)

	// Assert provider metadata is retained and usage is normalized before delivery.
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
		InputTokens:       5,
		OutputTokens:      2,
		CachedInputTokens: 4,
		CacheWriteTokens:  1,
		ReasoningTokens:   2,
		TotalTokens:       12,
	}, response.Usage.OrEmpty())
}
