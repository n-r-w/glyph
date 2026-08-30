//go:build integration

package compatible

import (
	"encoding/json/v2"

	"net/http"
	"net/http/httptest"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestNewPreservesBaseURLParseCause verifies URL syntax failures retain parser detail.
func (s *serviceSuite) TestNewPreservesBaseURLParseCause() {
	t := s.T()

	// Arrange a URL that url.Parse rejects before semantic validation.
	config := Config{
		ProviderID: "local", BaseURL: "https://example.com/invalid\x7fpath", API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: NewMockAPIKeyResolver(gomock.NewController(t)),
		ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	}

	// Act by constructing the driver.
	var service *Driver
	var err error
	completed := assert.NotPanics(t, func() {
		service, err = New(config)
	})

	// Assert both adapter context and the url.Parse cause are visible.
	if completed {
		assert.Nil(t, service)
		require.ErrorContains(t, err, "parse OpenAI-compatible base URL")
		assert.ErrorContains(t, err, "invalid control character in URL")
	}
}

func (s *serviceSuite) TestConstructionAndRequestValidation() {
	t := s.T()
	resolver := NewMockAPIKeyResolver(gomock.NewController(t))
	valid := Config{
		ProviderID: "local", BaseURL: "https://example.com/v1", API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: resolver, ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
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
		{name: "Responses format", mutate: func(config *Config) {
			config.ReasoningFormats = map[model.ID]string{"demo": "openrouter"}
		}},
		{name: "missing Chat Completions format", mutate: func(config *Config) {
			config.API = APIChatCompletions
			config.ReasoningFormats = map[model.ID]string{"demo": ""}
		}},
		{name: "unsupported Chat Completions format", mutate: func(config *Config) {
			config.API = APIChatCompletions
			config.ReasoningFormats = map[model.ID]string{"demo": "custom"}
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
		APIKey: NewMockAPIKeyResolver(gomock.NewController(t)), ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
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

// TestOffReasoningUsesEachAPIWireShape verifies each API owns its reasoning control mapping.
func (s *serviceSuite) TestOffReasoningUsesEachAPIWireShape() {
	t := s.T()
	for _, api := range []API{APIChatCompletions, APIResponses} {
		t.Run(string(api), func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				assert.NoError(t, json.UnmarshalRead(request.Body, &body))
				writer.Header().Set("Content-Type", "text/event-stream")
				if api == APIChatCompletions {
					writeSSE(t, writer, `{"id":"chat","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
				} else {
					writeSSE(
						t,
						writer,
						`{"type":"response.completed","response":{"id":"resp","status":"completed","output":[]}}`,
					)
				}
			}))
			t.Cleanup(server.Close)
			formats := map[model.ID]string{"demo": ""}
			if api == APIChatCompletions {
				formats["demo"] = string(reasoningFormatOpenAIChat)
			}
			service, err := New(Config{
				ProviderID: "local", BaseURL: server.URL, API: api,
				Models: map[model.ID]API{"demo": ""}, ReasoningFormats: formats,
				APIKey: expectAPIKey(t, "", nil, 1), ReasoningCompatibilityKeys: nil,
			})
			require.NoError(t, err)
			request := richRequest("local", "demo")
			request.Model.ReasoningCapabilities.Supported = true
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

// TestResponsesUsesModelReasoningCapabilities verifies Responses behavior needs no reasoning format.
func (s *serviceSuite) TestResponsesUsesModelReasoningCapabilities() {
	t := s.T()
	for _, testCase := range []struct {
		name      string
		reasoning bool
	}{
		{name: "supported reasoning", reasoning: true},
		{name: "no reasoning", reasoning: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := richRequest("local", "demo")
			request.Model.ReasoningCapabilities.Supported = testCase.reasoning
			body := runResponsesRequest(t, request, "")
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
