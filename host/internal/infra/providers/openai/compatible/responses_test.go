package compatible

import (
	"encoding/json"

	"net/http"
	"net/http/httptest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

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
		APIKey: expectAPIKey(t, "", nil, 1), ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	terminal := events[len(events)-1]
	assert.Contains(t, terminal.Response.OrEmpty().Content, model.Content{
		Kind: model.ContentReasoning, Text: mo.Some("visible reason"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
	})
	require.Len(t, terminal.Response.OrEmpty().Content, 1)
	assert.Empty(t, terminal.Response.OrEmpty().Content[0].ProviderContext.OrEmpty().Payload)
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
		Models:           map[model.ID]API{"demo": APIResponses},
		ReasoningFormats: map[model.ID]string{"demo": ""},
		APIKey:           expectAPIKey(t, "", nil, 1), ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)

	request := richRequest("local", "demo")
	request.Model.ReasoningCapabilities.Supported = true
	appendHistoryModelContent(&request,
		model.Content{Kind: model.ContentReasoning, Text: mo.Some(""), Final: true, ProviderContext: mo.Some(model.ProviderContext{Source: model.ProviderContextSource{ProviderID: "local", API: "responses", Model: "demo", CompatibilityKey: mo.None[string]()}, Payload: []byte(`{"id":"r-local","encrypted_content":"cipher","summary":["old"]}`)}), ToolCall: mo.None[model.ToolCall]()},
		model.Content{Kind: model.ContentReasoning, Text: mo.Some(""), Final: true, ProviderContext: mo.Some(model.ProviderContext{Source: model.ProviderContextSource{ProviderID: "foreign", API: "responses", Model: "demo", CompatibilityKey: mo.None[string]()}, Payload: []byte(`{"id":"r-foreign","encrypted_content":"secret","summary":[]}`)}), ToolCall: mo.None[model.ToolCall]()},
	)
	events := streamEvents(t, service, request)
	terminal := events[len(events)-1]
	assert.Equal(t, run.StreamEventDone, terminal.Kind)
	assert.Equal(t, model.OutcomeStop, terminal.Response.OrEmpty().Outcome.OrEmpty())
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

	body := runResponsesRequest(t, request, "shared-family")
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

	body := runResponsesRequest(t, request, "new-family")
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

			body := runResponsesRequest(t, request, "target-family")
			encoded, err := json.Marshal(body["input"])
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "r-incompatible")
			assert.NotContains(t, string(encoded), "private-cipher")
			assert.Contains(t, string(encoded), `"role":"assistant"`)
			assert.Contains(t, string(encoded), "visible reasoning")
		})
	}
}

// TestResponsesStreamsRefusalAndFragmentedToolCall verifies Responses clamps overlapping usage into disjoint buckets.
func (s *serviceSuite) TestResponsesStreamsRefusalAndFragmentedToolCall() {
	t := s.T()

	// Arrange a Responses stream with cache buckets above input and reasoning above output.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer,
			`{"type":"response.refusal.delta","output_index":0,"content_index":0,"delta":"blocked"}`,
			`{"type":"response.output_item.added","output_index":8,"item":{"id":"item-1","type":"function_call","call_id":"call-1","name":"read","arguments":"","status":"in_progress"}}`,
			`{"type":"response.function_call_arguments.delta","output_index":8,"item_id":"item-1","delta":"{\"path\":\"fi"}`,
			`{"type":"response.function_call_arguments.delta","output_index":8,"item_id":"item-1","delta":"le\"}"}`,
			`{"type":"response.function_call_arguments.done","output_index":8,"item_id":"item-1","name":"read","arguments":"{\"path\":\"file\"}"}`,
			`{"type":"response.completed","response":{"id":"resp-2","model":"actual","status":"completed","usage":{"input_tokens":2,"output_tokens":2,"total_tokens":99,"input_tokens_details":{"cached_tokens":4,"cache_write_tokens":1},"output_tokens_details":{"reasoning_tokens":3}},"output":[{"id":"m-1","type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"blocked"}]},{"id":"item-1","type":"function_call","call_id":"call-1","name":"read","arguments":"{\"path\":\"file\"}","status":"completed"}]}}`,
		)
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1), ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	// Act by collecting the terminal adapter response.
	events := streamEvents(t, service, richRequest("local", "demo"))

	// Assert lifecycle content and normalized usage both leave the adapter intact.
	assert.Contains(t, eventKinds(events), run.StreamEventToolCallStart)
	assert.Contains(t, eventKinds(events), run.StreamEventToolCallDelta)
	assert.Contains(t, eventKinds(events), run.StreamEventToolCallEnd)
	terminal := events[len(events)-1]
	assert.Equal(t, model.OutcomeToolUse, terminal.Response.OrEmpty().Outcome.OrEmpty())
	assert.Equal(t, model.Usage{InputTokens: 0, OutputTokens: 2, CachedInputTokens: 4, CacheWriteTokens: 1, ReasoningTokens: 2, TotalTokens: 7}, terminal.Response.OrEmpty().Usage.OrEmpty())
	assert.Contains(t, terminal.Response.OrEmpty().Content, model.Content{Kind: model.ContentRefusal, Text: mo.Some("blocked"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()})
}
