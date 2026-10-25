//go:build !integration

package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	"github.com/n-r-w/glyph/internal/errtree"
)

// TestOrdinaryObserversCaptureEligibilityBeforeDispatch verifies initial exclusion and later selected runtime loss.
func TestOrdinaryObserversCaptureEligibilityBeforeDispatch(t *testing.T) {
	t.Parallel()
	// Arrange initially unavailable, healthy, selected-lost, and forbidden later observers.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	issues := NewMockIssueDelivery(controller)
	service := New(runtime, contexts)
	service.BindIssueDelivery(issues)
	registrations := make([]startup.AcceptedRegistration, 0, 4)
	for _, id := range []string{"excluded", "healthy", "lost", "later"} {
		registrations = append(
			registrations,
			startup.AcceptedRegistration{
				ID:       id,
				Path:     "/" + id,
				Tools:    nil,
				Handlers: []startup.AcceptedHandler{{ID: id, Kind: startup.RawHandlerKindAgentStart}},
			},
		)
	}
	service.CommitLifecycleHandlers(registrations)
	binding := extension.Context{
		ID:                "binding",
		ExtensionID:       "healthy",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	gomock.InOrder(
		runtime.EXPECT().HandlerRuntimeAvailable("excluded").Return(false),
		runtime.EXPECT().HandlerRuntimeAvailable("healthy").Return(true),
		runtime.EXPECT().HandlerRuntimeAvailable("lost").Return(true),
		runtime.EXPECT().HandlerRuntimeAvailable("later").Return(true),
		contexts.EXPECT().IssueContext("healthy").Return(binding, nil),
		runtime.EXPECT().ObserveLifecycle(gomock.Any(), "healthy", "healthy", binding, gomock.Any()).Return(false, nil),
		contexts.EXPECT().IssueContext("lost").Return(binding, nil),
		runtime.EXPECT().ObserveLifecycle(gomock.Any(), "lost", "lost", binding, gomock.Any()).Return(true, nil),
		issues.EXPECT().DeliverExtensionIssue(gomock.Any(), gomock.Any()).Return(nil),
	)

	// Act after the full eligibility snapshot has been selected.
	err := service.Observe(t.Context(), lifecycleEvent(agent.EventAgentStart))

	// Assert a selected unavailable observer fails the chain rather than excluding it again.
	require.ErrorContains(t, err, "became unavailable")
}

// TestOrdinaryObserverOwningCancellationHasNoFailure verifies owning cancellation does not acquire extension meaning.
func TestOrdinaryObserverOwningCancellationHasNoFailure(t *testing.T) {
	t.Parallel()
	// Arrange an observer that returns cancellation only after its owning context is canceled.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	service := New(runtime, contexts)
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
		ID:                "binding",
		ExtensionID:       "extension",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/cwd",
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runtime.EXPECT().HandlerRuntimeAvailable("extension").Return(true)
	contexts.EXPECT().IssueContext("extension").Return(binding, nil)
	runtime.EXPECT().
		ObserveLifecycle(ctx, "extension", "observer", binding, gomock.Any()).
		DoAndReturn(func(context.Context, string, string, extension.Context, Event) (bool, error) {
			cancel()
			return false, context.Canceled
		})

	// Act while owning cancellation interrupts the selected observer.
	err := service.Observe(ctx, lifecycleEvent(agent.EventAgentStart))

	// Assert the original cancellation remains unclassified.
	require.ErrorIs(t, err, context.Canceled)
	require.True(
		t,
		errtree.IsCurrentCancellation(err, func(cause error) bool { return errors.Is(cause, context.Canceled) }),
	)
}
