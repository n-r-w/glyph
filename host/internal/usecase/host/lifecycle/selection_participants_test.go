//go:build !integration

package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestSelectionObserversCaptureEligibilityBeforeDispatch verifies selected model-observer loss fails after reasoning observation.
func TestSelectionObserversCaptureEligibilityBeforeDispatch(t *testing.T) {
	t.Parallel()
	// Arrange three registrations whose initial eligibility is captured before either observer group starts.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	delivery := NewMockIssueDelivery(controller)
	service := New(runtime, contexts)
	service.BindIssueDelivery(delivery)
	service.CommitLifecycleHandlers([]startup.AcceptedRegistration{
		{ID: "reasoning", Path: "/reasoning", Tools: nil, Handlers: []startup.AcceptedHandler{{
			ID: "first", Kind: startup.RawHandlerKindReasoningSelectionObserver,
		}}},
		{ID: "model", Path: "/model", Tools: nil, Handlers: []startup.AcceptedHandler{
			{ID: "lost", Kind: startup.RawHandlerKindModelSelectionObserver},
			{ID: "later", Kind: startup.RawHandlerKindModelSelectionObserver},
		}},
		{ID: "initially-unavailable", Path: "/unavailable", Tools: nil, Handlers: []startup.AcceptedHandler{{
			ID: "excluded", Kind: startup.RawHandlerKindModelSelectionObserver,
		}}},
	})
	cause := errors.New("selected model observer became unavailable: complete cause")
	change := modelselection.SelectionChange{
		Preceding: model.Selection{Provider: "old", Model: "old", ReasoningChoice: model.ReasoningChoiceLow},
		Committed: model.Selection{Provider: "new", Model: "new", ReasoningChoice: model.ReasoningChoiceHigh},
	}
	gomock.InOrder(
		runtime.EXPECT().HandlerRuntimeAvailable("reasoning").Return(true),
		runtime.EXPECT().HandlerRuntimeAvailable("model").Return(true),
		runtime.EXPECT().HandlerRuntimeAvailable("initially-unavailable").Return(false),
		contexts.EXPECT().IssueContext("reasoning").Return(extension.Context{}, nil),
		runtime.EXPECT().
			ObserveLifecycle(gomock.Any(), "reasoning", "first", gomock.Any(), gomock.Any()).
			Return(false, nil),
		contexts.EXPECT().IssueContext("model").Return(extension.Context{}, nil),
		runtime.EXPECT().
			ObserveLifecycle(gomock.Any(), "model", "lost", gomock.Any(), gomock.Any()).
			Return(true, cause),
		delivery.EXPECT().DeliverExtensionIssue(gomock.Any(), gomock.Any()).Return(nil),
	)

	// Act across reasoning-to-model observation with a selected participant lost after eligibility capture.
	issues, failure := service.ObserveSelection(t.Context(), change)

	// Assert the selected loss stops all later observers and retains its complete cause.
	require.ErrorIs(t, failure, cause)
	require.Len(t, issues, 1)
	require.Equal(t, "model", issues[0].ExtensionID)
	require.Equal(t, "lost", issues[0].HandlerID)
	require.ErrorIs(t, issues[0].Err, cause)
}

// TestSelectionObserverFailureWinsOwningCancellation verifies an independent post-commit failure remains current.
func TestSelectionObserverFailureWinsOwningCancellation(t *testing.T) {
	t.Parallel()
	// Arrange an observer that reports an independent failure while the owning operation is canceled.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	delivery := NewMockIssueDelivery(controller)
	service := New(runtime, contexts)
	service.BindIssueDelivery(delivery)
	service.CommitLifecycleHandlers([]startup.AcceptedRegistration{{
		ID: "extension", Path: "/extension", Tools: nil, Handlers: []startup.AcceptedHandler{
			{ID: "first", Kind: startup.RawHandlerKindReasoningSelectionObserver},
			{ID: "later", Kind: startup.RawHandlerKindReasoningSelectionObserver},
		},
	}})
	ctx, cancel := context.WithCancel(t.Context())
	cause := errors.New("independent post-commit observer failure")
	runtime.EXPECT().HandlerRuntimeAvailable("extension").Return(true)
	contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil)
	runtime.EXPECT().ObserveLifecycle(gomock.Any(), "extension", "first", gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, string, string, extension.Context, Event) (bool, error) {
			cancel()
			return false, cause
		})
	delivery.EXPECT().DeliverExtensionIssue(gomock.Any(), gomock.Any()).Return(nil)

	// Act through committed reasoning observation.
	_, failure := service.ObserveSelection(ctx, modelselection.SelectionChange{
		Preceding: model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow},
		Committed: model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh},
	})

	// Assert both current causes remain present with explicit failure meaning.
	require.ErrorIs(t, failure, cause)
	require.ErrorIs(t, failure, context.Canceled)
	classified, present := errors.AsType[interface {
		error
		IsExtensionFailure() bool
	}](failure)
	require.True(t, present)
	require.True(t, classified.IsExtensionFailure())
}
