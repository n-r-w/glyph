//go:build !integration

package run

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// concurrentDescriptor declares the tool used by signal-driven Core tests.
func concurrentDescriptor() tool.Descriptor {
	return tool.Descriptor{
		Name: "read", Description: "Read.", InputSchemaJSON: nil,
		ConstrainedSampling: mo.None[tool.ConstrainedSampling](), ReplacesTool: false, Concurrent: true,
	}
}

// TestConcurrentCallsFinalizeIndependently verifies reversed completion, independent finalizers and serial barriers.
func TestConcurrentCallsFinalizeIndependently(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange two concurrent calls, two undeclared barriers and another concurrent call.
		fixture := newFinalizationFixture(t, concurrentDescriptor())
		calls := []model.ToolCall{
			{ID: "first", Name: "read", Arguments: testToolCallArguments(`{}`)},
			{ID: "second", Name: "read", Arguments: testToolCallArguments(`{}`)},
			{ID: "barrier", Name: "serial", Arguments: testToolCallArguments(`{}`)},
			{ID: "barrier-two", Name: "serial", Arguments: testToolCallArguments(`{}`)},
			{ID: "last", Name: "read", Arguments: testToolCallArguments(`{}`)},
		}
		content := make([]model.Content, len(calls))
		for index, call := range calls {
			content[index] = testCallItem(call)
		}
		response := finalizationResponse(model.OutcomeToolUse, content...)
		stop := finalizationResponse(model.OutcomeStop, testTextItem("done"))
		gomock.InOrder(
			fixture.provider.EXPECT().
				Stream(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(streamResult(response, nil)),
			fixture.provider.EXPECT().
				Stream(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(streamResult(stop, nil)),
		)
		fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, value model.Response) (model.Response, error) { return value, nil },
		).AnyTimes()
		started := make(chan string, len(calls))
		finalizing := make(chan string, len(calls))
		release := make(map[string]chan struct{}, len(calls))
		for _, call := range calls {
			release[call.ID] = make(chan struct{})
			fixture.tools.EXPECT().Execute(gomock.Any(), call, gomock.Any()).DoAndReturn(
				func(_ context.Context, current model.ToolCall, progress tool.ProgressHandler) (agent.ToolResult, error) {
					started <- current.ID
					require.NoError(
						t,
						progress(tool.Progress{Channel: tool.ProgressChannelStatus, Content: current.ID}),
					)
					return textResult(current, "raw"), nil
				},
			)
		}
		fixture.finalizer.EXPECT().FinalizeToolResult(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, value agent.ToolResult) (agent.ToolResult, error) {
				finalizing <- value.CallID
				<-release[value.CallID]
				value.Contents = tool.TextContents("final " + value.CallID)
				return value, nil
			},
		).Times(len(calls))
		finished := make(chan struct{})
		go func() { defer close(finished); _, err := fixture.run(t); assert.NoError(t, err) }()
		// Act by finishing the second finalizer before the first.
		synctest.Wait()
		assert.Len(t, started, 2)
		assert.Len(t, finalizing, 2)
		close(release["second"])
		synctest.Wait()
		assert.Len(t, fixture.service.History(), 2)
		ends := fixture.eventsOf(agent.EventToolExecutionEnd)
		if assert.Len(t, ends, 1) {
			assert.Equal(t, "second", ends[0].ToolResult.OrEmpty().CallID)
		}
		assert.Empty(t, fixture.eventsOf(agent.EventToolResult))
		close(release["first"])
		synctest.Wait()
		assert.Len(t, started, 3)
		close(release["barrier"])
		synctest.Wait()
		assert.Len(t, started, 4)
		close(release["barrier-two"])
		synctest.Wait()
		assert.Len(t, started, 5)
		close(release["last"])
		<-finished
		// Assert ordered finalized history and the next model request retain model call order.
		require.Len(t, fixture.requests, 2)
		results := fixture.eventsOf(agent.EventToolResult)
		require.Len(t, results, len(calls))
		for index, call := range calls {
			assert.Equal(t, call.ID, fixture.requests[1].History[index+2].ToolResult.OrEmpty().CallID)
			assert.Equal(t, textResult(call, "final "+call.ID), results[index].ToolResult.OrEmpty())
		}
	})
}

// TestConcurrentTerminalCancellation verifies sibling cancellation, joining and persistence after terminal failure.
func TestConcurrentTerminalCancellation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"fatal", "finalizer", "progress", "end", "abort", "tool-cancel"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange a finalized result, a failing call and a sibling that delays cancellation cleanup.
				fixture := newFinalizationFixture(t, concurrentDescriptor())
				calls := []model.ToolCall{
					{ID: "completed", Name: "read", Arguments: testToolCallArguments(`{}`)},
					{ID: "failing", Name: "read", Arguments: testToolCallArguments(`{}`)},
					{ID: "sibling", Name: "read", Arguments: testToolCallArguments(`{}`)},
					{ID: "later", Name: "serial", Arguments: testToolCallArguments(`{}`)},
				}
				response := finalizationResponse(
					model.OutcomeToolUse,
					testCallItem(calls[0]),
					testCallItem(calls[1]),
					testCallItem(calls[2]),
					testCallItem(calls[3]),
				)
				fixture.provider.EXPECT().
					Stream(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(streamResult(response, nil))
				fixture.finalizer.EXPECT().FinalizeModelResponse(gomock.Any(), response).Return(response, nil)
				trigger := make(chan struct{})
				joined := make(chan struct{})
				canceled := make(chan struct{})
				cleanup := make(chan struct{})
				terminal := errors.New("terminal original cause")
				siblingCause := errors.New("sibling cleanup original cause")
				fatal := NewMockFatalOperationFailure(gomock.NewController(t))
				fatal.EXPECT().Error().Return(terminal.Error()).AnyTimes()
				fixture.tools.EXPECT().
					Execute(gomock.Any(), calls[0], gomock.Any()).
					Return(textResult(calls[0], "complete"), nil)
				fixture.tools.EXPECT().Execute(gomock.Any(), calls[1], gomock.Any()).DoAndReturn(
					func(_ context.Context, call model.ToolCall, progress tool.ProgressHandler) (agent.ToolResult, error) {
						<-trigger
						switch mode {
						case "fatal":
							return agent.ToolResult{}, fatal
						case "tool-cancel":
							return agent.ToolResult{}, context.Canceled
						case "progress":
							return agent.ToolResult{}, progress(
								tool.Progress{Channel: tool.ProgressChannelStatus, Content: "update"},
							)
						default:
							return textResult(call, "raw"), nil
						}
					},
				)
				fixture.tools.EXPECT().Execute(gomock.Any(), calls[2], gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ model.ToolCall, _ tool.ProgressHandler) (agent.ToolResult, error) {
						<-ctx.Done()
						close(canceled)
						<-cleanup
						close(joined)
						if mode == "abort" || mode == "tool-cancel" {
							return agent.ToolResult{}, ctx.Err()
						}
						return agent.ToolResult{}, errors.Join(ctx.Err(), siblingCause)
					},
				)
				fixture.finalizer.EXPECT().FinalizeToolResult(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, value agent.ToolResult) (agent.ToolResult, error) {
						if value.CallID == "sibling" {
							return agent.ToolResult{}, ctx.Err()
						}
						if value.CallID == "failing" && mode == "finalizer" {
							return agent.ToolResult{}, terminal
						}
						return value, nil
					},
				).AnyTimes()
				sink := NewMockEventSink(gomock.NewController(t))
				sink.EXPECT().
					Deliver(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, event agent.Event) error {
						fixture.events = append(fixture.events, event)
						if (mode == "progress" && event.Type == agent.EventToolExecutionUpdate) ||
							(mode == "end" && event.Type == agent.EventToolExecutionEnd && event.ToolCall.OrEmpty().ID == "failing") {
							return terminal
						}
						return nil
					}).
					AnyTimes()
				fixture.service.events = sink
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				finished := make(chan struct{})
				var result runcontrol.Result
				var runErr error
				go func() {
					defer close(finished)
					result, runErr = fixture.service.Run(
						ctx,
						runcontrol.Request{RunID: "terminal", Message: model.TextMessage("go")},
					)
				}()
				synctest.Wait()
				// Act only after the first result has completed finalization and the siblings are blocked.
				assert.Len(t, fixture.eventsOf(agent.EventToolExecutionEnd), 1)
				close(trigger)
				if mode == "abort" {
					cancel()
				}
				synctest.Wait()
				select {
				case <-canceled:
				default:
					t.Error("sibling was not canceled promptly")
				}
				select {
				case <-finished:
					t.Error("run settled before sibling cleanup")
				default:
				}
				close(cleanup)
				synctest.Wait()
				select {
				case <-finished:
				default:
					t.Fatal("run did not finish after cleanup")
				}
				// Assert every independent cause survives, and completed results persist before settlement.
				require.Error(t, runErr)
				if mode != "abort" && mode != "tool-cancel" {
					require.ErrorIs(t, runErr, siblingCause)
				}
				if mode == "fatal" {
					require.ErrorIs(t, runErr, fatal)
				} else if mode != "abort" && mode != "tool-cancel" {
					require.ErrorIs(t, runErr, terminal)
				}
				if mode == "abort" || mode == "tool-cancel" {
					assert.Equal(t, agent.RunOutcomeAborted, result.Outcome)
				} else {
					assert.Equal(t, agent.RunOutcomeFailed, result.Outcome)
				}
				select {
				case <-joined:
				default:
					t.Error("sibling was not joined")
				}
				history := fixture.service.History()
				require.GreaterOrEqual(t, len(history), 3)
				assert.Equal(t, "completed", history[2].ToolResult.OrEmpty().CallID)
				for index, event := range fixture.eventsOf(agent.EventToolResult) {
					assert.Equal(t, calls[index].ID, event.ToolResult.OrEmpty().CallID)
				}
			})
		})
	}
}

// TestEventDeliveryIsSerialized verifies concurrent callers never enter the event sink together.
func TestEventDeliveryIsSerialized(t *testing.T) {
	t.Parallel()
	// Arrange a sink that records overlap while yielding to competing callers.
	sink := NewMockEventSink(gomock.NewController(t))
	var active atomic.Int64
	var overlap atomic.Bool
	sink.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, agent.Event) error {
		if active.Add(1) != 1 {
			overlap.Store(true)
		}
		runtime.Gosched()
		active.Add(-1)
		return nil
	}).Times(64)
	service := New("", nil, nil, sink, nil, nil)
	start := make(chan struct{})
	var workers sync.WaitGroup
	// Act with callers released by the same signal.
	for range 64 {
		workers.Go(func() {
			<-start
			assert.NoError(t, service.deliver(t.Context(), newEvent(agent.EventToolExecutionUpdate, "run")))
		})
	}
	close(start)
	workers.Wait()
	// Assert no sink invocation overlapped another invocation.
	assert.False(t, overlap.Load())
}
