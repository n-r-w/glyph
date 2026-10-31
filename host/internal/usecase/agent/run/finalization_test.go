//go:build !integration

package run

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// finalizationFixture runs Core with strict provider, tool, and finalizer mocks and records delivered events.
type finalizationFixture struct {
	// provider streams the scripted model responses.
	provider *MockModelProvider
	// tools executes the scripted tool calls.
	tools *MockToolRuntime
	// finalizer returns the final values of terminal model responses and tool results.
	finalizer *MockMessageFinalizer
	// requests records each effective model request.
	requests []ModelRequest
	// events records each delivered lifecycle event.
	events []agent.Event
	// service is the Core under test.
	service *Service
}

// newFinalizationFixture creates Core whose effective context keeps the assembled request.
func newFinalizationFixture(t *testing.T) *finalizationFixture {
	t.Helper()
	controller := gomock.NewController(t)
	fixture := &finalizationFixture{
		provider: NewMockModelProvider(controller), tools: NewMockToolRuntime(controller),
		finalizer: NewMockMessageFinalizer(controller), requests: nil, events: nil, service: nil,
	}
	contexts := NewMockEffectiveContextPort(controller)
	contexts.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request ContextRequest) (EffectiveContext, error) {
			effective, err := passThroughContext(testModelDescriptor, model.ReasoningChoiceOff, fixture.provider)(
				ctx, request,
			)
			fixture.requests = append(fixture.requests, effective.Request)
			return effective, err
		},
	).AnyTimes()
	events := NewMockEventSink(controller)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		fixture.events = append(fixture.events, event)
		return nil
	}).AnyTimes()
	fixture.tools.EXPECT().Tools().Return(nil).AnyTimes()
	fixture.service = New(testInstructions, contexts, fixture.tools, events, newMockHistoryStore(t), fixture.finalizer)
	return fixture
}

// run executes one user request.
func (fixture *finalizationFixture) run(t *testing.T) (runcontrol.Result, error) {
	t.Helper()
	return fixture.service.Run(t.Context(), runcontrol.Request{RunID: "run", Message: model.TextMessage("hi")})
}

// eventsOf returns delivered events of one type in delivery order.
func (fixture *finalizationFixture) eventsOf(eventType agent.EventType) []agent.Event {
	var result []agent.Event
	for _, event := range fixture.events {
		if event.Type == eventType {
			result = append(result, event)
		}
	}
	return result
}

// finalizationResponse creates a terminal response with Host-owned identity and the given content.
func finalizationResponse(outcome model.Outcome, content ...model.Content) model.Response {
	return model.Response{
		Content: content, Outcome: mo.Some(outcome), ErrorMessage: mo.None[string](),
		Provider: mo.Some[model.ProviderID]("provider"), Model: mo.Some[model.ID]("model"),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.Some("response"),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
}

// textResult creates a successful tool result with one text block.
func textResult(call model.ToolCall, text string) agent.ToolResult {
	return agent.ToolResult{CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents(text), IsError: false}
}

// modelHistory wraps a model response in a history entry.
func modelHistory(response model.Response) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
		ToolResult: mo.None[agent.ToolResult](),
	}
}

// toolHistory wraps a tool result in a history entry.
func toolHistory(result agent.ToolResult) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(result),
	}
}

// TestRunUsesFinalizedModelAndToolValues verifies that final values reach history, events, tool execution, and the
// next model request.
func TestRunUsesFinalizedModelAndToolValues(t *testing.T) {
	t.Parallel()
	// Arrange a tool-use turn whose response and result the finalizer replaces, followed by a stop turn.
	fixture := newFinalizationFixture(t)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{"path":"a"}`)}
	finalCall := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{"path":"b"}`)}
	first := finalizationResponse(model.OutcomeToolUse, testTextItem("plan"), testCallItem(call))
	finalFirst := finalizationResponse(model.OutcomeToolUse, testTextItem("final plan"), testCallItem(finalCall))
	finalResult := textResult(finalCall, "final file")
	stop := finalizationResponse(model.OutcomeStop, testTextItem("done"))
	gomock.InOrder(
		fixture.provider.EXPECT().
			Stream(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(streamResult(first, nil)),
		fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), first).Return(finalFirst, nil),
		fixture.tools.EXPECT().
			Execute(gomock.Any(), finalCall, gomock.Any()).
			Return(textResult(finalCall, "file"), nil),
		fixture.finalizer.EXPECT().
			FinalizeToolResult(gomock.Any(), textResult(finalCall, "file")).
			Return(finalResult, nil),
		fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(stop, nil)),
		fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), stop).Return(stop, nil),
	)

	// Act through one run.
	result, err := fixture.run(t)

	// Assert history, completion events, run summary, and the next model request use the final values.
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	history := fixture.service.History()
	require.Equal(t, []agent.HistoryEntry{modelHistory(finalFirst), toolHistory(finalResult), modelHistory(stop)},
		history[1:])
	messageEnds := fixture.eventsOf(agent.EventMessageEnd)
	require.Len(t, messageEnds, 2)
	assert.Equal(t, mo.Some(finalFirst), messageEnds[0].Message)
	assert.Equal(t, mo.Some(finalResult), fixture.eventsOf(agent.EventToolExecutionEnd)[0].ToolResult)
	assert.Equal(t, mo.Some(finalResult), fixture.eventsOf(agent.EventToolResult)[0].ToolResult)
	assert.Equal(t, mo.Some(agent.TurnSummary{Response: finalFirst, ToolResults: []agent.ToolResult{finalResult}}),
		fixture.eventsOf(agent.EventTurnEnd)[0].Turn)
	assert.Equal(t, history, fixture.eventsOf(agent.EventAgentEnd)[0].Agent.MustGet().AddedHistory)
	require.Len(t, fixture.requests, 2)
	assert.Equal(t, history[:3], fixture.requests[1].History)
}

// TestRunFinalizesLengthToolErrors verifies that generated results of a length-limited response are finalized.
func TestRunFinalizesLengthToolErrors(t *testing.T) {
	t.Parallel()
	// Arrange a length-limited response with one tool call and a finalizer that replaces the generated result.
	fixture := newFinalizationFixture(t)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{}`)}
	response := finalizationResponse(model.OutcomeLength, testCallItem(call))
	generated := agent.ToolResult{
		CallID: "call", ToolName: "read", Contents: tool.TextContents(lengthCallMessage), IsError: true,
	}
	finalResult := textResult(call, "final skipped")
	stop := finalizationResponse(model.OutcomeStop, testTextItem("done"))
	gomock.InOrder(
		fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			streamResult(response, nil),
		),
		fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), response).Return(response, nil),
		fixture.finalizer.EXPECT().FinalizeToolResult(gomock.Any(), generated).Return(finalResult, nil),
		fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(stop, nil)),
		fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), stop).Return(stop, nil),
	)

	// Act through one run.
	_, err := fixture.run(t)

	// Assert the final result in history, its event, and its turn summary.
	require.NoError(t, err)
	assert.Equal(t, toolHistory(finalResult), fixture.service.History()[2])
	assert.Equal(t, mo.Some(finalResult), fixture.eventsOf(agent.EventToolResult)[0].ToolResult)
	assert.Equal(t, []agent.ToolResult{finalResult}, fixture.eventsOf(agent.EventTurnEnd)[0].Turn.MustGet().ToolResults)
}

// TestRunStopsWhenModelFinalizationFails verifies that a failed model finalization appends nothing and ends the run
// with the finalization error.
func TestRunStopsWhenModelFinalizationFails(t *testing.T) {
	t.Parallel()
	// Arrange a successful tool-use response whose finalization fails once.
	fixture := newFinalizationFixture(t)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{}`)}
	response := finalizationResponse(model.OutcomeToolUse, testCallItem(call))
	finalizationErr := errors.New(`middleware handler "finalized": extension source text`)
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, nil))
	fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), response).Return(model.Response{}, finalizationErr)

	// Act through one run.
	result, err := fixture.run(t)

	// Assert the failed run keeps the finalization cause, executes no tool, and appends no model response.
	require.ErrorIs(t, err, finalizationErr)
	require.NotErrorIs(t, err, agent.ErrPersistenceUnavailable)
	require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	require.Len(t, fixture.service.History(), 1)
	assert.Empty(t, fixture.eventsOf(agent.EventMessageEnd))
	summary := fixture.eventsOf(agent.EventAgentEnd)[0].Agent.MustGet()
	assert.Equal(t, mo.Some(finalizationErr.Error()), summary.ErrorMessage)
	assert.Len(t, summary.AddedHistory, 1)
}

// TestRunStopsWhenToolResultFinalizationFails verifies that a failed tool-result finalization keeps the executed
// call and the committed response, and appends no result.
func TestRunStopsWhenToolResultFinalizationFails(t *testing.T) {
	t.Parallel()
	// Arrange a tool-use response whose executed result fails finalization.
	fixture := newFinalizationFixture(t)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{}`)}
	response := finalizationResponse(model.OutcomeToolUse, testCallItem(call))
	finalizationErr := errors.New(`middleware handler "finalized": extension source text`)
	gomock.InOrder(
		fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			streamResult(response, nil),
		),
		fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), response).Return(response, nil),
		fixture.tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).Return(textResult(call, "file"), nil),
		fixture.finalizer.EXPECT().FinalizeToolResult(gomock.Any(), textResult(call, "file")).Return(
			agent.ToolResult{}, finalizationErr,
		),
	)

	// Act through one run.
	result, err := fixture.run(t)

	// Assert the failed run keeps the committed response and appends no result.
	require.ErrorIs(t, err, finalizationErr)
	require.NotErrorIs(t, err, agent.ErrPersistenceUnavailable)
	require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	require.Equal(t, []agent.HistoryEntry{modelHistory(response)}, fixture.service.History()[1:])
	assert.Empty(t, fixture.eventsOf(agent.EventToolExecutionEnd))
	assert.Empty(t, fixture.eventsOf(agent.EventToolResult))
}

// TestProviderFailureFinalization verifies retained failed responses are finalized once and canceled responses are
// recorded without finalization.
func TestProviderFailureFinalization(t *testing.T) {
	t.Parallel()
	providerErr := errors.New("provider unavailable")
	finalizationErr := errors.New(`middleware handler "finalized": extension source text`)
	for name, test := range map[string]struct {
		providerErr error
		finalize    func(*MockMessageFinalizer)
		history     []agent.HistoryEntry
		err         error
	}{
		"failed response is finalized": {
			providerErr: providerErr,
			finalize: func(finalizer *MockMessageFinalizer) {
				finalizer.EXPECT().FinalizeModelResponse(
					gomock.Any(), retainedResponse("partial", model.OutcomeFailed, providerErr.Error()),
				).Return(retainedResponse("final partial", model.OutcomeFailed, providerErr.Error()), nil)
			},
			history: []agent.HistoryEntry{
				modelHistory(retainedResponse("final partial", model.OutcomeFailed, providerErr.Error())),
			},
			err: providerErr,
		},
		"canceled response is not finalized": {
			providerErr: context.Canceled,
			finalize:    func(*MockMessageFinalizer) {},
			history: []agent.HistoryEntry{
				modelHistory(retainedResponse("partial", model.OutcomeAborted, abortedModelMessage)),
			},
			err: context.Canceled,
		},
		"failed finalization appends nothing": {
			providerErr: providerErr,
			finalize: func(finalizer *MockMessageFinalizer) {
				finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), gomock.Any()).Return(
					model.Response{}, finalizationErr,
				)
			},
			history: []agent.HistoryEntry{},
			err:     finalizationErr,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a provider that fails after streaming partial text.
			fixture := newFinalizationFixture(t)
			fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
					require.NoError(t, emitText(handle, 0, "partial"))
					return test.providerErr
				},
			)
			test.finalize(fixture.finalizer)

			// Act through one run.
			_, err := fixture.run(t)

			// Assert the run error and the recorded response.
			require.ErrorIs(t, err, test.err)
			require.Equal(t, test.history, fixture.service.History()[1:])
		})
	}
}

// retainedResponse creates the response that Core retains after a provider failure.
func retainedResponse(text string, outcome model.Outcome, errorMessage string) model.Response {
	return model.Response{
		Content: []model.Content{testTextItem(text)}, Outcome: mo.Some(outcome), ErrorMessage: mo.Some(errorMessage),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
}

// TestRunCanceledBeforeFinalization verifies that Agent Core records the unchanged terminal response that the
// finalizer returns after the run was canceled.
func TestRunCanceledBeforeFinalization(t *testing.T) {
	t.Parallel()
	// Arrange a successful stream that cancels the run and a finalizer that returns the response unchanged.
	fixture := newFinalizationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	response := finalizationResponse(model.OutcomeStop, testTextItem("done"))
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			defer cancel()
			return emitStream(handle, response, nil)
		},
	)
	fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), response).Return(response, nil)

	// Act through one run.
	_, err := fixture.service.Run(ctx, runcontrol.Request{RunID: "run", Message: model.TextMessage("hi")})

	// Assert the unchanged response in history and in its completion event.
	require.NoError(t, err)
	require.Equal(t, []agent.HistoryEntry{modelHistory(response)}, fixture.service.History()[1:])
	assert.Equal(t, mo.Some(response), fixture.eventsOf(agent.EventMessageEnd)[0].Message)
}

// TestRunCanceledBeforeToolResultFinalization verifies that Agent Core records the unchanged tool result that the
// finalizer returns after the tool canceled the run.
func TestRunCanceledBeforeToolResultFinalization(t *testing.T) {
	t.Parallel()
	// Arrange a tool-use response whose tool cancels the run before returning its result.
	fixture := newFinalizationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{}`)}
	response := finalizationResponse(model.OutcomeToolUse, testCallItem(call))
	gomock.InOrder(
		fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			streamResult(response, nil),
		),
		fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), response).Return(response, nil),
		fixture.tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).DoAndReturn(
			func(context.Context, model.ToolCall, tool.ProgressHandler) (agent.ToolResult, error) {
				cancel()
				return textResult(call, "file"), nil
			},
		),
		fixture.finalizer.EXPECT().FinalizeToolResult(gomock.Any(), textResult(call, "file")).Return(
			textResult(call, "file"), nil,
		),
	)

	// Act through one run.
	result, _ := fixture.service.Run(ctx, runcontrol.Request{RunID: "run", Message: model.TextMessage("hi")})

	// Assert the aborted run and the unchanged result in history and in its event.
	require.Equal(t, agent.RunOutcomeAborted, result.Outcome)
	require.Equal(t, []agent.HistoryEntry{modelHistory(response), toolHistory(textResult(call, "file"))},
		fixture.service.History()[1:])
	assert.Equal(t, mo.Some(textResult(call, "file")), fixture.eventsOf(agent.EventToolResult)[0].ToolResult)
}

// TestRunCanceledDuringFinalization verifies that cancellation while a finalization chain runs aborts the run
// without an append.
func TestRunCanceledDuringFinalization(t *testing.T) {
	t.Parallel()
	// Arrange a finalizer that observes cancellation while it runs.
	fixture := newFinalizationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	response := finalizationResponse(model.OutcomeStop, testTextItem("done"))
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, nil))
	fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), response).DoAndReturn(
		func(ctx context.Context, _ model.Response) (model.Response, error) {
			cancel()
			return model.Response{}, fmt.Errorf(`middleware handler "finalized": %w`, ctx.Err())
		},
	)

	// Act through one run.
	result, err := fixture.service.Run(ctx, runcontrol.Request{RunID: "run", Message: model.TextMessage("hi")})

	// Assert the aborted run and the absent model response.
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, agent.RunOutcomeAborted, result.Outcome)
	require.Len(t, fixture.service.History(), 1)
	assert.Empty(t, fixture.eventsOf(agent.EventMessageEnd))
}

// TestRunCanceledAfterFinalization verifies that cancellation after a successful finalization keeps the final value.
func TestRunCanceledAfterFinalization(t *testing.T) {
	t.Parallel()
	// Arrange a finalizer that replaces the response and then observes cancellation.
	fixture := newFinalizationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	response := finalizationResponse(model.OutcomeStop, testTextItem("done"))
	final := finalizationResponse(model.OutcomeStop, testTextItem("final done"))
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, nil))
	fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), response).DoAndReturn(
		func(context.Context, model.Response) (model.Response, error) {
			cancel()
			return final, nil
		},
	)

	// Act through one run.
	_, _ = fixture.service.Run(ctx, runcontrol.Request{RunID: "run", Message: model.TextMessage("hi")})

	// Assert the final value in history and in its completion event.
	require.Equal(t, []agent.HistoryEntry{modelHistory(final)}, fixture.service.History()[1:])
	assert.Equal(t, mo.Some(final), fixture.eventsOf(agent.EventMessageEnd)[0].Message)
}

// TestProviderFailureCanceledDuringFinalization verifies that cancellation while a retained failed response is
// finalized aborts the run without an append.
func TestProviderFailureCanceledDuringFinalization(t *testing.T) {
	t.Parallel()
	// Arrange a provider failure after partial text and a finalizer that observes cancellation while it runs.
	fixture := newFinalizationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ModelRequest, handle StreamHandler) error {
			require.NoError(t, emitText(handle, 0, "partial"))
			return errors.New("provider unavailable")
		},
	)
	fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ model.Response) (model.Response, error) {
			cancel()
			return model.Response{}, fmt.Errorf(`middleware handler "finalized": %w`, ctx.Err())
		},
	)

	// Act through one run.
	result, err := fixture.service.Run(ctx, runcontrol.Request{RunID: "run", Message: model.TextMessage("hi")})

	// Assert the aborted run, both causes, and no recorded response.
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "provider unavailable")
	require.Equal(t, agent.RunOutcomeAborted, result.Outcome)
	require.Len(t, fixture.service.History(), 1)
}
