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
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestOrdinaryObserverFailureStopsChain verifies selected failures retain all causes and stop later observers.
func TestOrdinaryObserverFailureStopsChain(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"ordinary", "independent cancellation", "mixed cancellation", "classified cancellation", "context", "unavailable", "settled", "issue delivery"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			// Arrange two selected observers and one independently failing first participant.
			controller := gomock.NewController(t)
			runtime := NewMockRuntime(controller)
			contexts := NewMockContextIssuer(controller)
			issues := NewMockIssueDelivery(controller)
			service := New(runtime, contexts)
			service.BindIssueDelivery(issues)
			kind := startup.RawHandlerKindAgentStart
			if scenario == "settled" {
				kind = startup.RawHandlerKindAgentSettled
			}
			service.CommitLifecycleHandlers([]startup.AcceptedRegistration{
				{
					ID:       "first",
					Path:     "/first",
					Tools:    nil,
					Handlers: []startup.AcceptedHandler{{ID: "broken", Kind: kind}},
				},
				{
					ID:       "second",
					Path:     "/second",
					Tools:    nil,
					Handlers: []startup.AcceptedHandler{{ID: "later", Kind: kind}},
				},
			})
			cause := errors.New("ordinary observer complete original cause")
			if scenario == "independent cancellation" {
				cause = context.Canceled
			}
			if scenario == "classified cancellation" {
				cause = modelselection.NewExtensionFailure(context.Canceled)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			binding := extension.Context{
				ID:                "binding",
				ExtensionID:       "first",
				RuntimeInstanceID: "runtime",
				SessionID:         "session",
				WorkingDirectory:  "/cwd",
			}
			runtime.EXPECT().HandlerRuntimeAvailable("first").Return(true)
			runtime.EXPECT().HandlerRuntimeAvailable("second").Return(true)
			if scenario == "context" {
				contexts.EXPECT().IssueContext("first").Return(extension.Context{}, cause)
			} else {
				contexts.EXPECT().IssueContext("first").Return(binding, nil)
				runtime.EXPECT().ObserveLifecycle(ctx, "first", "broken", binding, gomock.Any()).DoAndReturn(
					func(context.Context, string, string, extension.Context, Event) (bool, error) {
						if scenario == "classified cancellation" {
							cancel()
						}
						if scenario == "mixed cancellation" {
							cancel()
							return false, errors.Join(cause, context.Canceled)
						}
						return scenario == "unavailable", cause
					})
			}
			var deliveryCause error
			if scenario == "issue delivery" {
				deliveryCause = errors.New("diagnostic delivery complete cause")
			}
			issues.EXPECT().DeliverExtensionIssue(ctx, gomock.Any()).Return(deliveryCause)

			// Act through the ordinary or settled lifecycle entry point.
			var err error
			if scenario == "settled" {
				err = service.ObserveSettled(ctx, "run")
			} else {
				err = service.Observe(ctx, lifecycleEvent(agent.EventAgentStart))
			}

			// Assert source identity and explicit failure survive without admitting the later observer.
			require.ErrorIs(t, err, cause)
			if deliveryCause != nil {
				require.ErrorIs(t, err, deliveryCause)
			}
			classified, ok := errors.AsType[*modelselection.SelectionError](err)
			require.True(t, ok)
			require.Equal(t, modelselection.ErrorCodeExtensionFailed, classified.Code)
		})
	}
}
