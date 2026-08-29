package compatible

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostproviders "github.com/n-r-w/glyph/host/internal/usecase/host/providers"
)

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
		APIKey: expectAPIKey(t, "", nil, 1), ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	assert.Equal(t, run.StreamEventError, events[len(events)-1].Kind)
	assert.Equal(t, model.OutcomeFailed, events[len(events)-1].Response.OrEmpty().Outcome.OrEmpty())
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
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1), ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
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

// TestFinalErrorHandlerFailurePreservesProviderCause verifies final callback failure retains provider cause once.
func (s *serviceSuite) TestFinalErrorHandlerFailurePreservesProviderCause() {
	t := s.T()

	// Arrange one provider API failure with unique detail.
	providerMarker := "unique compatible final provider failure"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusInternalServerError)
		_, err := writer.Write([]byte(`{"error":{"message":"` + providerMarker + `"}}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": APIResponses}, APIKey: expectAPIKey(t, "", nil, 1),
		ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	handlerErr := errors.New("unique compatible final error handler failure")
	callbacks := 0

	// Act by rejecting the one final provider error event.
	err = service.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
		callbacks++
		assert.Equal(t, run.StreamEventError, event.Kind)
		return handlerErr
	})

	// Assert both causes occur once and no second terminal callback is attempted.
	require.ErrorIs(t, err, handlerErr)
	var providerErr *openai.Error
	require.ErrorAs(t, err, &providerErr)
	require.ErrorIs(t, err, providerErr)
	assert.Equal(t, 1, strings.Count(err.Error(), providerMarker))
	assert.Equal(t, 1, strings.Count(err.Error(), handlerErr.Error()))
	assert.Equal(t, 1, callbacks)
}

// TestPartialStreamFailureJoinsContentEndDelivery verifies stream and finalization causes survive once.
func (s *serviceSuite) TestPartialStreamFailureJoinsContentEndDelivery() {
	t := s.T()
	tests := map[string]struct {
		api          API
		partialEvent string
	}{
		"Chat Completions": {
			api:          APIChatCompletions,
			partialEvent: `{"id":"chat-partial","choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		},
		"Responses": {
			api:          APIResponses,
			partialEvent: `{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"partial"}`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Arrange a partial stream followed by malformed SDK input and failed ContentEnd delivery.
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				payload := []byte("data: " + test.partialEvent + "\n\n")
				writer.Header().Set("Content-Type", "text/event-stream")
				writer.Header().Set("Content-Length", strconv.Itoa(len(payload)+100))
				_, err := writer.Write(payload)
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)
			service, err := New(Config{
				ProviderID: "local", BaseURL: server.URL, API: test.api,
				Models: map[model.ID]API{"demo": test.api}, APIKey: expectAPIKey(t, "", nil, 1),
				ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
			})
			require.NoError(t, err)
			handlerErr := errors.New("unique ContentEnd delivery failure")
			events := make([]run.StreamEventKind, 0)

			// Act by streaming until partial-content finalization reaches the failed handler.
			err = service.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
				events = append(events, event.Kind)
				if event.Kind == run.StreamEventContentEnd {
					return handlerErr
				}
				return nil
			})

			// Assert both exact causes occur once and no terminal callback follows handler failure.
			require.Error(t, err)
			require.ErrorIs(t, err, handlerErr)
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			assert.Equal(t, 1, strings.Count(err.Error(), handlerErr.Error()))
			assert.Equal(t, 1, strings.Count(err.Error(), io.ErrUnexpectedEOF.Error()))
			require.NotEmpty(t, events)
			assert.Equal(t, run.StreamEventContentEnd, events[len(events)-1])
			assert.NotContains(t, events, run.StreamEventDone)
			assert.NotContains(t, events, run.StreamEventError)
		})
	}
}

// TestResolverFailurePreservesCause verifies credential diagnostics reach both error boundaries.
func (s *serviceSuite) TestResolverFailurePreservesCause() {
	t := s.T()

	// Arrange a resolver failure before any HTTP request starts.
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""},
		APIKey: expectAPIKey(t, "", errors.New("credential store checksum mismatch"), 1), ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	var events []run.StreamEvent

	// Act by starting one provider request.
	err = service.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	// Assert the resolver cause and adapter context are returned and delivered.
	require.ErrorContains(t, err, "resolve OpenAI-compatible API key: credential store checksum mismatch")
	assert.Zero(t, calls.Load())
	require.Len(t, events, 1)
	terminal := events[0]
	assert.Equal(t, run.StreamEventError, terminal.Kind)
	assert.Contains(t, terminal.Response.OrEmpty().ErrorMessage.OrEmpty(), "resolve OpenAI-compatible API key: credential store checksum mismatch")
}

// TestResponsesFailedEventPreservesProviderMessage verifies a failed event keeps provider diagnostics.
func (s *serviceSuite) TestResponsesFailedEventPreservesProviderMessage() {
	t := s.T()

	// Arrange a terminal provider failure with a unique message.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, `{"type":"response.failed","response":{"id":"resp-failed","status":"failed","error":{"code":"server_error","message":"provider capacity shard unavailable"},"output":[]}}`)
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1), ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	var events []run.StreamEvent

	// Act by consuming the failed event.
	err = service.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	// Assert driver context, Responses context, and provider detail reach both error boundaries.
	for _, detail := range []string{
		"OpenAI-compatible request failed",
		"responses request failed",
		"provider capacity shard unavailable",
	} {
		require.ErrorContains(t, err, detail)
	}
	require.Len(t, events, 1)
	terminal := events[0]
	assert.Equal(t, run.StreamEventError, terminal.Kind)
	assert.Equal(t, model.OutcomeFailed, terminal.Response.OrEmpty().Outcome.OrEmpty())
	for _, detail := range []string{
		"OpenAI-compatible request failed",
		"responses request failed",
		"provider capacity shard unavailable",
	} {
		assert.Contains(t, terminal.Response.OrEmpty().ErrorMessage.OrEmpty(), detail)
	}
}

// TestMalformedToolArgumentsPreserveParserCause verifies each adapter path keeps JSON parser detail.
func (s *serviceSuite) TestMalformedToolArgumentsPreserveParserCause() {
	t := s.T()
	tests := []struct {
		name   string
		api    API
		events []string
		want   string
	}{
		{
			name: "Chat Completions stream", api: APIChatCompletions,
			events: []string{
				`{"id":"chat-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"read","arguments":"}"}}]},"finish_reason":"tool_calls"}]}`,
			},
			want: "decode chat Completions tool-call arguments: jsontext: invalid character",
		},
		{
			name: "Responses stream", api: APIResponses,
			events: []string{
				`{"type":"response.output_item.added","output_index":0,"item":{"id":"item-1","type":"function_call","call_id":"call-1","name":"read","arguments":"","status":"in_progress"}}`,
				`{"type":"response.function_call_arguments.done","output_index":0,"item_id":"item-1","name":"read","arguments":"}"}`,
			},
			want: "decode Responses tool-call arguments: jsontext: invalid character",
		},
		{
			name: "Responses completed output", api: APIResponses,
			events: []string{
				`{"type":"response.completed","response":{"id":"resp-1","model":"actual","status":"completed","output":[{"id":"item-1","type":"function_call","call_id":"call-1","name":"read","arguments":"}","status":"completed"}]}}`,
			},
			want: "decode Responses tool-call arguments: jsontext: invalid character",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange malformed arguments at one adapter decoding boundary.
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				writeSSE(t, writer, test.events...)
			}))
			t.Cleanup(server.Close)
			service, err := New(Config{
				ProviderID: "local", BaseURL: server.URL, API: test.api,
				Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1),
				ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
			})
			require.NoError(t, err)
			var events []run.StreamEvent

			// Act by streaming the malformed provider payload.
			err = service.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
				events = append(events, event)
				return nil
			})

			// Assert parser detail and adapter context reach both error boundaries.
			require.ErrorContains(t, err, test.want)
			require.NotEmpty(t, events)
			terminal := events[len(events)-1]
			assert.Equal(t, run.StreamEventError, terminal.Kind)
			assert.Contains(t, terminal.Response.OrEmpty().ErrorMessage.OrEmpty(), test.want)
		})
	}
}

// TestMalformedProviderContextPreservesParserCause verifies replay decoding keeps JSON parser detail.
func (s *serviceSuite) TestMalformedProviderContextPreservesParserCause() {
	t := s.T()

	// Arrange compatible provider context with malformed JSON.
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1),
		ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	request := richRequest("local", "demo")
	appendHistoryModelContent(&request, model.Content{
		Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true,
		ProviderContext: mo.Some(model.ProviderContext{
			Source: model.ProviderContextSource{
				ProviderID: "local", API: "responses", Model: "demo", CompatibilityKey: mo.None[string](),
			},
			Payload: []byte(`{"id":}`),
		}), ToolCall: mo.None[model.ToolCall](),
	})
	var events []run.StreamEvent

	// Act by streaming a request that must decode the provider context.
	err = service.Stream(t.Context(), request, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	// Assert parser detail and adapter context are returned and delivered before HTTP dispatch.
	require.ErrorContains(t, err, "decode OpenAI-compatible provider context: jsontext: invalid character")
	assert.Zero(t, calls.Load())
	require.Len(t, events, 1)
	assert.Equal(t, run.StreamEventError, events[0].Kind)
	assert.Contains(t, events[0].Response.OrEmpty().ErrorMessage.OrEmpty(), "decode OpenAI-compatible provider context: jsontext: invalid character")
}

// TestRemoteContextRejectionIsTerminalAndPreservesSelection verifies one replay attempt through the active runtime snapshot.
func (s *serviceSuite) TestRemoteContextRejectionIsTerminalAndPreservesSelection() {
	t := s.T()

	// Arrange a selected runtime whose endpoint rejects replayed reasoning context.
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		var body map[string]any
		if !assert.NoError(t, json.UnmarshalRead(request.Body, &body)) {
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
		APIKey:                     expectAPIKey(t, "", nil, 1), ReasoningFormats: nil,
	})
	require.NoError(t, err)
	selection := model.Selection{
		Provider: "local", Model: "demo", ReasoningChoice: model.ReasoningChoiceHigh,
	}
	catalog, err := hostproviders.New([]hostproviders.Entry{{
		Descriptor: model.Descriptor{
			Provider: "local", Model: "demo",
			Input: []model.InputModality{model.InputModalityText}, ContextWindow: 131072, MaxTokens: 16384,
			ReasoningCapabilities: model.ReasoningCapabilities{
				Supported: true,
				Choices:   []model.ReasoningChoice{model.ReasoningChoiceHigh},
				Default:   model.ReasoningChoiceHigh,
			}, ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
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

	// Act by streaming the request with rejected opaque context.
	err = runtime.Provider.Stream(t.Context(), request, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})

	// Assert rejection is terminal and does not mutate catalog selection.
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
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1), ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	events := streamEvents(t, service, richRequest("local", "demo"))
	kinds := eventKinds(events)
	assert.Contains(t, kinds, run.StreamEventContentEnd)
	assert.Equal(t, run.StreamEventError, kinds[len(kinds)-1])
}

// TestCancellationAndHTTPFailureMapTerminalErrors verifies cancellation remains canonical and HTTP detail remains visible.
func (s *serviceSuite) TestCancellationAndHTTPFailureMapTerminalErrors() {
	t := s.T()

	// Arrange one endpoint for a canceled request and one provider HTTP failure.
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"error":{"message":"provider-secret-detail"}}`))
	}))
	t.Cleanup(server.Close)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 2), ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)

	// Act by canceling before dispatch.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var canceled []run.StreamEvent
	err = service.Stream(ctx, richRequest("local", "demo"), func(event run.StreamEvent) error {
		canceled = append(canceled, event)
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, canceled, 1)
	assert.Equal(t, model.OutcomeAborted, canceled[0].Response.OrEmpty().Outcome.OrEmpty())
	assert.Zero(t, calls.Load())

	// Act by dispatching a request that receives the HTTP failure.
	var failed []run.StreamEvent
	err = service.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
		failed = append(failed, event)
		return nil
	})
	// Assert the HTTP cause reaches both error boundaries without changing cancellation classification.
	require.ErrorContains(t, err, "provider-secret-detail")
	require.Len(t, failed, 1)
	assert.Equal(t, model.OutcomeFailed, failed[0].Response.OrEmpty().Outcome.OrEmpty())
	assert.Contains(t, failed[0].Response.OrEmpty().ErrorMessage.OrEmpty(), "provider-secret-detail")
	assert.Equal(t, int64(1), calls.Load())
}
