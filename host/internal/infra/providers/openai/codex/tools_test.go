//go:build integration

package codex

import (
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

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

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
				`{"type":"response.output_item.added","output_index":0,`+
					`"item":{"id":"fc-1","type":"function_call","call_id":"call-1",`+
					`"name":"read","arguments":"","status":"in_progress"}}`,
				`{"type":"response.function_call_arguments.delta",`+
					`"output_index":0,"item_id":"fc-1",`+
					`"delta":"{\"path\":\"file.txt\",\"query\":\"hel"}`,
				`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc-1","delta":"lo\"}"}`,
				`{"type":"response.function_call_arguments.done","output_index":0,`+
					`"item_id":"fc-1","name":"read",`+
					`"arguments":"{\"path\":\"file.txt\",\"query\":\"hello\"}"}`,
				`{"type":"response.output_item.done","output_index":0,`+
					`"item":{"id":"fc-1","type":"function_call","call_id":"call-1",`+
					`"name":"read","arguments":"{\"path\":\"file.txt\",`+
					`\"query\":\"hello\"}","status":"completed"}}`,
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
	require.Equal(t, "read", events[0].Preview.OrEmpty().Name)
	require.True(t, events[0].Preview.OrEmpty().Provisional)
	require.Equal(t, mo.Some("hel"), events[1].Preview.OrEmpty().Fields[1].Prefix)
	require.Equal(
		t,
		map[string]any{"path": "file.txt", "query": "hello"},
		events[3].ToolCall.OrEmpty().Arguments,
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
				`{"type":"response.output_item.done","output_index":0,`+
					`"item":{"id":"fc-1","type":"function_call","call_id":"call-1",`+
					`"name":"read","arguments":"{\"path\":\"file.txt\"}",`+
					`"status":"completed"}}`,
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
	assert.Equal(t, "call-1", events[0].Preview.OrEmpty().CallID)
	assert.Equal(t, "read", events[0].Preview.OrEmpty().Name)
	assert.Equal(t, model.ToolCall{
		ID:        "call-1",
		Name:      "read",
		Arguments: map[string]any{"path": "file.txt"},
	}, events[1].ToolCall.OrEmpty())
}

// TestDriverStreamRejectsInvalidFinalFunctionArguments verifies malformed final arguments terminate without a tool-call
// end.
func TestDriverStreamRejectsInvalidFinalFunctionArguments(t *testing.T) {
	t.Parallel()

	// Arrange an authenticated stream with malformed final function arguments.
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
				`{"type":"response.output_item.added","output_index":0,`+
					`"item":{"id":"fc-1","type":"function_call","call_id":"call-1",`+
					`"name":"read","arguments":"","status":"in_progress"}}`,
				`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc-1","delta":"{\"path\":"}`,
				`{"type":"response.function_call_arguments.done","output_index":0,`+
					`"item_id":"fc-1","name":"read","arguments":"{\"path\":"}`,
			)
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))

	events := make([]run.StreamEvent, 0)

	// Act by consuming the malformed provider stream.
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

	// Assert the request exposes the JSON cause without publishing a completed tool call.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected EOF")
	terminal := events[len(events)-1].Response.OrEmpty()
	require.Equal(t, model.OutcomeFailed, terminal.Outcome.OrEmpty())
	assert.Contains(t, terminal.ErrorMessage.OrEmpty(), "unexpected EOF")
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
				`{"type":"response.output_item.done","output_index":0,`+
					`"item":{"id":"r-final","type":"reasoning",`+
					`"encrypted_content":"enc-final","summary":[]}}`,
				`{"type":"response.output_item.done","output_index":1,`+
					`"item":{"id":"m-final","type":"message","role":"assistant",`+
					`"status":"completed","content":[{"type":"output_text",`+
					`"text":"final answer","annotations":[],"logprobs":[]}]}}`,
				`{"type":"response.output_item.done","output_index":2,`+
					`"item":{"id":"fc-final","type":"function_call",`+
					`"call_id":"call-final","name":"read",`+
					`"arguments":"{\"path\":\"file.txt\"}","status":"completed"}}`,
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
					Model:      mo.None[model.Response](),
					ToolResult: mo.None[agent.ToolResult](),
					Kind:       agent.HistoryEntryUser,
					User:       mo.Some(model.TextMessage("request")),
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
		textDeltaStreamEvent(model.ContentText, "final "),
		textDeltaStreamEvent(model.ContentText, "answer"),
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
