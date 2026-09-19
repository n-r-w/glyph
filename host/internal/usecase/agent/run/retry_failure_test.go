//go:build !integration

package run

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// completeLogicalFailure is a test logical failure with a stable public category.
type completeLogicalFailure struct {
	// cause contains all contributing attempt and handler errors.
	cause error
}

// Error returns all contributing failure text.
func (failure *completeLogicalFailure) Error() string { return failure.cause.Error() }

// Unwrap exposes every contributing cause.
func (failure *completeLogicalFailure) Unwrap() error { return failure.cause }

// FailureCode returns the test logical category.
func (*completeLogicalFailure) FailureCode() string { return "EXTENSION_FAILED" }

// TestServiceRunLogicalFailurePublishesCompleteError verifies retained provider text cannot hide logical causes.
func TestServiceRunLogicalFailurePublishesCompleteError(t *testing.T) {
	t.Parallel()

	// Arrange terminal provider text and distinct attempt and handler causes.
	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	response := emptyModelResponse(model.OutcomeFailed)
	response.ErrorMessage = mo.Some("provider terminal detail")
	attemptErr := errors.New("first attempt failed")
	handlerErr := errors.New("retry handler failed")
	logicalErr := &completeLogicalFailure{cause: errors.Join(attemptErr, handlerErr)}
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, logicalErr))
	var messageEnd model.Response
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event agent.Event) error {
			if event.Type == agent.EventMessageEnd {
				messageEnd = event.Message.OrEmpty()
			}
			return nil
		},
	).AnyTimes()
	service := newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, tools, events,
	)

	// Act through the complete Core failure path.
	_, err := service.Run(t.Context(), runcontrol.Request{RunID: "run-complete-logical-error", UserText: "hi"})

	// Assert stored and delivered public errors include provider detail and all logical causes.
	require.ErrorIs(t, err, attemptErr)
	require.ErrorIs(t, err, handlerErr)
	history := service.History()
	require.Len(t, history, 2)
	for _, text := range []string{
		history[1].Model.OrEmpty().ErrorMessage.OrEmpty(), messageEnd.ErrorMessage.OrEmpty(),
	} {
		assert.Contains(t, text, "provider terminal detail")
		assert.Contains(t, text, attemptErr.Error())
		assert.Contains(t, text, handlerErr.Error())
	}
}

// TestServiceRunMixedCancellationPreservesIndependentFailure verifies caller cancellation does not hide another cause.
func TestServiceRunMixedCancellationPreservesIndependentFailure(t *testing.T) {
	t.Parallel()

	// Arrange provider execution that acquires an independent failure while caller cancellation also arrives.
	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	ctx, cancel := context.WithCancel(t.Context())
	independent := errors.New("provider connection failed independently")
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, ModelRequest, StreamHandler) error {
			cancel()
			return errors.Join(context.Canceled, independent)
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh, provider, tools, events,
	)

	// Act through the logical Core run.
	_, err := service.Run(ctx, runcontrol.Request{RunID: "run-mixed-cancellation", UserText: "hi"})

	// Assert the independent cause produces a failed response rather than a pure cancellation outcome.
	require.ErrorIs(t, err, independent)
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, model.OutcomeFailed, history[1].Model.OrEmpty().Outcome.MustGet())
}
