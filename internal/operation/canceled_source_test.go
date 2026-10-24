//go:build !integration

package operation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestOwnerRetainsCanceledSourceBeforeReleaseAndTerminal checks canceled diagnostics during operation settlement.
func TestOwnerRetainsCanceledSourceBeforeReleaseAndTerminal(t *testing.T) {
	t.Parallel()
	// Arrange diagnosed cancellation with terminal acknowledgement held open.
	mocks := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](mocks)
	prepared := NewMockPrepared[string, string](mocks)
	source := errors.Join(context.Canceled, errors.New("recovered operation diagnostic"))
	ack := newAcknowledgement()
	owner, constructionErr := NewOwner(t.Context(), delivery)
	require.NoError(t, constructionErr)
	defer func() {
		ack.resolve(nil)
		owner.Close()
		owner.Wait()
	}()
	var releaseSource, terminalSource error
	var terminalOutcome Outcome[string]
	released, releasedBeforeTerminal := false, false
	delivered := make(chan struct{})
	delivery.EXPECT().Accepted("diagnosed-cancel").Return(resolvedAcknowledgement(nil), nil)
	delivery.EXPECT().Running("diagnosed-cancel").Return(nil)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).Return(CanceledWithSource[string](source))
	prepared.EXPECT().Release().Do(func() {
		releaseSource = owner.SourceErrors()
		released = true
	})
	delivery.EXPECT().Terminal("diagnosed-cancel", gomock.Any()).DoAndReturn(
		func(_ string, outcome Outcome[string]) (*Acknowledgement, error) {
			releasedBeforeTerminal = released
			terminalOutcome = outcome
			terminalSource = owner.SourceErrors()
			close(delivered)
			return ack, nil
		},
	)
	// Act through the terminal owner while holding its delivery acknowledgement.
	require.NoError(t, owner.Start("diagnosed-cancel", preparedBy(prepared)))
	<-delivered
	// Assert that the owner retains the source during release and terminal delivery.
	require.ErrorIs(t, releaseSource, source)
	require.True(t, releasedBeforeTerminal)
	require.Equal(t, TerminalStateCanceled, terminalOutcome.State())
	require.NoError(t, terminalOutcome.Err())
	require.Same(t, source, terminalOutcome.SourceError())
	require.ErrorIs(t, terminalSource, source)
	require.ErrorIs(t, owner.SourceErrors(), source)
	ack.resolve(nil)
	owner.Wait()
	// Assert that the owner releases source bookkeeping after terminal acknowledgement.
	require.NoError(t, owner.SourceErrors())
}
