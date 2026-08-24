//nolint:exhaustruct // Tests set only fields relevant to each provider boundary.
package compatible

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

type serviceSuite struct{ suite.Suite }

func expectAPIKey(t *testing.T, key string, resolveErr error, calls int) *MockAPIKeyResolver {
	t.Helper()
	resolver := NewMockAPIKeyResolver(gomock.NewController(t))
	resolver.EXPECT().ResolveAPIKey(gomock.Any()).Return(key, resolveErr).Times(calls)
	return resolver
}

func TestServiceSuite(t *testing.T) {
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
			`{"id":"chat-1","model":"actual-model","choices":[{"index":0,"delta":{"content":"hello "}}]}`,
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
		Models: models, APIKey: expectAPIKey(t, "secret", nil, 1),
	})
	require.NoError(t, err)
	models["demo"] = APIResponses

	events := streamEvents(t, service, richRequest("openrouter", "demo"))
	require.NotEmpty(t, events)
	terminal := events[len(events)-1]
	assert.Equal(t, run.StreamEventDone, terminal.Kind)
	assert.Equal(t, model.OutcomeToolUse, terminal.Response.Outcome)
	assert.Equal(t, model.Usage{InputTokens: 12, OutputTokens: 7, CachedInputTokens: 3, ReasoningTokens: 2, TotalTokens: 19}, terminal.Response.Usage)
	assert.Equal(t, "actual-model", string(*terminal.Response.ResponseModel))
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
	assert.Contains(t, terminal.Response.Content, model.Content{Kind: model.ContentRefusal, Text: "no", Final: true})
	assert.Contains(t, terminal.Response.Content, model.Content{Kind: model.ContentToolCall, Final: true, ToolCall: model.ToolCall{ID: "call-new", Name: "read", Arguments: map[string]any{"path": "file"}}})
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
		Models: map[model.ID]API{"demo": ""}, APIKey: resolver,
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
		APIKey: expectAPIKey(t, "", nil, 1),
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	assert.Equal(t, run.StreamEventError, events[len(events)-1].Kind)
	assert.Equal(t, model.OutcomeFailed, events[len(events)-1].Response.Outcome)
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
		APIKey: expectAPIKey(t, "", nil, 1),
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	terminal := events[len(events)-1]
	assert.Contains(t, terminal.Response.Content, model.Content{
		Kind: model.ContentReasoning, Text: "visible reason", Final: true,
	})
	for _, content := range terminal.Response.Content {
		assert.NotEqual(t, model.ContentProviderContext, content.Kind)
	}
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
		Models: map[model.ID]API{"demo": APIResponses}, APIKey: expectAPIKey(t, "", nil, 1),
	})
	require.NoError(t, err)

	request := richRequest("local", "demo")
	request.History[1].Model.Content = append(request.History[1].Model.Content,
		model.Content{Kind: model.ContentProviderContext, Final: true, ProviderContext: model.ProviderContext{ProviderID: "local", Payload: []byte(`{"id":"r-local","encrypted_content":"cipher","summary":["old"]}`)}},
		model.Content{Kind: model.ContentProviderContext, Final: true, ProviderContext: model.ProviderContext{ProviderID: "foreign", Payload: []byte(`{"id":"r-foreign","encrypted_content":"secret","summary":[]}`)}},
	)
	events := streamEvents(t, service, request)
	terminal := events[len(events)-1]
	assert.Equal(t, run.StreamEventDone, terminal.Kind)
	assert.Equal(t, model.OutcomeStop, terminal.Response.Outcome)
	input := body["input"].([]any)
	encoded, err := json.Marshal(input)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "r-local")
	assert.NotContains(t, string(encoded), "r-foreign")
	assert.Equal(t, "high", body["reasoning"].(map[string]any)["effort"])
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
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1),
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	assert.Contains(t, eventKinds(events), run.StreamEventToolCallStart)
	assert.Contains(t, eventKinds(events), run.StreamEventToolCallDelta)
	assert.Contains(t, eventKinds(events), run.StreamEventToolCallEnd)
	terminal := events[len(events)-1]
	assert.Equal(t, model.OutcomeToolUse, terminal.Response.Outcome)
	assert.Contains(t, terminal.Response.Content, model.Content{Kind: model.ContentRefusal, Text: "blocked", Final: true})
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
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1),
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
		APIKey: expectAPIKey(t, "top-secret", errors.New("credential source unavailable: top-secret"), 1),
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
	assert.NotContains(t, terminal.Response.ErrorMessage, "top-secret")
	assert.NotContains(t, terminal.Response.ErrorMessage, "credential source unavailable")
}

func (s *serviceSuite) TestConstructionAndRequestValidation() {
	t := s.T()
	resolver := NewMockAPIKeyResolver(gomock.NewController(t))
	valid := Config{
		ProviderID: "local", BaseURL: "https://example.com/v1", API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: resolver,
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
		APIKey: NewMockAPIKeyResolver(gomock.NewController(t)),
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

func (s *serviceSuite) TestNoneReasoningIsOmittedForBothAPIs() {
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
			service, err := New(Config{
				ProviderID: "local", BaseURL: server.URL, API: api,
				Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1),
			})
			require.NoError(t, err)
			request := richRequest("local", "demo")
			request.ReasoningLevel = model.ReasoningLevelNone
			events := streamEvents(t, service, request)
			assert.Equal(t, run.StreamEventDone, events[len(events)-1].Kind)
			if api == APIChatCompletions {
				assert.NotContains(t, body, "reasoning_effort")
			} else {
				reasoning, exists := body["reasoning"]
				if exists {
					assert.NotContains(t, reasoning, "effort")
				}
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
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1),
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	terminal := events[len(events)-1]
	assert.Equal(t, run.StreamEventError, terminal.Kind)
	assert.Equal(t, model.OutcomeFailed, terminal.Response.Outcome)
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
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1),
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
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 2),
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
	assert.Equal(t, model.OutcomeAborted, canceled[0].Response.Outcome)
	assert.Zero(t, calls.Load())

	var failed []run.StreamEvent
	err = service.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
		failed = append(failed, event)
		return nil
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "provider-secret-detail")
	require.Len(t, failed, 1)
	assert.Equal(t, model.OutcomeFailed, failed[0].Response.Outcome)
	assert.NotContains(t, failed[0].Response.ErrorMessage, "provider-secret-detail")
	assert.Equal(t, int64(1), calls.Load())
}

func richRequest(provider model.ProviderID, modelID model.ID) run.ModelRequest {
	return run.ModelRequest{
		Instructions: "be useful", Model: model.Descriptor{Provider: provider, Model: modelID},
		ReasoningLevel: model.ReasoningLevelHigh,
		History: []agent.HistoryEntry{
			{
				Kind: agent.HistoryEntryUser,
				User: model.Message{Content: []model.InputContent{
					{Kind: model.InputContentText, Text: "look"},
					{Kind: model.InputContentImage, MediaType: "image/png", Data: []byte{1, 2, 3}},
				}},
			},
			{
				Kind: agent.HistoryEntryModel,
				Model: model.Response{Content: []model.Content{
					{Kind: model.ContentText, Text: "checking", Final: true},
					{
						Kind: model.ContentToolCall, Final: true,
						ToolCall: model.ToolCall{ID: "call-old", Name: "read", Arguments: map[string]any{"path": "old"}},
					},
				}},
			},
			{
				Kind: agent.HistoryEntryToolResult,
				ToolResult: agent.ToolResult{
					CallID: "call-old", ToolName: "read", Contents: tool.TextContents("done"),
				},
			},
		},
		Tools: []tool.Descriptor{{Name: "read", Description: "Read a file", InputSchemaJSON: []byte(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}},
	}
}

func streamEvents(t *testing.T, service *Service, request run.ModelRequest) []run.StreamEvent {
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
	kinds := make([]run.StreamEventKind, len(events))
	for index := range events {
		kinds[index] = events[index].Kind
	}
	return kinds
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
