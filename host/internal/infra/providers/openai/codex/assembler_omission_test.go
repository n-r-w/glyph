//go:build integration

package codex

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestDriverStreamRecoversFunctionCallFromTerminalOutput verifies terminal output closes an active call.
func TestDriverStreamRecoversFunctionCallFromTerminalOutput(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,` +
			`"item":{"id":"fc-1","type":"function_call","call_id":"call-1",` +
			`"name":"read","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"path\":\"partial"}`,
		completedEvent(
			`[{"id":"fc-1","type":"function_call","call_id":"call-1",` +
				`"name":"read","arguments":"{\"path\":\"file.txt\"}",` +
				`"status":"completed"}]`,
		),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallDelta,
		run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, map[string]any{"path": "file.txt"}, events[2].ToolCall.OrEmpty().Arguments)
}

// TestDriverStreamRecoversMissingFunctionLifecycleFromTerminalOutput verifies terminal identity creates both events.
func TestDriverStreamRecoversMissingFunctionLifecycleFromTerminalOutput(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		completedEvent(
			`[{"id":"fc-1","type":"function_call","call_id":"call-1",` +
				`"name":"read","arguments":"{\"path\":\"file.txt\"}",` +
				`"status":"completed"}]`,
		),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, "call-1", events[0].Preview.OrEmpty().CallID)
	assert.Equal(t, map[string]any{"path": "file.txt"}, events[1].ToolCall.OrEmpty().Arguments)
}

// TestDriverStreamAcceptsFunctionDoneWithoutIdentity verifies output index closes the active call.
func TestDriverStreamAcceptsFunctionDoneWithoutIdentity(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,` +
			`"item":{"id":"fc-1","type":"function_call","call_id":"call-1",` +
			`"name":"read","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"path\":\"file.txt\"}"}`,
		`{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"path\":\"file.txt\"}"}`,
		completedEvent(
			`[{"id":"fc-1","type":"function_call","call_id":"call-1",` +
				`"name":"read","arguments":"{\"path\":\"file.txt\"}",` +
				`"status":"completed"}]`,
		),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallDelta,
		run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, map[string]any{"path": "file.txt"}, events[2].ToolCall.OrEmpty().Arguments)
}

// TestDriverStreamAcceptsSemanticallyEquivalentFinalizedFunctionArguments verifies decoded values define equality.
func TestDriverStreamAcceptsSemanticallyEquivalentFinalizedFunctionArguments(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,` +
			`"item":{"id":"fc-1","type":"function_call","call_id":"call-1",` +
			`"name":"read","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.done","output_index":0,` +
			`"item_id":"fc-1","name":"read","arguments":"{ \"path\": ` +
			`\"file.txt\", \"depth\": 1 }"}`,
		completedEvent(
			`[{"id":"fc-1","type":"function_call","call_id":"call-1",` +
				`"name":"read","arguments":"{\"depth\":1.0,\"path\":\"file.txt\"}` +
				`","status":"completed"}]`,
		),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, model.OutcomeToolUse, events[len(events)-1].Response.OrEmpty().Outcome.OrEmpty())
}

// TestDriverStreamRejectsConflictingFinalizedFunctionArguments verifies terminal output cannot replace finalized
// values.
func TestDriverStreamRejectsConflictingFinalizedFunctionArguments(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,` +
			`"item":{"id":"fc-1","type":"function_call","call_id":"call-1",` +
			`"name":"read","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.done","output_index":0,` +
			`"item_id":"fc-1","name":"read",` +
			`"arguments":"{\"path\":\"approved.txt\"}"}`,
		completedEvent(
			`[{"id":"fc-1","type":"function_call","call_id":"call-1",` +
				`"name":"read","arguments":"{\"path\":\"replaced.txt\"}",` +
				`"status":"completed"}]`,
		),
	}, nil)

	require.Error(t, err)
	require.NotEmpty(t, events)
	assert.Equal(t, model.OutcomeFailed, events[len(events)-1].Response.OrEmpty().Outcome.OrEmpty())
	assert.Equal(t, requestFailedMessage, events[len(events)-1].Response.OrEmpty().ErrorMessage.OrEmpty())
	assert.NotContains(t, streamEventKinds(events), run.StreamEventDone)
}

// TestDriverStreamRejectsConflictingFinalizedCustomInput verifies terminal output cannot replace finalized input.
func TestDriverStreamRejectsConflictingFinalizedCustomInput(t *testing.T) {
	t.Parallel()

	descriptor := constrainedDescriptor(0, tool.GrammarVariants{Lark: mo.None[string](), Regex: mo.Some("[a-z]+")})
	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,` +
			`"item":{"id":"ctc-1","type":"custom_tool_call",` +
			`"call_id":"call-1","name":"sample","input":"",` +
			`"status":"in_progress"}}`,
		`{"type":"response.custom_tool_call_input.done","output_index":0,"item_id":"ctc-1","input":"approved"}`,
		completedEvent(
			`[{"id":"ctc-1","type":"custom_tool_call","call_id":"call-1",` +
				`"name":"sample","input":"replaced","status":"completed"}]`,
		),
	}, []tool.Descriptor{descriptor})

	require.Error(t, err)
	require.NotEmpty(t, events)
	assert.Equal(t, model.OutcomeFailed, events[len(events)-1].Response.OrEmpty().Outcome.OrEmpty())
	assert.Equal(t, requestFailedMessage, events[len(events)-1].Response.OrEmpty().ErrorMessage.OrEmpty())
	assert.NotContains(t, streamEventKinds(events), run.StreamEventDone)
}

// TestDriverStreamRejectsConflictingFunctionDeltaIdentity verifies output index cannot mask a provider conflict.
func TestDriverStreamRejectsConflictingFunctionDeltaIdentity(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,` +
			`"item":{"id":"fc-1","type":"function_call","call_id":"call-1",` +
			`"name":"read","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc-other","delta":"{}"}`,
	}, nil)

	require.Error(t, err)
	require.NotEmpty(t, events)
	assert.Equal(t, model.OutcomeFailed, events[len(events)-1].Response.OrEmpty().Outcome.OrEmpty())
	assert.NotContains(t, streamEventKinds(events), run.StreamEventToolCallEnd)
}

// TestDriverStreamRejectsInvalidTerminalFunctionArguments verifies recovered calls keep strict JSON decoding.
func TestDriverStreamRejectsInvalidTerminalFunctionArguments(t *testing.T) {
	t.Parallel()

	events, err := streamOmittedToolEvents(t, []string{
		completedEvent(
			`[{"id":"fc-1","type":"function_call","call_id":"call-1",` +
				`"name":"read","arguments":"{\"path\":","status":"completed"}]`,
		),
	}, nil)

	require.Error(t, err)
	assert.NotContains(t, streamEventKinds(events), run.StreamEventToolCallEnd)
}

// TestDriverStreamRecoversCustomCallWithoutAddedEvent verifies authoritative custom input is preserved exactly.
func TestDriverStreamRecoversCustomCallWithoutAddedEvent(t *testing.T) {
	t.Parallel()

	descriptor := constrainedDescriptor(0, tool.GrammarVariants{Lark: mo.None[string](), Regex: mo.Some("[a-z]+")})
	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"ab"}`,
		`{"type":"response.output_item.done","output_index":0,` +
			`"item":{"id":"ctc-1","type":"custom_tool_call",` +
			`"call_id":"call-1","name":"sample","input":"abc",` +
			`"status":"completed"}}`,
		completedEvent(`[]`),
	}, []tool.Descriptor{descriptor})

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, map[string]any{"payload": "abc"}, events[1].ToolCall.OrEmpty().Arguments)
}

// TestDriverStreamRecoversCustomCallFromTerminalOutput verifies terminal custom input closes an active call once.
func TestDriverStreamRecoversCustomCallFromTerminalOutput(t *testing.T) {
	t.Parallel()

	descriptor := constrainedDescriptor(0, tool.GrammarVariants{Lark: mo.None[string](), Regex: mo.Some("[a-z]+")})
	events, err := streamOmittedToolEvents(t, []string{
		`{"type":"response.output_item.added","output_index":0,` +
			`"item":{"id":"ctc-1","type":"custom_tool_call",` +
			`"call_id":"call-1","name":"sample","input":"",` +
			`"status":"in_progress"}}`,
		`{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"ab"}`,
		completedEvent(
			`[{"id":"ctc-1","type":"custom_tool_call","call_id":"call-1","name":"sample","input":"abc","status":"completed"}]`,
		),
	}, []tool.Descriptor{descriptor})

	require.NoError(t, err)
	require.Equal(t, []run.StreamEventKind{
		run.StreamEventToolCallStart, run.StreamEventToolCallDelta,
		run.StreamEventToolCallEnd, run.StreamEventDone,
	}, streamEventKinds(events))
	assert.Equal(t, mo.Some("ab"), events[1].Preview.OrEmpty().Fields[0].Prefix)
	assert.Equal(t, map[string]any{"payload": "abc"}, events[2].ToolCall.OrEmpty().Arguments)
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
	service := newDriver(testConfig(), credentials, interaction, testProviderOptions(server))
	events := make([]run.StreamEvent, 0)
	err := service.Stream(t.Context(), run.ModelRequest{
		Instructions: "test", Model: testModelDescriptor("gpt-test"),
		ReasoningChoice: model.ReasoningChoiceHigh, History: nil, Tools: tools,
	}, func(event run.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	return events, err
}
