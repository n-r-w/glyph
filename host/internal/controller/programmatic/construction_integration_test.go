//go:build integration

package programmatic

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestAcceptedFailureRemovesPreparedRegistryTarget verifies failed startup cleans metadata and waiters.
func TestAcceptedFailureRemovesPreparedRegistryTarget(t *testing.T) {
	t.Parallel()

	// Arrange successful preparation followed by a blocked Accepted send that fails.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	prepared := operationmock.NewMockOperationPrepared[OperationProgress, Response](controller)
	prepared.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(prepared, nil)
	registry := newTargetRegistry()
	sendErr := errors.New("Accepted delivery failed")
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	writer, constructionErr := operation.NewWriter(func(*programmaticv1.OpenResponse) error {
		close(sendStarted)
		<-releaseSend
		return sendErr
	})
	require.NoError(t, constructionErr)
	delivery := &streamDelivery{
		context: t.Context(), writer: writer, registry: registry, fail: func(error) {},
	}
	owner, constructionErr := operation.NewOwner[OperationProgress, Response](t.Context(), delivery)
	require.NoError(t, constructionErr)
	writerResult := make(chan error, 1)
	go func() { writerResult <- writer.Run(t.Context()) }()
	stream := NewMockOpenStream(controller)
	finishReceive := make(chan struct{})
	request := new(programmaticv1.OpenRequest)
	request.SetOperationId("target")
	payload := new(programmaticv1.ControllerRequest)
	payload.SetGetMessages(new(programmaticv1.GetMessages))
	request.SetRequest(payload)
	gomock.InOrder(
		stream.EXPECT().Recv().Return(request, nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
			<-finishReceive
			return nil, io.EOF
		}),
	)
	service := New(t.Context(), host, NewMockConnectionOutput(controller))
	receiveResult := make(chan error, 1)
	closing := new(localClosingState)
	go func() { receiveResult <- service.receive(t.Context(), stream, owner, delivery, registry, closing) }()
	<-sendStarted
	target, active := registry.active("target")
	require.True(t, active)
	cancellation := &cancellationPrepared{owner: owner, targetID: "target", target: target}
	cancellationResult := make(chan operation.Outcome[Response], 1)
	go func() {
		var reporter operation.Reporter[OperationProgress]
		cancellationResult <- cancellation.Run(t.Context(), reporter)
	}()

	// Act by failing Accepted delivery after cancellation captured the prepared target.
	close(releaseSend)
	var outcome operation.Outcome[Response]
	select {
	case outcome = <-cancellationResult:
	case <-time.After(time.Second):
		require.FailNow(t, "captured cancellation waiter did not resolve after Accepted failure")
	}
	owner.Wait()
	_, targetActive := registry.active("target")
	close(finishReceive)

	// Assert cleanup delegates Release, resolves the waiter, and removes registry metadata.
	assert.Equal(t, operation.TerminalStateFailed, outcome.State())
	require.Error(t, outcome.Err())
	assert.False(t, targetActive)
	require.ErrorIs(t, <-writerResult, sendErr)
	require.ErrorIs(t, <-receiveResult, io.EOF)
}
