//go:build integration

package compatible

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestChatCompletionsMapsRequestAndStream verifies Chat Completions normalizes usage before terminal delivery.
func (s *serviceSuite) TestChatCompletionsMapsRequestAndStream() {
	t := s.T()

	// Arrange a Chat Completions stream with cached input and a provider-derived total.
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/v1/chat/completions", request.URL.Path)
		assert.Equal(t, []string{"Bearer secret"}, request.Header.Values("Authorization"))
		assert.NoError(t, json.UnmarshalRead(request.Body, &body))
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(
			t,
			writer,
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,"delta":{"reasoning":""}}]}`,
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,"delta":{"reasoning":"think ","content":"hello "}}]}`,
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,"delta":{"refusal":"no"}}]}`,
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,`+
				`"delta":{"tool_calls":[{"index":0,"id":"call-new",`+
				`"type":"function","function":{"name":"read",`+
				`"arguments":"{\"path\":\"fi"}}]}}]}`,
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,`+
				`"delta":{"tool_calls":[{"index":0,"function":{"arguments":"le\"}`+
				`"}}]},"finish_reason":"tool_calls"}]}`,
			`{"id":"chat-1","model":"actual-model","choices":[],`+
				`"usage":{"prompt_tokens":12,"completion_tokens":7,`+
				`"total_tokens":99,"prompt_tokens_details":{"cached_tokens":3},`+
				`"completion_tokens_details":{"reasoning_tokens":2}}}`,
		)
	}))
	t.Cleanup(server.Close)

	models := map[model.ID]API{"demo": ""}
	service, err := New(Config{
		ProviderID: "openrouter", BaseURL: server.URL + "/v1", API: APIChatCompletions,
		Models: models, ReasoningFormats: map[model.ID]string{"demo": string(reasoningFormatOpenAIChat)},
		APIKey: expectAPIKey(t, "secret", nil, 1), ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	models["demo"] = APIResponses

	// Act by collecting the terminal adapter response.
	events := streamEvents(t, service, richRequest("openrouter", "demo"))

	// Assert input excludes cached tokens and total uses normalized buckets.
	require.NotEmpty(t, events)
	terminal := events[len(events)-1]
	assert.Equal(t, run.StreamEventDone, terminal.Kind)
	assert.Equal(t, model.OutcomeToolUse, terminal.Response.OrEmpty().Outcome.OrEmpty())
	assert.Equal(
		t,
		model.Usage{
			InputTokens:       9,
			OutputTokens:      7,
			CachedInputTokens: 3,
			ReasoningTokens:   2,
			TotalTokens:       19,
			CacheWriteTokens:  0,
		},
		terminal.Response.OrEmpty().Usage.OrEmpty(),
	)
	assert.Equal(t, "actual-model", string(terminal.Response.OrEmpty().ResponseModel.OrEmpty()))
	assert.Equal(t, "high", body["reasoning_effort"])
	assert.Equal(t, false, body["parallel_tool_calls"])
	messages := body["messages"].([]any)
	assert.Equal(t, "system", messages[0].(map[string]any)["role"])
	assert.Equal(t, "user", messages[1].(map[string]any)["role"])
	assert.Equal(t, "assistant", messages[2].(map[string]any)["role"])
	assert.Equal(t, "tool", messages[3].(map[string]any)["role"])
	assert.Len(t, body["tools"], 1)
	assert.Contains(t, eventKinds(events), run.StreamEventTextDelta)
	assert.Contains(t, eventKinds(events), run.StreamEventToolCallDelta)
	require.GreaterOrEqual(t, len(terminal.Response.OrEmpty().Content), 2)
	assert.Equal(
		t,
		model.Content{
			Kind:            model.ContentReasoning,
			Text:            mo.Some("think "),
			Final:           true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
		terminal.Response.OrEmpty().Content[0],
	)
	assert.Equal(
		t,
		model.Content{
			Kind:            model.ContentText,
			Text:            mo.Some("hello "),
			Final:           true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
		terminal.Response.OrEmpty().Content[1],
	)
	assert.Contains(
		t,
		terminal.Response.OrEmpty().Content,
		model.Content{
			Kind:            model.ContentRefusal,
			Text:            mo.Some("no"),
			Final:           true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
	)
	assert.Contains(
		t,
		terminal.Response.OrEmpty().Content,
		model.Content{
			Kind:  model.ContentToolCall,
			Final: true,
			ToolCall: mo.Some(
				model.ToolCall{ID: "call-new", Name: "read", Arguments: map[string]any{"path": "file"}},
			),
			Text:            mo.None[string](),
			ProviderContext: mo.None[model.ProviderContext](),
		},
	)
}

// TestOpenRouterRequestsIncludeAttributionHeaders verifies both supported APIs identify Glyph to OpenRouter.
func (s *serviceSuite) TestOpenRouterRequestsIncludeAttributionHeaders() {
	t := s.T()

	for _, testCase := range []struct {
		name     string
		api      API
		path     string
		response string
	}{
		{
			name: "Chat Completions", api: APIChatCompletions, path: "/chat/completions",
			response: `{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		},
		{
			name: "Responses", api: APIResponses, path: "/responses",
			response: `{"type":"response.completed","response":{"id":"response",` +
				`"status":"completed","output":[]}}`,
		},
	} {
		s.Run(testCase.name, func() {
			// Arrange an OpenRouter-compatible endpoint that validates request attribution.
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				assert.Equal(t, testCase.path, request.URL.Path)
				assert.Equal(t, "glyph", request.Header.Get("User-Agent"))
				assert.Equal(t, "https://github.com/n-r-w/glyph", request.Header.Get("HTTP-Referer"))
				assert.Equal(t, "Glyph", request.Header.Get("X-OpenRouter-Title"))
				assert.Equal(t, "cli-agent", request.Header.Get("X-OpenRouter-Categories"))
				writer.Header().Set("Content-Type", "text/event-stream")
				writeSSE(t, writer, testCase.response)
			}))
			t.Cleanup(server.Close)
			service, err := New(Config{
				ProviderID: "openrouter", BaseURL: server.URL, API: testCase.api,
				Models: map[model.ID]API{"demo": ""}, ReasoningFormats: nil,
				ReasoningCompatibilityKeys: nil, APIKey: expectAPIKey(t, "secret", nil, 1),
			})
			require.NoError(t, err)

			// Act by sending one request through the selected API.
			events := streamEvents(t, service, richRequest("openrouter", "demo"))

			// Assert the provider completed after receiving the expected headers.
			require.NotEmpty(t, events)
			assert.Equal(t, run.StreamEventDone, events[len(events)-1].Kind)
		})
	}
}

// TestChatReasoningMapsChoices verifies each Chat Completions format maps off, effort, and fixed-on choices.
func (s *serviceSuite) TestChatReasoningMapsChoices() {
	for _, testCase := range []struct {
		name                    string
		format                  string
		choice                  model.ReasoningChoice
		expectedReasoningEffort string
		expectedReasoning       map[string]any
	}{
		{
			name: "OpenAI off", format: "openai-chat", choice: model.ReasoningChoiceOff,
			expectedReasoningEffort: "none", expectedReasoning: nil,
		},
		{
			name: "OpenAI effort", format: "openai-chat", choice: model.ReasoningChoiceLow,
			expectedReasoningEffort: "low", expectedReasoning: nil,
		},
		{
			name: "OpenAI on", format: "openai-chat", choice: model.ReasoningChoiceOn,
			expectedReasoningEffort: "", expectedReasoning: nil,
		},
		{
			name: "OpenRouter off", format: "openrouter", choice: model.ReasoningChoiceOff,
			expectedReasoningEffort: "", expectedReasoning: map[string]any{"effort": "none"},
		},
		{
			name: "OpenRouter effort", format: "openrouter", choice: model.ReasoningChoiceHigh,
			expectedReasoningEffort: "", expectedReasoning: map[string]any{"effort": "high"},
		},
		{
			name: "OpenRouter on", format: "openrouter", choice: model.ReasoningChoiceOn,
			expectedReasoningEffort: "", expectedReasoning: map[string]any{"enabled": true},
		},
	} {
		s.Run(testCase.name, func() {
			t := s.T()

			// Arrange a server that records one request and returns a terminal stream chunk.
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if !assert.NoError(t, json.UnmarshalRead(request.Body, &body)) {
					return
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				writeSSE(t, writer, `{"id":"chat-choice","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
			}))
			t.Cleanup(server.Close)
			driver, err := New(Config{
				ProviderID: "local", BaseURL: server.URL, API: APIChatCompletions,
				Models:           map[model.ID]API{"demo": ""},
				ReasoningFormats: map[model.ID]string{"demo": testCase.format},
				APIKey:           expectAPIKey(t, "", nil, 1), ReasoningCompatibilityKeys: nil,
			})
			s.Require().NoError(err)
			request := richRequest("local", "demo")
			request.ReasoningChoice = testCase.choice

			// Act by sending the selected reasoning choice.
			events := streamEvents(t, driver, request)

			// Assert the format owns its request fields and does not leak the other format's fields.
			s.Equal(run.StreamEventDone, events[len(events)-1].Kind)
			if testCase.expectedReasoningEffort == "" {
				s.NotContains(body, "reasoning_effort")
			} else {
				s.Equal(testCase.expectedReasoningEffort, body["reasoning_effort"])
			}
			if testCase.expectedReasoning == nil {
				s.NotContains(body, "reasoning")
			} else {
				s.Equal(testCase.expectedReasoning, body["reasoning"])
			}
		})
	}
}

// TestOpenRouterReasoningDetailsRoundTrip verifies visible reasoning and opaque details survive tool continuation.
func (s *serviceSuite) TestOpenRouterReasoningDetailsRoundTrip() {
	t := s.T()

	// Arrange two sequential OpenRouter requests and fragment structured reasoning before a tool call.
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if !assert.NoError(t, json.UnmarshalRead(request.Body, &body)) {
			return
		}
		bodies = append(bodies, body)
		writer.Header().Set("Content-Type", "text/event-stream")
		if len(bodies) == 1 {
			writeSSE(
				t,
				writer,
				`{"id":"chat-openrouter","choices":[{"index":0,`+
					`"delta":{"reasoning":"think ",`+
					`"reasoning_details":[{"type":"reasoning.text","text":"part ",`+
					`"id":null,"format":"","index":null,"signature":""}]}}]}`,
				`{"id":"chat-openrouter","choices":[{"index":0,`+
					`"delta":{"reasoning":"more",`+
					`"reasoning_details":[{"type":"reasoning.text","text":"two",`+
					`"id":"r1","format":"unknown","index":0,"signature":"sig"},`+
					`{"type":"reasoning.summary","summary":"sum ","id":null,`+
					`"format":"","index":null}]}}]}`,
				`{"id":"chat-openrouter","choices":[{"index":0,`+
					`"delta":{"reasoning_details":[{"type":"reasoning.summary",`+
					`"summary":"mary","id":"s1","format":"unknown","index":1},`+
					`{"type":"reasoning.encrypted","data":"cipher","id":"e1",`+
					`"format":"unknown","index":2,"vendor":{"x":1}}],`+
					`"tool_calls":[{"index":0,"id":"call-new","type":"function",`+
					`"function":{"name":"read","arguments":"{\"path\":\"file\"}"}}]},`+
					`"finish_reason":"tool_calls"}]}`,
			)
			return
		}
		writeSSE(
			t,
			writer,
			`{"id":"chat-next","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`,
		)
	}))
	t.Cleanup(server.Close)
	driver, err := New(Config{
		ProviderID: "openrouter", BaseURL: server.URL, API: APIChatCompletions,
		Models:           map[model.ID]API{"demo": ""},
		ReasoningFormats: map[model.ID]string{"demo": string(reasoningFormatOpenRouter)},
		ReasoningCompatibilityKeys: map[model.ID]mo.Option[string]{
			"demo": mo.Some("shared"),
		},
		APIKey: expectAPIKey(t, "", nil, 2),
	})
	require.NoError(t, err)

	// Act by streaming one tool call and replaying its terminal response with the tool result.
	firstRequest := richRequest("openrouter", "demo")
	firstEvents := streamEvents(t, driver, firstRequest)
	firstResponse := firstEvents[len(firstEvents)-1].Response.OrEmpty()
	secondRequest := richRequest("openrouter", "demo")
	replaceHistoryModelContent(&secondRequest, firstResponse.Content)
	secondRequest.History[2].ToolResult = mo.Some(agent.ToolResult{
		CallID: "call-new", ToolName: "read", Contents: tool.TextContents("done"), IsError: false,
	})
	secondEvents := streamEvents(t, driver, secondRequest)

	// Assert visible reasoning is provider-neutral and replay restores the merged opaque detail array exactly.
	require.Equal(t, run.StreamEventDone, firstEvents[len(firstEvents)-1].Kind)
	require.Equal(t, run.StreamEventDone, secondEvents[len(secondEvents)-1].Kind)
	require.Len(t, bodies, 2)
	require.NotEmpty(t, firstResponse.Content)
	var reasoningContent model.Content
	for _, content := range firstResponse.Content {
		if content.Kind == model.ContentReasoning {
			reasoningContent = content
			break
		}
	}
	assert.Equal(t, "think more", reasoningContent.Text.OrEmpty())
	providerContext, present := reasoningContent.ProviderContext.Get()
	require.True(t, present)
	assert.Equal(t, model.ProviderContextSource{
		ProviderID: "openrouter", API: "chat-completions", Model: "demo", CompatibilityKey: mo.Some("shared"),
	}, providerContext.Source)
	var capturedDetails []map[string]any
	require.NoError(t, json.Unmarshal(providerContext.Payload, &capturedDetails))
	expectedDetails := []map[string]any{
		{
			"type":      "reasoning.text",
			"text":      "part two",
			"id":        "r1",
			"format":    "unknown",
			"index":     float64(0),
			"signature": "sig",
		},
		{"type": "reasoning.summary", "summary": "sum mary", "id": "s1", "format": "unknown", "index": float64(1)},
		{
			"type":   "reasoning.encrypted",
			"data":   "cipher",
			"id":     "e1",
			"format": "unknown",
			"index":  float64(2),
			"vendor": map[string]any{"x": float64(1)},
		},
	}
	assert.Equal(t, expectedDetails, capturedDetails)
	messages := bodies[1]["messages"].([]any)
	assistant := messages[2].(map[string]any)
	assert.Equal(t, []any{expectedDetails[0], expectedDetails[1], expectedDetails[2]}, assistant["reasoning_details"])
	assert.NotContains(t, assistant, "reasoning")
}

// TestChatHistoryUsesNativeReasoningOrTextFallback verifies visible replay and opaque-context filtering.
func (s *serviceSuite) TestChatHistoryUsesNativeReasoningOrTextFallback() {
	for _, testCase := range []struct {
		name            string
		format          string
		expectedContent string
		expectedReason  string
	}{
		{name: "native", format: string(reasoningFormatOpenAIChat), expectedContent: "answer", expectedReason: "firstsecond"},
		{name: "text fallback", format: "", expectedContent: "firstanswersecond", expectedReason: ""},
	} {
		s.Run(testCase.name, func() {
			t := s.T()
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if !assert.NoError(t, json.UnmarshalRead(request.Body, &body)) {
					return
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				writeSSE(
					t,
					writer,
					`{"id":"chat-history","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`,
				)
			}))
			t.Cleanup(server.Close)
			formats := map[model.ID]string(nil)
			if testCase.format != "" {
				formats = map[model.ID]string{"demo": testCase.format}
			}
			driver, err := New(Config{
				ProviderID: "local", BaseURL: server.URL, API: APIChatCompletions,
				Models:           map[model.ID]API{"demo": ""},
				ReasoningFormats: formats,
				APIKey:           expectAPIKey(t, "", nil, 1), ReasoningCompatibilityKeys: nil,
			})
			s.Require().NoError(err)
			request := richRequest("local", "demo")
			replaceHistoryModelContent(&request, []model.Content{
				{
					Kind:  model.ContentReasoning,
					Text:  mo.Some("first"),
					Final: true,
					ProviderContext: mo.Some(model.ProviderContext{
						Source: model.ProviderContextSource{
							ProviderID:       "other",
							API:              "responses",
							Model:            "source",
							CompatibilityKey: mo.None[string](),
						},
						Payload: []byte("opaque-secret"),
					}),
					ToolCall: mo.None[model.ToolCall](),
				},
				{
					Kind:            model.ContentText,
					Text:            mo.Some("answer"),
					Final:           true,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
				},
				{
					Kind:            model.ContentReasoning,
					Text:            mo.Some(""),
					Final:           true,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
				},
				{
					Kind:            model.ContentReasoning,
					Text:            mo.Some("second"),
					Final:           true,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
				},
			})
			request.History = append(request.History, agent.HistoryEntry{
				Kind: agent.HistoryEntryModel,
				Model: mo.Some(
					model.Response{
						Content: []model.Content{
							{
								Kind:            model.ContentReasoning,
								Text:            mo.Some(""),
								Final:           true,
								ProviderContext: mo.None[model.ProviderContext](),
								ToolCall:        mo.None[model.ToolCall](),
							},
						},
						Outcome:       mo.None[model.Outcome](),
						ErrorMessage:  mo.None[string](),
						Provider:      mo.None[model.ProviderID](),
						Model:         mo.None[model.ID](),
						ResponseModel: mo.None[model.ID](),
						ResponseID:    mo.None[string](),
						Usage:         mo.None[model.Usage](),
						Diagnostics:   nil,
					},
				),
				User:       mo.None[model.Message](),
				ToolResult: mo.None[agent.ToolResult](),
			})

			events := streamEvents(t, driver, request)

			s.Equal(run.StreamEventDone, events[len(events)-1].Kind)
			messages := body["messages"].([]any)
			s.Len(messages, 4)
			assistant := messages[2].(map[string]any)
			s.Equal(testCase.expectedContent, assistant["content"])
			if testCase.expectedReason == "" {
				s.NotContains(assistant, "reasoning")
			} else {
				s.Equal(testCase.expectedReason, assistant["reasoning"])
			}
			encoded, encodeErr := json.Marshal(body)
			s.Require().NoError(encodeErr)
			s.NotContains(string(encoded), "opaque-secret")
		})
	}
}

// TestChatReasoningSupportsFixedOn verifies its control-free request, shared stream parser, and native replay.
func (s *serviceSuite) TestChatReasoningSupportsFixedOn() {
	t := s.T()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, json.UnmarshalRead(request.Body, &body)) {
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(
			t,
			writer,
			`{"id":"fixed","choices":[{"index":0,"delta":{"reasoning":""}}]}`,
			`{"id":"fixed","choices":[{"index":0,"delta":{"reasoning":"think ","content":"answer"},"finish_reason":"stop"}]}`,
		)
	}))
	t.Cleanup(server.Close)
	driver, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIChatCompletions,
		Models:           map[model.ID]API{"fixed": ""},
		ReasoningFormats: map[model.ID]string{"fixed": string(reasoningFormatOpenAIChat)},
		APIKey:           expectAPIKey(t, "", nil, 1), ReasoningCompatibilityKeys: nil,
	})
	s.Require().NoError(err)
	request := richRequest("local", "fixed")
	request.ReasoningChoice = model.ReasoningChoiceOn
	replaceHistoryModelContent(&request, []model.Content{
		{
			Kind:  model.ContentReasoning,
			Text:  mo.Some("earlier"),
			Final: true,
			ProviderContext: mo.Some(model.ProviderContext{
				Source: model.ProviderContextSource{
					ProviderID:       "other",
					API:              "responses",
					Model:            "source",
					CompatibilityKey: mo.None[string](),
				},
				Payload: []byte("opaque-secret"),
			}),
			ToolCall: mo.None[model.ToolCall](),
		},
		{
			Kind:            model.ContentText,
			Text:            mo.Some("history"),
			Final:           true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
	})

	events := streamEvents(t, driver, request)

	s.Require().GreaterOrEqual(len(events), 7)
	s.Equal(run.StreamEventContentStart, events[0].Kind)
	s.Equal(model.ContentReasoning, events[0].Content.OrEmpty().Kind)
	s.Equal(run.StreamEventTextDelta, events[1].Kind)
	s.Equal("think ", events[1].Delta.OrEmpty())
	s.Equal(run.StreamEventContentStart, events[2].Kind)
	s.Equal(model.ContentText, events[2].Content.OrEmpty().Kind)
	s.Equal(run.StreamEventTextDelta, events[3].Kind)
	s.Equal("answer", events[3].Delta.OrEmpty())
	s.Equal(run.StreamEventContentEnd, events[4].Kind)
	s.Equal(model.ContentReasoning, events[4].Content.OrEmpty().Kind)
	s.Equal(run.StreamEventContentEnd, events[5].Kind)
	s.Equal(model.ContentText, events[5].Content.OrEmpty().Kind)
	terminal := events[len(events)-1]
	s.Equal(run.StreamEventDone, terminal.Kind)
	s.Equal([]model.Content{
		{
			Kind:            model.ContentReasoning,
			Text:            mo.Some("think "),
			Final:           true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
		{
			Kind:            model.ContentText,
			Text:            mo.Some("answer"),
			Final:           true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
	}, terminal.Response.OrEmpty().Content)
	s.NotContains(body, "reasoning_effort")
	s.NotContains(body, "reasoning")
	assistant := body["messages"].([]any)[2].(map[string]any)
	s.Equal("earlier", assistant["reasoning"])
	s.Equal("history", assistant["content"])
	encoded, encodeErr := json.Marshal(body)
	s.Require().NoError(encodeErr)
	s.NotContains(string(encoded), "opaque-secret")
}
