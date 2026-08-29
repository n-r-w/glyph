package compatible

import (
	"encoding/json"

	"net/http"
	"net/http/httptest"

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

// runResponsesRequest captures one compatible Responses request through the driver boundary.
func runResponsesRequest(
	t *testing.T,
	request run.ModelRequest,
	compatibilityKey string,
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
		Models:           map[model.ID]API{request.Model.Model: APIResponses},
		ReasoningFormats: map[model.ID]string{request.Model.Model: ""},
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
		Instructions: "be useful", Model: model.Descriptor{Provider: provider, Model: modelID, Input: nil, ContextWindow: 0, MaxTokens: 0, ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing]()},
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
