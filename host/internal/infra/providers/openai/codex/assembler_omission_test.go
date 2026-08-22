package codex

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestServiceStreamRecoversFunctionCallFromTerminalOutput verifies terminal output closes an active call.
func TestServiceStreamRecoversFunctionCallFromTerminalOutput(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"path\":\"partial"}`,
		completedEvent(`[{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"{\"path\":\"file.txt\"}","status":"completed"}]`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallDelta,
		run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, map[string]any{"path": "file.txt"}, events[2].ToolCall.Arguments)
}

// TestServiceStreamRecoversMissingFunctionLifecycleFromTerminalOutput verifies terminal identity creates both events.
func TestServiceStreamRecoversMissingFunctionLifecycleFromTerminalOutput(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		completedEvent(`[{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"{\"path\":\"file.txt\"}","status":"completed"}]`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, "call-1", events[0].Preview.CallID)
	assert.Equal(t, map[string]any{"path": "file.txt"}, events[1].ToolCall.Arguments)
}

// TestServiceStreamAcceptsFunctionDoneWithoutIdentity verifies output index closes the active call.
func TestServiceStreamAcceptsFunctionDoneWithoutIdentity(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"path\":\"file.txt\"}"}`,
		`{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"path\":\"file.txt\"}"}`,
		completedEvent(`[{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"{\"path\":\"file.txt\"}","status":"completed"}]`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallDelta,
		run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, map[string]any{"path": "file.txt"}, events[2].ToolCall.Arguments)
}

// TestServiceStreamAcceptsSemanticallyEquivalentFinalizedFunctionArguments verifies decoded values define equality.
func TestServiceStreamAcceptsSemanticallyEquivalentFinalizedFunctionArguments(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc-1","name":"read","arguments":"{ \"path\": \"file.txt\", \"depth\": 1 }"}`,
		completedEvent(`[{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"{\"depth\":1.0,\"path\":\"file.txt\"}","status":"completed"}]`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, model.OutcomeToolUse, events[len(events)-1].Response.Outcome)
}

// TestServiceStreamRejectsConflictingFinalizedFunctionArguments verifies terminal output cannot replace finalized values.
func TestServiceStreamRejectsConflictingFinalizedFunctionArguments(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc-1","name":"read","arguments":"{\"path\":\"approved.txt\"}"}`,
		completedEvent(`[{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"{\"path\":\"replaced.txt\"}","status":"completed"}]`),
	}, nil)

	require.Error(t, err)
	require.NotEmpty(t, events)
	assert.Equal(t, model.OutcomeFailed, events[len(events)-1].Response.Outcome)
	assert.Equal(t, requestFailedMessage, events[len(events)-1].Response.ErrorMessage)
	assert.NotContains(t, streamEventKinds(events), run.StreamEventDone)
}

// TestServiceStreamRejectsConflictingFinalizedCustomInput verifies terminal output cannot replace finalized input.
func TestServiceStreamRejectsConflictingFinalizedCustomInput(t *testing.T) {
	t.Parallel()

	descriptor := constrainedDescriptor(0, tool.GrammarVariants{Lark: "", Regex: "[a-z]+"})
	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"ctc-1","type":"custom_tool_call","call_id":"call-1","name":"sample","input":"","status":"in_progress"}}`,
		`{"type":"response.custom_tool_call_input.done","output_index":0,"item_id":"ctc-1","input":"approved"}`,
		completedEvent(`[{"id":"ctc-1","type":"custom_tool_call","call_id":"call-1","name":"sample","input":"replaced","status":"completed"}]`),
	}, []tool.Descriptor{descriptor})

	require.Error(t, err)
	require.NotEmpty(t, events)
	assert.Equal(t, model.OutcomeFailed, events[len(events)-1].Response.Outcome)
	assert.Equal(t, requestFailedMessage, events[len(events)-1].Response.ErrorMessage)
	assert.NotContains(t, streamEventKinds(events), run.StreamEventDone)
}

// TestServiceStreamRejectsConflictingFunctionDeltaIdentity verifies output index cannot mask a provider conflict.
func TestServiceStreamRejectsConflictingFunctionDeltaIdentity(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc-other","delta":"{}"}`,
	}, nil)

	require.Error(t, err)
	require.NotEmpty(t, events)
	assert.Equal(t, model.OutcomeFailed, events[len(events)-1].Response.Outcome)
	assert.NotContains(t, streamEventKinds(events), run.StreamEventToolCallEnd)
}

// TestServiceStreamRejectsInvalidTerminalFunctionArguments verifies recovered calls keep strict JSON decoding.
func TestServiceStreamRejectsInvalidTerminalFunctionArguments(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		completedEvent(`[{"id":"fc-1","type":"function_call","call_id":"call-1","name":"read","arguments":"{\"path\":","status":"completed"}]`),
	}, nil)

	require.Error(t, err)
	assert.NotContains(t, streamEventKinds(events), run.StreamEventToolCallEnd)
}

// TestServiceStreamRecoversCustomCallWithoutAddedEvent verifies authoritative custom input is preserved exactly.
func TestServiceStreamRecoversCustomCallWithoutAddedEvent(t *testing.T) {
	t.Parallel()

	descriptor := constrainedDescriptor(0, tool.GrammarVariants{Lark: "", Regex: "[a-z]+"})
	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"ab"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"id":"ctc-1","type":"custom_tool_call","call_id":"call-1","name":"sample","input":"abc","status":"completed"}}`,
		completedEvent(`[]`),
	}, []tool.Descriptor{descriptor})

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, map[string]any{"payload": "abc"}, events[1].ToolCall.Arguments)
}

// TestServiceStreamRecoversCustomCallFromTerminalOutput verifies terminal custom input closes an active call once.
func TestServiceStreamRecoversCustomCallFromTerminalOutput(t *testing.T) {
	t.Parallel()

	descriptor := constrainedDescriptor(0, tool.GrammarVariants{Lark: "", Regex: "[a-z]+"})
	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"id":"ctc-1","type":"custom_tool_call","call_id":"call-1","name":"sample","input":"","status":"in_progress"}}`,
		`{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"ab"}`,
		completedEvent(`[{"id":"ctc-1","type":"custom_tool_call","call_id":"call-1","name":"sample","input":"abc","status":"completed"}]`),
	}, []tool.Descriptor{descriptor})

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallDelta,
		run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, "ab", events[1].Preview.Fields[0].Prefix)
	assert.Equal(t, map[string]any{"payload": "abc"}, events[2].ToolCall.Arguments)
}

func streamOmittedToolEvents(
	t *testing.T,
	fixtures []string,
	tools []tool.Descriptor,
) ([]run.StreamEvent, error) {
	t.Helper()
	accountID := "account-omitted-tool-events"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(
		testCredentialPayload(t, accessToken, "refresh", accountID, time.Now().Add(time.Hour)), true, nil,
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writeSSE(writer, fixtures...)
	}))
	t.Cleanup(server.Close)
	service := newService(testConfig("gpt-test", "high"), credentials, interaction, testProviderOptions(server))
	events := make([]run.StreamEvent, 0)
	err := service.Stream(t.Context(), run.ModelRequest{
		Instructions: "test", Model: model.Descriptor{
			Provider: ProviderID, Model: "gpt-test", ToolCapabilities: model.ToolCapabilities{
				StrictJSONSchema: false, Grammar: model.GrammarCapabilities{Lark: false, Regex: false},
			},
		},
		History: nil, Tools: tools,
	}, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	return events, err
}
