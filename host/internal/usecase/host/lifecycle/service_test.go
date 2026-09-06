//go:build !integration

package lifecycle

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
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// lifecycleEvent creates one payload-free source event for observer policy tests.
func lifecycleEvent(eventType run.EventType) run.Event {
	return run.Event{
		Type: eventType, RunID: "run", Position: mo.None[int](), Content: mo.None[model.Content](),
		Message: mo.None[model.Response](), Preview: mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](), Progress: mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](), Turn: mo.None[run.TurnSummary](), Agent: mo.None[run.AgentSummary](),
	}
}

// TestServiceObservesEveryLifecycleGroup verifies source event kinds select the approved observer groups.
func TestServiceObservesEveryLifecycleGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		eventType run.EventType
		kind      startup.RawHandlerKind
	}{
		{name: "agent start", eventType: run.EventAgentStart, kind: startup.RawHandlerKindAgentStart},
		{name: "agent end", eventType: run.EventAgentEnd, kind: startup.RawHandlerKindAgentEnd},
		{name: "turn start", eventType: run.EventTurnStart, kind: startup.RawHandlerKindTurnStart},
		{name: "turn end", eventType: run.EventTurnEnd, kind: startup.RawHandlerKindTurnEnd},
		{name: "message start", eventType: run.EventMessageStart, kind: startup.RawHandlerKindMessageStart},
		{name: "content start", eventType: run.EventContentStart, kind: startup.RawHandlerKindMessageUpdate},
		{name: "text delta", eventType: run.EventTextDelta, kind: startup.RawHandlerKindMessageUpdate},
		{name: "content end", eventType: run.EventContentEnd, kind: startup.RawHandlerKindMessageUpdate},
		{name: "tool call start", eventType: run.EventToolCallStart, kind: startup.RawHandlerKindMessageUpdate},
		{name: "tool call delta", eventType: run.EventToolCallDelta, kind: startup.RawHandlerKindMessageUpdate},
		{name: "tool call end", eventType: run.EventToolCallEnd, kind: startup.RawHandlerKindMessageUpdate},
		{name: "message end", eventType: run.EventMessageEnd, kind: startup.RawHandlerKindMessageEnd},
		{
			name:      "tool execution start",
			eventType: run.EventToolExecutionStart,
			kind:      startup.RawHandlerKindToolExecutionStart,
		},
		{
			name:      "tool execution update",
			eventType: run.EventToolExecutionUpdate,
			kind:      startup.RawHandlerKindToolExecutionUpdate,
		},
		{
			name:      "tool execution end",
			eventType: run.EventToolExecutionEnd,
			kind:      startup.RawHandlerKindToolExecutionEnd,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange one matching observer and one current delivery context.
			controller := gomock.NewController(t)
			runtime := NewMockRuntime(controller)
			contexts := NewMockContextIssuer(controller)
			issues := NewMockIssueDelivery(controller)
			service := New(runtime, contexts)
			service.BindIssueDelivery(issues)
			accepted, err := service.ValidateLifecycleHandlers(startup.PendingRegistration{
				ID: "extension", Path: "/extension", Tools: nil,
				Handlers: []startup.RawHandlerDescriptor{{Present: true, ID: "observer", Kind: test.kind}},
			})
			require.NoError(t, err)
			service.CommitLifecycleHandlers([]startup.AcceptedRegistration{{
				ID: "extension", Path: "/extension", Tools: nil, Handlers: accepted,
			}})
			binding := extension.Context{
				ID:                "context",
				ExtensionID:       "extension",
				RuntimeInstanceID: "runtime",
				SessionID:         "session",
				WorkingDirectory:  "/cwd",
			}
			runtime.EXPECT().HandlerRuntimeAvailable("extension").Return(true)
			contexts.EXPECT().IssueContext("extension").Return(binding, nil)
			runtime.EXPECT().ObserveLifecycle(t.Context(), "extension", "observer", binding, gomock.Any()).DoAndReturn(
				func(_ context.Context, _, _ string, _ extension.Context, event Event) (bool, error) {
					assert.False(t, event.Settled)
					assert.Equal(t, test.eventType, event.Agent.Type)
					return false, nil
				},
			)

			// Act by observing the source event.
			err = service.Observe(t.Context(), lifecycleEvent(test.eventType))

			// Assert the matching observer completes successfully.
			require.NoError(t, err)
		})
	}
}

// TestServiceObservesAgentSettlement verifies Host-owned settlement uses its separate lifecycle group.
func TestServiceObservesAgentSettlement(t *testing.T) {
	t.Parallel()

	// Arrange one settlement observer and current delivery context.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	service := New(runtime, contexts)
	service.CommitLifecycleHandlers([]startup.AcceptedRegistration{{
		ID: "extension", Path: "/extension", Tools: nil,
		Handlers: []startup.AcceptedHandler{{ID: "settled", Kind: startup.RawHandlerKindAgentSettled}},
	}})
	binding := extension.Context{
		ID:                "context",
		ExtensionID:       "extension",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	runtime.EXPECT().HandlerRuntimeAvailable("extension").Return(true)
	contexts.EXPECT().IssueContext("extension").Return(binding, nil)
	runtime.EXPECT().ObserveLifecycle(t.Context(), "extension", "settled", binding, gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, _ extension.Context, event Event) (bool, error) {
			assert.True(t, event.Settled)
			assert.Equal(t, "run", event.Agent.RunID)
			return false, nil
		},
	)

	// Act by observing Host settlement.
	err := service.ObserveSettled(t.Context(), "run")

	// Assert the settlement observer completes.
	require.NoError(t, err)
}

// TestServiceContinuesAfterOrdinaryObserverError verifies ordered issue delivery and continuation.
func TestServiceContinuesAfterOrdinaryObserverError(t *testing.T) {
	t.Parallel()

	// Arrange three observers where the first returns an ordinary error.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	issues := NewMockIssueDelivery(controller)
	service := New(runtime, contexts)
	service.BindIssueDelivery(issues)
	registrations := []startup.AcceptedRegistration{
		{
			ID:       "first",
			Path:     "/first",
			Tools:    nil,
			Handlers: []startup.AcceptedHandler{{ID: "broken", Kind: startup.RawHandlerKindAgentStart}},
		},
		{
			ID:       "second",
			Path:     "/second",
			Tools:    nil,
			Handlers: []startup.AcceptedHandler{{ID: "later", Kind: startup.RawHandlerKindAgentStart}},
		},
	}
	service.CommitLifecycleHandlers(registrations)
	firstBinding := extension.Context{
		ID:                "one",
		ExtensionID:       "first",
		RuntimeInstanceID: "runtime-one",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	secondBinding := extension.Context{
		ID:                "two",
		ExtensionID:       "second",
		RuntimeInstanceID: "runtime-two",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	order := make([]string, 0, 3)
	runtime.EXPECT().HandlerRuntimeAvailable("first").Return(true)
	runtime.EXPECT().HandlerRuntimeAvailable("second").Return(true)
	contexts.EXPECT().IssueContext("first").Return(firstBinding, nil)
	runtime.EXPECT().ObserveLifecycle(t.Context(), "first", "broken", firstBinding, gomock.Any()).DoAndReturn(
		func(context.Context, string, string, extension.Context, Event) (bool, error) {
			order = append(order, "first")
			return false, errors.New("observer failed: exact cause")
		},
	)
	issues.EXPECT().
		DeliverExtensionIssue(t.Context(), gomock.Any()).
		DoAndReturn(func(_ context.Context, issue Issue) error {
			order = append(order, "issue")
			assert.Equal(t, "first", issue.ExtensionID)
			assert.Equal(t, "broken", issue.HandlerID)
			assert.Equal(t, IssueCodeObserverError, issue.Code)
			assert.EqualError(t, issue.Err, "observer failed: exact cause")
			return nil
		})
	contexts.EXPECT().IssueContext("second").Return(secondBinding, nil)
	runtime.EXPECT().ObserveLifecycle(t.Context(), "second", "later", secondBinding, gomock.Any()).DoAndReturn(
		func(context.Context, string, string, extension.Context, Event) (bool, error) {
			order = append(order, "second")
			return false, nil
		},
	)

	// Act by observing one event.
	err := service.Observe(t.Context(), lifecycleEvent(run.EventAgentStart))

	// Assert the issue is nonterminal and later observers still run in order.
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "issue", "second"}, order)
}

// TestServiceTreatsIndependentObserverCancellationAsIssue verifies remote cancellation does not cancel Agent Core.
func TestServiceTreatsIndependentObserverCancellationAsIssue(t *testing.T) {
	t.Parallel()

	// Arrange an active caller, one independently canceled observer, and one later observer.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	issues := NewMockIssueDelivery(controller)
	service := New(runtime, contexts)
	service.BindIssueDelivery(issues)
	service.CommitLifecycleHandlers([]startup.AcceptedRegistration{
		{
			ID:       "first",
			Path:     "/first",
			Tools:    nil,
			Handlers: []startup.AcceptedHandler{{ID: "canceled", Kind: startup.RawHandlerKindAgentStart}},
		},
		{
			ID:       "second",
			Path:     "/second",
			Tools:    nil,
			Handlers: []startup.AcceptedHandler{{ID: "later", Kind: startup.RawHandlerKindAgentStart}},
		},
	})
	firstBinding := extension.Context{
		ID:                "one",
		ExtensionID:       "first",
		RuntimeInstanceID: "runtime-one",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	secondBinding := extension.Context{
		ID:                "two",
		ExtensionID:       "second",
		RuntimeInstanceID: "runtime-two",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	observerErr := fmt.Errorf("observer stopped its own work: %w", context.Canceled)
	order := make([]string, 0, 3)
	runtime.EXPECT().HandlerRuntimeAvailable("first").Return(true)
	contexts.EXPECT().IssueContext("first").Return(firstBinding, nil)
	runtime.EXPECT().ObserveLifecycle(t.Context(), "first", "canceled", firstBinding, gomock.Any()).DoAndReturn(
		func(context.Context, string, string, extension.Context, Event) (bool, error) {
			order = append(order, "first")
			return false, observerErr
		},
	)
	issues.EXPECT().
		DeliverExtensionIssue(t.Context(), gomock.Any()).
		DoAndReturn(func(_ context.Context, issue Issue) error {
			order = append(order, "issue")
			assert.Equal(t, "first", issue.ExtensionID)
			assert.Equal(t, "canceled", issue.HandlerID)
			assert.Equal(t, IssueCodeObserverError, issue.Code)
			assert.ErrorIs(t, issue.Err, context.Canceled)
			assert.EqualError(t, issue.Err, "observer stopped its own work: context canceled")
			return nil
		})
	runtime.EXPECT().HandlerRuntimeAvailable("second").Return(true)
	contexts.EXPECT().IssueContext("second").Return(secondBinding, nil)
	runtime.EXPECT().ObserveLifecycle(t.Context(), "second", "later", secondBinding, gomock.Any()).DoAndReturn(
		func(context.Context, string, string, extension.Context, Event) (bool, error) {
			order = append(order, "second")
			return false, nil
		},
	)

	// Act through one synchronous observer chain.
	err := service.Observe(t.Context(), lifecycleEvent(run.EventAgentStart))

	// Assert successful issue delivery makes independent cancellation nonterminal.
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "issue", "second"}, order)
}

// TestServicePreservesCallerCancellationDuringInvocation verifies the owning context stops observation.
func TestServicePreservesCallerCancellationDuringInvocation(t *testing.T) {
	t.Parallel()

	// Arrange one observer that observes and returns caller cancellation.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	issues := NewMockIssueDelivery(controller)
	service := New(runtime, contexts)
	service.BindIssueDelivery(issues)
	service.CommitLifecycleHandlers([]startup.AcceptedRegistration{{
		ID: "extension", Path: "/extension", Tools: nil,
		Handlers: []startup.AcceptedHandler{{ID: "observer", Kind: startup.RawHandlerKindAgentStart}},
	}})
	binding := extension.Context{
		ID:                "context",
		ExtensionID:       "extension",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	ctx, cancel := context.WithCancel(t.Context())
	runtime.EXPECT().HandlerRuntimeAvailable("extension").Return(true)
	contexts.EXPECT().IssueContext("extension").Return(binding, nil)
	runtime.EXPECT().ObserveLifecycle(ctx, "extension", "observer", binding, gomock.Any()).DoAndReturn(
		func(context.Context, string, string, extension.Context, Event) (bool, error) {
			cancel()
			return false, context.Canceled
		},
	)

	// Act while the runtime observes caller cancellation during invocation.
	err := service.Observe(ctx, lifecycleEvent(run.EventAgentStart))

	// Assert caller cancellation remains terminal and no observer issue is emitted.
	require.ErrorIs(t, err, context.Canceled)
}

// TestServiceSkipsContextRaceAfterRuntimeExit verifies availability ownership covers delivery-time binding races.
func TestServiceSkipsContextRaceAfterRuntimeExit(t *testing.T) {
	t.Parallel()

	// Arrange one runtime that exits between availability selection and context issuance.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	service := New(runtime, contexts)
	service.CommitLifecycleHandlers([]startup.AcceptedRegistration{{
		ID: "extension", Path: "/extension", Tools: nil,
		Handlers: []startup.AcceptedHandler{{ID: "observer", Kind: startup.RawHandlerKindAgentStart}},
	}})
	runtime.EXPECT().HandlerRuntimeAvailable("extension").Return(true)
	contexts.EXPECT().
		IssueContext("extension").
		Return(extension.Context{}, errors.New("runtime or active session is unavailable"))
	runtime.EXPECT().HandlerRuntimeAvailable("extension").Return(false)

	// Act by delivering one event across the runtime-exit race.
	err := service.Observe(t.Context(), lifecycleEvent(run.EventAgentStart))

	// Assert extension runtime ownership absorbs the unavailable observer without failing Agent Core.
	require.NoError(t, err)
}

// TestServiceContinuesAfterRuntimeUnavailability verifies runtime ownership disables only one observer source.
func TestServiceContinuesAfterRuntimeUnavailability(t *testing.T) {
	t.Parallel()

	// Arrange two available observer registrations where the first runtime fails during invocation.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	service := New(runtime, contexts)
	service.CommitLifecycleHandlers([]startup.AcceptedRegistration{
		{
			ID:       "failed",
			Path:     "/failed",
			Tools:    nil,
			Handlers: []startup.AcceptedHandler{{ID: "first", Kind: startup.RawHandlerKindAgentStart}},
		},
		{
			ID:       "healthy",
			Path:     "/healthy",
			Tools:    nil,
			Handlers: []startup.AcceptedHandler{{ID: "second", Kind: startup.RawHandlerKindAgentStart}},
		},
	})
	failedBinding := extension.Context{
		ID:                "one",
		ExtensionID:       "failed",
		RuntimeInstanceID: "runtime-one",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	healthyBinding := extension.Context{
		ID:                "two",
		ExtensionID:       "healthy",
		RuntimeInstanceID: "runtime-two",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	order := make([]string, 0, 2)
	runtime.EXPECT().HandlerRuntimeAvailable("failed").Return(true)
	contexts.EXPECT().IssueContext("failed").Return(failedBinding, nil)
	runtime.EXPECT().ObserveLifecycle(t.Context(), "failed", "first", failedBinding, gomock.Any()).DoAndReturn(
		func(context.Context, string, string, extension.Context, Event) (bool, error) {
			order = append(order, "failed")
			return true, errors.New("runtime process exited: complete cause")
		},
	)
	runtime.EXPECT().HandlerRuntimeAvailable("healthy").Return(true)
	contexts.EXPECT().IssueContext("healthy").Return(healthyBinding, nil)
	runtime.EXPECT().ObserveLifecycle(t.Context(), "healthy", "second", healthyBinding, gomock.Any()).DoAndReturn(
		func(context.Context, string, string, extension.Context, Event) (bool, error) {
			order = append(order, "healthy")
			return false, nil
		},
	)

	// Act by delivering one event through the observer chain.
	err := service.Observe(t.Context(), lifecycleEvent(run.EventAgentStart))

	// Assert runtime reporting owns the failure and the later observer still completes.
	require.NoError(t, err)
	assert.Equal(t, []string{"failed", "healthy"}, order)
}

// TestServiceCancellationStopsObserverAdmission verifies caller cancellation settles the synchronous chain.
func TestServiceCancellationStopsObserverAdmission(t *testing.T) {
	t.Parallel()

	// Arrange one accepted observer and an already canceled delivery context.
	controller := gomock.NewController(t)
	service := New(NewMockRuntime(controller), NewMockContextIssuer(controller))
	service.CommitLifecycleHandlers([]startup.AcceptedRegistration{{
		ID: "extension", Path: "/extension", Tools: nil,
		Handlers: []startup.AcceptedHandler{{ID: "observer", Kind: startup.RawHandlerKindAgentStart}},
	}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act by attempting lifecycle delivery after caller cancellation.
	err := service.Observe(ctx, lifecycleEvent(run.EventAgentStart))

	// Assert cancellation is preserved and no observer work starts.
	require.ErrorIs(t, err, context.Canceled)
}

// TestServiceReturnsObserverAndIssueDeliveryCauses verifies failed issue publication is diagnosable.
func TestServiceReturnsObserverAndIssueDeliveryCauses(t *testing.T) {
	t.Parallel()

	// Arrange one ordinary observer error and one issue-delivery error.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	issues := NewMockIssueDelivery(controller)
	service := New(runtime, contexts)
	service.BindIssueDelivery(issues)
	service.CommitLifecycleHandlers(
		[]startup.AcceptedRegistration{
			{
				ID:       "extension",
				Path:     "/extension",
				Tools:    nil,
				Handlers: []startup.AcceptedHandler{{ID: "observer", Kind: startup.RawHandlerKindAgentStart}},
			},
		},
	)
	binding := extension.Context{
		ID:                "context",
		ExtensionID:       "extension",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	observerErr := fmt.Errorf("observer exact cause: %w", context.Canceled)
	deliveryErr := errors.New("issue delivery exact cause")
	runtime.EXPECT().HandlerRuntimeAvailable("extension").Return(true)
	contexts.EXPECT().IssueContext("extension").Return(binding, nil)
	runtime.EXPECT().
		ObserveLifecycle(t.Context(), "extension", "observer", binding, gomock.Any()).
		Return(false, observerErr)
	issues.EXPECT().DeliverExtensionIssue(t.Context(), gomock.Any()).Return(deliveryErr)

	// Act by observing one event.
	err := service.Observe(t.Context(), lifecycleEvent(run.EventAgentStart))

	// Assert both complete causes remain discoverable.
	require.ErrorIs(t, err, observerErr)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, deliveryErr)
}
