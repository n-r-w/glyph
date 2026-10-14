//go:build !integration

package uiv1

import (
	"context"
	"errors"
	"testing"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/internal/operation"
	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestUICanceledMessage checks that the SDK preserves received cancellation diagnostics in waits and notifications.
func TestUICanceledMessage(t *testing.T) {
	t.Parallel()
	// Arrange a received canceled event containing diagnostic wording.
	text := "failure words do not change canceled state\nUnicode: λ\n  "
	event := new(uipb.HostEvent)
	event.SetCanceled(operationpb.Canceled_builder{Message: new(text)}.Build())
	// Act through the Host event mapper and both actual SDK readers.
	mapped := mapHostEvent("canceled", event)
	_, cause := (&Operation{}).waitResult(mapped, true)
	notification, err := operationNotification(mapped)
	// Assert that the SDK retains complete received text and cancellation identity.
	require.NoError(t, err)
	require.Equal(t, text, mapped.Message)
	require.ErrorContains(t, cause, text)
	require.ErrorIs(t, cause, context.Canceled)
	require.ErrorContains(t, notification.OperationError(), text)
	require.ErrorIs(t, notification.OperationError(), context.Canceled)
}

// TestUIInitializeCanceledSource checks SDK initialization closure handling and failure-source retention.
func TestUIInitializeCanceledSource(t *testing.T) {
	t.Parallel()
	diagnosed := diagnostic.NewCurrentCause(context.Canceled, errors.New("UI initialization canceled diagnostics"))
	cases := []struct {
		name   string
		source error
		state  operation.TerminalState
	}{
		{name: "plain closure", source: context.Canceled, state: operation.TerminalStateCanceled},
		{name: "diagnostic failure", source: diagnosed, state: operation.TerminalStateFailed},
		{
			name:   "classified closure",
			source: Fail(failureCodeInternal, context.Canceled),
			state:  operation.TerminalStateCanceled,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange an initializer with a terminal source.
			mock := NewMockInitializeOperation(gomock.NewController(t))
			mock.EXPECT().Run(gomock.Any()).Return(nil, testCase.source)
			prepared := &initializePrepared{operation: mock}
			// Act through the SDK initializer outcome classifier.
			outcome := prepared.Run(t.Context(), operation.Reporter[struct{}]{})
			// Assert that SDK preserves the initializer's closure policy and complete failure source.
			require.Equal(t, testCase.state, outcome.State())
			if testCase.state == operation.TerminalStateCanceled {
				require.Nil(t, outcome.SourceError())
			} else {
				require.Same(t, testCase.source, outcome.SourceError())
			}
		})
	}
}
