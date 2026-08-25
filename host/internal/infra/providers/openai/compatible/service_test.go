package compatible

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostproviders "github.com/n-r-w/glyph/host/internal/usecase/host/providers"
)

type serviceSuite struct{ suite.Suite }

func expectAPIKey(t *testing.T, key string, resolveErr error, calls int) *MockAPIKeyResolver {
	t.Helper()
	resolver := NewMockAPIKeyResolver(gomock.NewController(t))
	resolver.EXPECT().ResolveAPIKey(gomock.Any()).Return(key, resolveErr).Times(calls)
	return resolver
}

func TestDriverSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(serviceSuite))
}

func (s *serviceSuite) TestChatCompletionsMapsRequestAndStream() {
	t := s.T()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/v1/chat/completions", request.URL.Path)
		assert.Equal(t, []string{"Bearer secret"}, request.Header.Values("Authorization"))
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer,
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,"delta":{"reasoning":""}}]}`,
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,"delta":{"reasoning":"think ","content":"hello "}}]}`,
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,"delta":{"refusal":"no"}}]}`,
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-new","type":"function","function":{"name":"read","arguments":"{\"path\":\"fi"}}]}}]}`,
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"le\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"id":"chat-1","model":"actual-model","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":2}}}`,
		)
	}))
	t.Cleanup(server.Close)

	models := map[model.ID]API{"demo": ""}
	service, err := New(Config{
		ProviderID: "openrouter", BaseURL: server.URL + "/v1", API: APIChatCompletions,
		Models: models, ReasoningWireFormats: map[model.ID]string{"demo": reasoningWireFormatOpenAIChatEffort},
		APIKey: expectAPIKey(t, "secret", nil, 1), ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	models["demo"] = APIResponses

	events := streamEvents(t, service, richRequest("openrouter", "demo"))
	require.NotEmpty(t, events)
	terminal := events[len(events)-1]
	assert.Equal(t, run.StreamEventDone, terminal.Kind)
	assert.Equal(t, model.OutcomeToolUse, terminal.Response.Outcome.OrEmpty())
	assert.Equal(t, model.Usage{InputTokens: 12, OutputTokens: 7, CachedInputTokens: 3, ReasoningTokens: 2, TotalTokens: 19, CacheWriteTokens: 0}, terminal.Response.Usage.OrEmpty())
	assert.Equal(t, "actual-model", string(terminal.Response.ResponseModel.OrEmpty()))
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
	require.GreaterOrEqual(t, len(terminal.Response.Content), 2)
	assert.Equal(t, model.Content{Kind: model.ContentReasoning, Text: mo.Some("think "), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}, terminal.Response.Content[0])
	assert.Equal(t, model.Content{Kind: model.ContentText, Text: mo.Some("hello "), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}, terminal.Response.Content[1])
	assert.Contains(t, terminal.Response.Content, model.Content{Kind: model.ContentRefusal, Text: mo.Some("no"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()})
	assert.Contains(t, terminal.Response.Content, model.Content{Kind: model.ContentToolCall, Final: true, ToolCall: mo.Some(model.ToolCall{ID: "call-new", Name: "read", Arguments: map[string]any{"path": "file"}}), Text: mo.None[string](), ProviderContext: mo.None[model.ProviderContext]()})
}

// TestChatEffortMapsChoices verifies the closed choice-to-field mapping for Chat Completions.
func (s *serviceSuite) TestChatEffortMapsChoices() {
	for _, testCase := range []struct {
		name     string
		choice   model.ReasoningChoice
		expected string
		present  bool
	}{
		{name: "off", choice: model.ReasoningChoiceOff, expected: "none", present: true},
		{name: "effort", choice: model.ReasoningChoiceLow, expected: "low", present: true},
		{name: "on", choice: model.ReasoningChoiceOn, present: false, expected: ""},
	} {
		s.Run(testCase.name, func() {
			t := s.T()
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if !assert.NoError(t, json.NewDecoder(request.Body).Decode(&body)) {
					return
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				writeSSE(t, writer, `{"id":"chat-choice","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
			}))
			t.Cleanup(server.Close)
			driver, err := New(Config{
				ProviderID: "local", BaseURL: server.URL, API: APIChatCompletions,
				Models:               map[model.ID]API{"demo": ""},
				ReasoningWireFormats: map[model.ID]string{"demo": reasoningWireFormatOpenAIChatEffort},
				APIKey:               expectAPIKey(t, "", nil, 1), ReasoningCompatibilityKeys: nil,
			})
			s.Require().NoError(err)
			request := richRequest("local", "demo")
			request.ReasoningChoice = testCase.choice

			events := streamEvents(t, driver, request)

			s.Equal(run.StreamEventDone, events[len(events)-1].Kind)
			value, present := body["reasoning_effort"]
			s.Equal(testCase.present, present)
			if testCase.present {
				s.Equal(testCase.expected, value)
			}
		})
	}
}

// TestChatHistoryUsesNativeReasoningOrTextFallback verifies visible replay and opaque-context filtering.
func (s *serviceSuite) TestChatHistoryUsesNativeReasoningOrTextFallback() {
	for _, testCase := range []struct {
		name            string
		wireFormat      string
		expectedContent string
		expectedReason  string
	}{
		{name: "native", wireFormat: reasoningWireFormatOpenAIChatEffort, expectedContent: "answer", expectedReason: "firstsecond"},
		{name: "text fallback", wireFormat: "", expectedContent: "firstanswersecond", expectedReason: ""},
	} {
		s.Run(testCase.name, func() {
			t := s.T()
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if !assert.NoError(t, json.NewDecoder(request.Body).Decode(&body)) {
					return
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				writeSSE(t, writer, `{"id":"chat-history","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`)
			}))
			t.Cleanup(server.Close)
			driver, err := New(Config{
				ProviderID: "local", BaseURL: server.URL, API: APIChatCompletions,
				Models:               map[model.ID]API{"demo": ""},
				ReasoningWireFormats: map[model.ID]string{"demo": testCase.wireFormat},
				APIKey:               expectAPIKey(t, "", nil, 1), ReasoningCompatibilityKeys: nil,
			})
			s.Require().NoError(err)
			request := richRequest("local", "demo")
			replaceHistoryModelContent(&request, []model.Content{
				{Kind: model.ContentReasoning, Text: mo.Some("first"), Final: true, ProviderContext: mo.Some(model.ProviderContext{
					Source:  model.ProviderContextSource{ProviderID: "other", API: "responses", Model: "source", CompatibilityKey: mo.None[string]()},
					Payload: []byte("opaque-secret"),
				}), ToolCall: mo.None[model.ToolCall](),
				},
				{Kind: model.ContentText, Text: mo.Some("answer"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
				{Kind: model.ContentReasoning, Text: mo.Some(""), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
				{Kind: model.ContentReasoning, Text: mo.Some("second"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
			})
			request.History = append(request.History, agent.HistoryEntry{
				Kind:  agent.HistoryEntryModel,
				Model: mo.Some(model.Response{Content: []model.Content{{Kind: model.ContentReasoning, Text: mo.Some(""), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}}, Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil}), User: mo.None[model.Message](), ToolResult: mo.None[agent.ToolResult](),
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

// TestOrnithUsesFixedNativeReasoning verifies its control-free request, shared stream parser, and native replay.
func (s *serviceSuite) TestOrnithUsesFixedNativeReasoning() {
	t := s.T()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, json.NewDecoder(request.Body).Decode(&body)) {
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer,
			`{"id":"ornith","choices":[{"index":0,"delta":{"reasoning":""}}]}`,
			`{"id":"ornith","choices":[{"index":0,"delta":{"reasoning":"think ","content":"answer"},"finish_reason":"stop"}]}`,
		)
	}))
	t.Cleanup(server.Close)
	driver, err := New(Config{
		ProviderID: "ollama", BaseURL: server.URL, API: APIChatCompletions,
		Models:               map[model.ID]API{"ornith": ""},
		ReasoningWireFormats: map[model.ID]string{"ornith": reasoningWireFormatOllamaOrnith},
		APIKey:               expectAPIKey(t, "", nil, 1), ReasoningCompatibilityKeys: nil,
	})
	s.Require().NoError(err)
	request := richRequest("ollama", "ornith")
	request.ReasoningChoice = model.ReasoningChoiceOn
	replaceHistoryModelContent(&request, []model.Content{
		{Kind: model.ContentReasoning, Text: mo.Some("earlier"), Final: true, ProviderContext: mo.Some(model.ProviderContext{
			Source: model.ProviderContextSource{ProviderID: "other", API: "responses", Model: "source", CompatibilityKey: mo.None[string]()}, Payload: []byte("opaque-secret"),
		}), ToolCall: mo.None[model.ToolCall](),
		},
		{Kind: model.ContentText, Text: mo.Some("history"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
	})

	events := streamEvents(t, driver, request)

	s.Require().GreaterOrEqual(len(events), 7)
	s.Equal(run.StreamEventContentStart, events[0].Kind)
	s.Equal(model.ContentReasoning, events[0].Content.Kind)
	s.Equal(run.StreamEventTextDelta, events[1].Kind)
	s.Equal("think ", events[1].Delta)
	s.Equal(run.StreamEventContentStart, events[2].Kind)
	s.Equal(model.ContentText, events[2].Content.Kind)
	s.Equal(run.StreamEventTextDelta, events[3].Kind)
	s.Equal("answer", events[3].Delta)
	s.Equal(run.StreamEventContentEnd, events[4].Kind)
	s.Equal(model.ContentReasoning, events[4].Content.Kind)
	s.Equal(run.StreamEventContentEnd, events[5].Kind)
	s.Equal(model.ContentText, events[5].Content.Kind)
	terminal := events[len(events)-1]
	s.Equal(run.StreamEventDone, terminal.Kind)
	s.Equal([]model.Content{
		{Kind: model.ContentReasoning, Text: mo.Some("think "), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
		{Kind: model.ContentText, Text: mo.Some("answer"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
	}, terminal.Response.Content)
	s.NotContains(body, "reasoning_effort")
	s.NotContains(body, "reasoning")
	assistant := body["messages"].([]any)[2].(map[string]any)
	s.Equal("earlier", assistant["reasoning"])
	s.Equal("history", assistant["content"])
	encoded, encodeErr := json.Marshal(body)
	s.Require().NoError(encodeErr)
	s.NotContains(string(encoded), "opaque-secret")
}

func (s *serviceSuite) TestAPIKeyResolvesBeforeEveryRequest() {
	t := s.T()
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorizations = append(authorizations, request.Header.Get("Authorization"))
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, `{"id":"chat-key","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	resolver := NewMockAPIKeyResolver(gomock.NewController(t))
	gomock.InOrder(
		resolver.EXPECT().ResolveAPIKey(gomock.Any()).Return("first-key", nil),
		resolver.EXPECT().ResolveAPIKey(gomock.Any()).Return("second-key", nil),
	)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIChatCompletions,
		Models: map[model.ID]API{"demo": ""}, APIKey: resolver, ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)

	streamEvents(t, service, richRequest("local", "demo"))
	streamEvents(t, service, richRequest("local", "demo"))

	assert.Equal(t, []string{"Bearer first-key", "Bearer second-key"}, authorizations)
}

func (s *serviceSuite) TestChatCompletionsRequiresFinishReason() {
	t := s.T()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, `{"id":"chat-no-finish","choices":[{"index":0,"delta":{"content":"partial"}}]}`)
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIChatCompletions,
		Models: map[model.ID]API{"demo": ""},
		APIKey: expectAPIKey(t, "", nil, 1), ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	assert.Equal(t, run.StreamEventError, events[len(events)-1].Kind)
	assert.Equal(t, model.OutcomeFailed, events[len(events)-1].Response.Outcome.OrEmpty())
}

func (s *serviceSuite) TestResponsesOmitsUnusableProviderContext() {
	t := s.T()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, `{"type":"response.completed","response":{"id":"resp-reasoning","status":"completed","output":[{"id":"","type":"reasoning","summary":[{"type":"summary_text","text":"visible reason"}]}]}}`)
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""},
		APIKey: expectAPIKey(t, "", nil, 1), ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	terminal := events[len(events)-1]
	assert.Contains(t, terminal.Response.Content, model.Content{
		Kind: model.ContentReasoning, Text: mo.Some("visible reason"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
	})
	require.Len(t, terminal.Response.Content, 1)
	assert.Empty(t, terminal.Response.Content[0].ProviderContext.OrEmpty().Payload)
}

func (s *serviceSuite) TestResponsesUsesOverrideAndFiltersProviderContext() {
	t := s.T()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/v1/responses", request.URL.Path)
		assert.Empty(t, request.Header.Values("Authorization"))
		assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer,
			`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"answer"}`,
			`{"type":"response.completed","response":{"id":"resp-1","model":"actual-model","status":"completed","output":[{"id":"m-1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer","annotations":[],"logprobs":[]}]}],"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8,"input_tokens_details":{"cached_tokens":1},"output_tokens_details":{"reasoning_tokens":2}}}}`,
		)
	}))
	t.Cleanup(server.Close)

	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL + "/v1", API: APIChatCompletions,
		Models:               map[model.ID]API{"demo": APIResponses},
		ReasoningWireFormats: map[model.ID]string{"demo": reasoningWireFormatOpenAIResponses},
		APIKey:               expectAPIKey(t, "", nil, 1), ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)

	request := richRequest("local", "demo")
	appendHistoryModelContent(&request,
		model.Content{Kind: model.ContentReasoning, Text: mo.Some(""), Final: true, ProviderContext: mo.Some(model.ProviderContext{Source: model.ProviderContextSource{ProviderID: "local", API: "responses", Model: "demo", CompatibilityKey: mo.None[string]()}, Payload: []byte(`{"id":"r-local","encrypted_content":"cipher","summary":["old"]}`)}), ToolCall: mo.None[model.ToolCall]()},
		model.Content{Kind: model.ContentReasoning, Text: mo.Some(""), Final: true, ProviderContext: mo.Some(model.ProviderContext{Source: model.ProviderContextSource{ProviderID: "foreign", API: "responses", Model: "demo", CompatibilityKey: mo.None[string]()}, Payload: []byte(`{"id":"r-foreign","encrypted_content":"secret","summary":[]}`)}), ToolCall: mo.None[model.ToolCall]()},
	)
	events := streamEvents(t, service, request)
	terminal := events[len(events)-1]
	assert.Equal(t, run.StreamEventDone, terminal.Kind)
	assert.Equal(t, model.OutcomeStop, terminal.Response.Outcome.OrEmpty())
	input := body["input"].([]any)
	encoded, err := json.Marshal(input)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "r-local")
	assert.NotContains(t, string(encoded), "r-foreign")
	assert.Equal(t, "high", body["reasoning"].(map[string]any)["effort"])
}

// TestResponsesReplaysContextAcrossModelsWithSharedCompatibilityKey verifies additive family compatibility.
func (s *serviceSuite) TestResponsesReplaysContextAcrossModelsWithSharedCompatibilityKey() {
	t := s.T()
	request := richRequest("local", "target-model")
	appendHistoryModelContent(&request, model.Content{
		Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true,
		ProviderContext: mo.Some(model.ProviderContext{
			Source: model.ProviderContextSource{
				ProviderID: "local", API: "responses", Model: "source-model", CompatibilityKey: mo.Some("shared-family"),
			},
			Payload: []byte(`{"id":"r-shared","encrypted_content":"shared-cipher","summary":["summary"]}`),
		}), ToolCall: mo.None[model.ToolCall](),
	})

	body := runResponsesRequest(t, request, "shared-family", "openai-responses")
	encoded, err := json.Marshal(body["input"])
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "r-shared")
	assert.Contains(t, string(encoded), "shared-cipher")
	assert.NotContains(t, string(encoded), "visible reasoning")
}

// TestResponsesReplaysExactModelAfterCompatibilityKeyChange verifies model identity takes precedence over key changes.
func (s *serviceSuite) TestResponsesReplaysExactModelAfterCompatibilityKeyChange() {
	t := s.T()
	request := richRequest("local", "same-model")
	appendHistoryModelContent(&request, model.Content{
		Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true,
		ProviderContext: mo.Some(model.ProviderContext{
			Source: model.ProviderContextSource{
				ProviderID: "local", API: "responses", Model: "same-model", CompatibilityKey: mo.Some("old-family"),
			},
			Payload: []byte(`{"id":"r-exact","encrypted_content":"exact-cipher","summary":[]}`),
		}), ToolCall: mo.None[model.ToolCall](),
	})

	body := runResponsesRequest(t, request, "new-family", "openai-responses")
	encoded, err := json.Marshal(body["input"])
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "r-exact")
	assert.Contains(t, string(encoded), "exact-cipher")
	assert.NotContains(t, string(encoded), "visible reasoning")
}

// TestResponsesOmitsIncompatibleContextAndKeepsVisibleReasoning verifies safe text fallback without opaque values.
func (s *serviceSuite) TestResponsesOmitsIncompatibleContextAndKeepsVisibleReasoning() {
	for _, testCase := range []struct {
		name   string
		source model.ProviderContextSource
	}{
		{
			name: "model family",
			source: model.ProviderContextSource{
				ProviderID: "local", API: "responses", Model: "other-model", CompatibilityKey: mo.Some("other-family"),
			},
		},
		{
			name: "provider instance",
			source: model.ProviderContextSource{
				ProviderID: "other", API: "responses", Model: "target-model", CompatibilityKey: mo.Some("target-family"),
			},
		},
		{
			name: "API",
			source: model.ProviderContextSource{
				ProviderID: "local", API: "chat-completions", Model: "target-model", CompatibilityKey: mo.Some("target-family"),
			},
		},
	} {
		s.Run(testCase.name, func() {
			t := s.T()
			request := richRequest("local", "target-model")
			appendHistoryModelContent(&request, model.Content{
				Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true,
				ProviderContext: mo.Some(model.ProviderContext{
					Source:  testCase.source,
					Payload: []byte(`{"id":"r-incompatible","encrypted_content":"private-cipher","summary":[]}`),
				}), ToolCall: mo.None[model.ToolCall](),
			})

			body := runResponsesRequest(t, request, "target-family", "openai-responses")
			encoded, err := json.Marshal(body["input"])
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "r-incompatible")
			assert.NotContains(t, string(encoded), "private-cipher")
			assert.Contains(t, string(encoded), `"role":"assistant"`)
			assert.Contains(t, string(encoded), "visible reasoning")
		})
	}
}

func (s *serviceSuite) TestResponsesStreamsRefusalAndFragmentedToolCall() {
	t := s.T()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer,
			`{"type":"response.refusal.delta","output_index":0,"content_index":0,"delta":"blocked"}`,
			`{"type":"response.output_item.added","output_index":8,"item":{"id":"item-1","type":"function_call","call_id":"call-1","name":"read","arguments":"","status":"in_progress"}}`,
			`{"type":"response.function_call_arguments.delta","output_index":8,"item_id":"item-1","delta":"{\"path\":\"fi"}`,
			`{"type":"response.function_call_arguments.delta","output_index":8,"item_id":"item-1","delta":"le\"}"}`,
			`{"type":"response.function_call_arguments.done","output_index":8,"item_id":"item-1","name":"read","arguments":"{\"path\":\"file\"}"}`,
			`{"type":"response.completed","response":{"id":"resp-2","model":"actual","status":"completed","output":[{"id":"m-1","type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"blocked"}]},{"id":"item-1","type":"function_call","call_id":"call-1","name":"read","arguments":"{\"path\":\"file\"}","status":"completed"}]}}`,
		)
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1), ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	assert.Contains(t, eventKinds(events), run.StreamEventToolCallStart)
	assert.Contains(t, eventKinds(events), run.StreamEventToolCallDelta)
	assert.Contains(t, eventKinds(events), run.StreamEventToolCallEnd)
	terminal := events[len(events)-1]
	assert.Equal(t, model.OutcomeToolUse, terminal.Response.Outcome.OrEmpty())
	assert.Contains(t, terminal.Response.Content, model.Content{Kind: model.ContentRefusal, Text: mo.Some("blocked"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()})
}

func (s *serviceSuite) TestHandlerFailureStopsWithoutTerminalEvent() {
	t := s.T()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, `{"id":"chat-1","choices":[{"index":0,"delta":{"content":"text"}}]}`)
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIChatCompletions,
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1), ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	handlerErr := errors.New("sink stopped")
	var events []run.StreamEvent
	err = service.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
		events = append(events, event)
		if event.Kind == run.StreamEventTextDelta {
			return handlerErr
		}
		return nil
	})
	require.ErrorIs(t, err, handlerErr)
	for _, event := range events {
		assert.NotContains(t, []run.StreamEventKind{run.StreamEventDone, run.StreamEventError}, event.Kind)
	}
}

func (s *serviceSuite) TestResolverFailureStartsNoRequestAndIsSafe() {
	t := s.T()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""},
		APIKey: expectAPIKey(t, "top-secret", errors.New("credential source unavailable: top-secret"), 1), ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	var events []run.StreamEvent
	err = service.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "top-secret")
	assert.Zero(t, calls.Load())
	terminal := events[len(events)-1]
	assert.Equal(t, run.StreamEventError, terminal.Kind)
	assert.NotContains(t, terminal.Response.ErrorMessage.OrEmpty(), "top-secret")
	assert.NotContains(t, terminal.Response.ErrorMessage.OrEmpty(), "credential source unavailable")
}

func (s *serviceSuite) TestConstructionAndRequestValidation() {
	t := s.T()
	resolver := NewMockAPIKeyResolver(gomock.NewController(t))
	valid := Config{
		ProviderID: "local", BaseURL: "https://example.com/v1", API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: resolver, ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "empty provider", mutate: func(config *Config) { config.ProviderID = "" }},
		{name: "relative URL", mutate: func(config *Config) { config.BaseURL = "/v1" }},
		{name: "unknown API", mutate: func(config *Config) { config.API = "legacy" }},
		{name: "no models", mutate: func(config *Config) { config.Models = nil }},
		{name: "empty model", mutate: func(config *Config) { config.Models = map[model.ID]API{"": ""} }},
		{name: "unknown override", mutate: func(config *Config) { config.Models = map[model.ID]API{"demo": "legacy"} }},
		{name: "unsupported reasoning wire format", mutate: func(config *Config) {
			config.ReasoningWireFormats = map[model.ID]string{"demo": "custom"}
		}},
		{name: "responses reasoning wire format API mismatch", mutate: func(config *Config) {
			config.API = APIChatCompletions
			config.ReasoningWireFormats = map[model.ID]string{"demo": "openai-responses"}
		}},
		{name: "chat reasoning wire format API mismatch", mutate: func(config *Config) {
			config.ReasoningWireFormats = map[model.ID]string{"demo": "openai-chat-effort"}
		}},
		{name: "Ornith reasoning wire format API mismatch", mutate: func(config *Config) {
			config.ReasoningWireFormats = map[model.ID]string{"demo": reasoningWireFormatOllamaOrnith}
		}},
		{name: "no resolver", mutate: func(config *Config) { config.APIKey = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			service, err := New(config)
			assert.Nil(t, service)
			assert.Error(t, err)
		})
	}

	service, err := New(Config{
		ProviderID: "local", BaseURL: "https://example.com/v1", API: APIResponses,
		Models: map[model.ID]API{"demo": ""},
		APIKey: NewMockAPIKeyResolver(gomock.NewController(t)), ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	for _, request := range []run.ModelRequest{richRequest("other", "demo"), richRequest("local", "unknown")} {
		var events []run.StreamEvent
		err = service.Stream(t.Context(), request, func(event run.StreamEvent) error {
			events = append(events, event)
			return nil
		})
		require.Error(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, run.StreamEventError, events[0].Kind)
	}
}

func (s *serviceSuite) TestOffReasoningUsesEachAPIWireShape() {
	t := s.T()
	for _, api := range []API{APIChatCompletions, APIResponses} {
		t.Run(string(api), func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				writer.Header().Set("Content-Type", "text/event-stream")
				if api == APIChatCompletions {
					writeSSE(t, writer, `{"id":"chat","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
				} else {
					writeSSE(t, writer, `{"type":"response.completed","response":{"id":"resp","status":"completed","output":[]}}`)
				}
			}))
			t.Cleanup(server.Close)
			wireFormats := map[model.ID]string{}
			if api == APIResponses {
				wireFormats["demo"] = "openai-responses"
			} else {
				wireFormats["demo"] = "openai-chat-effort"
			}
			service, err := New(Config{
				ProviderID: "local", BaseURL: server.URL, API: api,
				Models: map[model.ID]API{"demo": ""}, ReasoningWireFormats: wireFormats,
				APIKey: expectAPIKey(t, "", nil, 1), ReasoningCompatibilityKeys: nil,
			})
			require.NoError(t, err)
			request := richRequest("local", "demo")
			request.ReasoningChoice = model.ReasoningChoiceOff
			events := streamEvents(t, service, request)
			assert.Equal(t, run.StreamEventDone, events[len(events)-1].Kind)
			if api == APIChatCompletions {
				assert.Equal(t, "none", body["reasoning_effort"])
			} else {
				reasoning := body["reasoning"].(map[string]any)
				assert.Equal(t, "none", reasoning["effort"])
			}
		})
	}
}

// TestResponsesUsesConfiguredReasoningWireFormat verifies only configured reasoning models send reasoning fields.
func (s *serviceSuite) TestResponsesUsesConfiguredReasoningWireFormat() {
	t := s.T()
	for _, testCase := range []struct {
		name       string
		wireFormat string
		reasoning  bool
	}{
		{name: "supported reasoning", wireFormat: "openai-responses", reasoning: true},
		{name: "no reasoning", wireFormat: "", reasoning: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := richRequest("local", "demo")
			body := runResponsesRequest(t, request, "", testCase.wireFormat)
			if testCase.reasoning {
				reasoning, ok := body["reasoning"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "high", reasoning["effort"])
			} else {
				assert.NotContains(t, body, "reasoning")
			}
		})
	}
}

func (s *serviceSuite) TestResponsesFailedEventIsTerminalError() {
	t := s.T()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, `{"type":"response.failed","response":{"id":"resp-failed","status":"failed","output":[]}}`)
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1), ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	terminal := events[len(events)-1]
	assert.Equal(t, run.StreamEventError, terminal.Kind)
	assert.Equal(t, model.OutcomeFailed, terminal.Response.Outcome.OrEmpty())
}

// TestRemoteContextRejectionIsTerminalAndPreservesSelection verifies one replay attempt through the active runtime snapshot.
func (s *serviceSuite) TestRemoteContextRejectionIsTerminalAndPreservesSelection() {
	t := s.T()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(request.Body).Decode(&body)) {
			return
		}
		encoded, err := json.Marshal(body)
		if !assert.NoError(t, err) {
			return
		}
		assert.Contains(t, string(encoded), "rejected-cipher")
		http.Error(writer, "invalid reasoning context", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	driver, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models:                     map[model.ID]API{"demo": ""},
		ReasoningCompatibilityKeys: map[model.ID]mo.Option[string]{"demo": mo.Some("family")},
		APIKey:                     expectAPIKey(t, "", nil, 1), ReasoningWireFormats: nil,
	})
	require.NoError(t, err)
	selection := model.Selection{
		Provider: "local", Model: "demo", ReasoningChoice: model.ReasoningChoiceHigh,
	}
	catalog, err := hostproviders.New([]hostproviders.Entry{{
		Descriptor: model.Descriptor{
			Provider: "local", Model: "demo",
			ReasoningCapabilities: model.ReasoningCapabilities{
				Supported: true,
				Choices:   []model.ReasoningChoice{model.ReasoningChoiceHigh},
				Default:   model.ReasoningChoiceHigh,
			}, ToolCapabilities: model.ToolCapabilities{},
		},
		Provider: driver, SelectionCredentialValidator: nil, Authentication: nil,
	}}, selection)
	require.NoError(t, err)
	runtime := catalog.Current()
	request := richRequest(runtime.Model.Provider, runtime.Model.Model)
	request.Model = runtime.Model
	request.ReasoningChoice = runtime.ReasoningChoice
	appendHistoryModelContent(&request, model.Content{
		Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true,
		ProviderContext: mo.Some(model.ProviderContext{
			Source: model.ProviderContextSource{
				ProviderID: "local", API: "responses", Model: "demo", CompatibilityKey: mo.Some("family"),
			},
			Payload: []byte(`{"id":"reasoning","encrypted_content":"rejected-cipher","summary":[]}`),
		}), ToolCall: mo.None[model.ToolCall](),
	})
	var events []run.StreamEvent

	err = runtime.Provider.Stream(t.Context(), request, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	require.Error(t, err)
	assert.Equal(t, int64(1), calls.Load())
	require.NotEmpty(t, events)
	assert.Equal(t, run.StreamEventError, events[len(events)-1].Kind)
	assert.Equal(t, selection, catalog.Selection())
}

func (s *serviceSuite) TestInterruptedStreamClosesActiveContentBeforeFailure() {
	t := s.T()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, err := writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"partial\"}\n\n"))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1), ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	kinds := eventKinds(events)
	assert.Contains(t, kinds, run.StreamEventContentEnd)
	assert.Equal(t, run.StreamEventError, kinds[len(kinds)-1])
}

func (s *serviceSuite) TestCancellationAndHTTPFailureMapTerminalErrors() {
	t := s.T()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"error":{"message":"provider-secret-detail"}}`))
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 2), ReasoningWireFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var canceled []run.StreamEvent
	err = service.Stream(ctx, richRequest("local", "demo"), func(event run.StreamEvent) error {
		canceled = append(canceled, event)
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, canceled, 1)
	assert.Equal(t, model.OutcomeAborted, canceled[0].Response.Outcome.OrEmpty())
	assert.Zero(t, calls.Load())

	var failed []run.StreamEvent
	err = service.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
		failed = append(failed, event)
		return nil
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "provider-secret-detail")
	require.Len(t, failed, 1)
	assert.Equal(t, model.OutcomeFailed, failed[0].Response.Outcome.OrEmpty())
	assert.NotContains(t, failed[0].Response.ErrorMessage.OrEmpty(), "provider-secret-detail")
	assert.Equal(t, int64(1), calls.Load())
}

// runResponsesRequest captures one compatible Responses request through the driver boundary.
func runResponsesRequest(
	t *testing.T,
	request run.ModelRequest,
	compatibilityKey string,
	wireFormat string,
) map[string]any {
	t.Helper()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		assert.NoError(t, json.NewDecoder(httpRequest.Body).Decode(&body))
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, `{"type":"response.completed","response":{"id":"resp","status":"completed","output":[]}}`)
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models:               map[model.ID]API{request.Model.Model: APIResponses},
		ReasoningWireFormats: map[model.ID]string{request.Model.Model: wireFormat},
		ReasoningCompatibilityKeys: map[model.ID]mo.Option[string]{
			request.Model.Model: mo.EmptyableToOption(compatibilityKey),
		},
		APIKey: expectAPIKey(t, "", nil, 1),
	})
	require.NoError(t, err)
	events := streamEvents(t, service, request)
	require.Equal(t, run.StreamEventDone, events[len(events)-1].Kind)
	return body
}

// replaceHistoryModelContent replaces model content and restores the updated Option value.
func replaceHistoryModelContent(request *run.ModelRequest, content []model.Content) {
	response := request.History[1].Model.OrEmpty()
	response.Content = content
	request.History[1].Model = mo.Some(response)
}

// appendHistoryModelContent appends model content and restores the updated Option value.
func appendHistoryModelContent(request *run.ModelRequest, content ...model.Content) {
	response := request.History[1].Model.OrEmpty()
	response.Content = append(response.Content, content...)
	request.History[1].Model = mo.Some(response)
}

func richRequest(provider model.ProviderID, modelID model.ID) run.ModelRequest {
	return run.ModelRequest{
		Instructions: "be useful", Model: model.Descriptor{Provider: provider, Model: modelID, ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{}},
		ReasoningChoice: model.ReasoningChoiceHigh,
		History: []agent.HistoryEntry{
			{
				Kind: agent.HistoryEntryUser,
				User: mo.Some(model.Message{Content: []model.InputContent{
					{Kind: model.InputContentText, Text: mo.Some("look"), MediaType: mo.None[string](), Data: mo.None[[]byte]()},
					{Kind: model.InputContentImage, MediaType: mo.Some("image/png"), Data: mo.Some([]byte{1, 2, 3}), Text: mo.None[string]()},
				}}), Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			},
			{
				Kind: agent.HistoryEntryModel,
				Model: mo.Some(model.Response{Content: []model.Content{
					{Kind: model.ContentText, Text: mo.Some("checking"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
					{
						Kind: model.ContentToolCall, Final: true,
						ToolCall: mo.Some(model.ToolCall{ID: "call-old", Name: "read", Arguments: map[string]any{"path": "old"}}), Text: mo.None[string](), ProviderContext: mo.None[model.ProviderContext](),
					},
				}, Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
				}), User: mo.None[model.Message](), ToolResult: mo.None[agent.ToolResult](),
			},
			{
				Kind: agent.HistoryEntryToolResult,
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call-old", ToolName: "read", Contents: tool.TextContents("done"), IsError: false,
				}), User: mo.None[model.Message](), Model: mo.None[model.Response](),
			},
		},
		Tools: []tool.Descriptor{{Name: "read", Description: "Read a file", InputSchemaJSON: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`), ConstrainedSampling: mo.None[tool.ConstrainedSampling]()}},
	}
}

func streamEvents(t *testing.T, service *Driver, request run.ModelRequest) []run.StreamEvent {
	t.Helper()
	var events []run.StreamEvent
	err := service.Stream(t.Context(), request, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if len(events) > 0 && events[len(events)-1].Kind == run.StreamEventError {
		assert.Error(t, err)
	} else {
		require.NoError(t, err)
	}
	return events
}

func eventKinds(events []run.StreamEvent) []run.StreamEventKind {
	return lo.Map(events, func(event run.StreamEvent, _ int) run.StreamEventKind {
		return event.Kind
	})
}

func writeSSE(t *testing.T, writer http.ResponseWriter, events ...string) {
	t.Helper()
	for _, event := range events {
		_, err := writer.Write([]byte("data: " + event + "\n\n"))
		require.NoError(t, err)
	}
	_, err := writer.Write([]byte("data: [DONE]\n\n"))
	require.NoError(t, err)
}
