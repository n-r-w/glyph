//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// newObservedStreamHarness creates a stream with deterministic receive and send observation hooks.
func newObservedStreamHarness(
	t *testing.T,
	ctx context.Context,
	onReceive func(int),
	onSend func(*programmaticv1.OpenResponse),
) *streamHarness {
	t.Helper()
	stream := NewMockOpenStream(gomock.NewController(t))
	harness := &streamHarness{
		stream: stream, requests: make(chan *programmaticv1.OpenRequest, 64),
		responses: make(chan *programmaticv1.OpenResponse, 128), closeOnce: sync.Once{},
	}
	receiveCall := 0
	stream.EXPECT().Context().Return(ctx).AnyTimes()
	stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
		receiveCall++
		if onReceive != nil {
			onReceive(receiveCall)
		}
		request, open := <-harness.requests
		if !open {
			return nil, io.EOF
		}
		return request, nil
	}).AnyTimes()
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *programmaticv1.OpenResponse) error {
		if onSend != nil {
			onSend(response)
		}
		harness.responses <- response
		return nil
	}).AnyTimes()
	return harness
}

// TestCancellationDoesNotAffectTargetBeforeRunning verifies preparation has no target side effect.
func TestCancellationDoesNotAffectTargetBeforeRunning(t *testing.T) {
	t.Parallel()

	// Arrange running target work and block cancellation Accepted delivery.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	target := operation.NewMockPrepared[AgentEvent, Response](controller)
	targetContext := make(chan context.Context, 1)
	target.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ operation.Reporter[AgentEvent]) operation.Outcome[Response] {
			targetContext <- ctx
			<-ctx.Done()
			return operation.Canceled[Response]()
		},
	)
	target.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(target, nil)
	cancellationAcceptedStarted := make(chan struct{})
	releaseCancellationAccepted := make(chan struct{})
	var acceptedOnce sync.Once
	stream := newObservedStreamHarness(t, t.Context(), nil, func(response *programmaticv1.OpenResponse) {
		if response.GetOperationId() == "cancel" && response.GetEvent().HasAccepted() {
			acceptedOnce.Do(func() { close(cancellationAcceptedStarted) })
			<-releaseCancellationAccepted
		}
	})
	service := New(t.Context(), host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream.stream) }()
	stream.requests <- testRequest("target", func(request *programmaticv1.ControllerRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	})
	for {
		response := <-stream.responses
		if response.GetOperationId() == "target" && response.GetEvent().HasRunning() {
			break
		}
	}
	ctx := <-targetContext
	stream.requests <- testRequest("cancel", func(request *programmaticv1.ControllerRequest) {
		payload := new(operationv1.CancelOperation)
		payload.SetTargetOperationId("target")
		request.SetCancel(payload)
	})
	<-cancellationAcceptedStarted

	// Act by observing target state before cancellation Running can be queued.
	prematureCancellation := ctx.Err()
	close(releaseCancellationAccepted)
	for {
		response := <-stream.responses
		if response.GetOperationId() == "cancel" && response.GetEvent().HasCompleted() {
			break
		}
	}
	stream.closeSend()
	require.NoError(t, <-result)

	// Assert preparation did not cancel target work before cancellation Running.
	assert.NoError(t, prematureCancellation)
}

// TestCancellationUsesTerminalStateCompletedBeforeExecution verifies the admitted target snapshot survives Owner removal.
func TestCancellationUsesTerminalStateCompletedBeforeExecution(t *testing.T) {
	t.Parallel()

	// Arrange a running target and block cancellation Accepted delivery after admission.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	target := operation.NewMockPrepared[AgentEvent, Response](controller)
	releaseTarget := make(chan struct{})
	targetReturned := make(chan struct{})
	target.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, operation.Reporter[AgentEvent]) operation.Outcome[Response] {
			<-releaseTarget
			close(targetReturned)
			return operation.Completed(testResponse(ResponseMessages))
		},
	)
	target.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(target, nil)
	cancellationAcceptedStarted := make(chan struct{})
	releaseCancellationAccepted := make(chan struct{})
	var acceptedOnce sync.Once
	stream := newObservedStreamHarness(t, t.Context(), nil, func(response *programmaticv1.OpenResponse) {
		if response.GetOperationId() == "cancel" && response.GetEvent().HasAccepted() {
			acceptedOnce.Do(func() { close(cancellationAcceptedStarted) })
			<-releaseCancellationAccepted
		}
	})
	service := New(t.Context(), host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream.stream) }()
	stream.requests <- testRequest("target", func(request *programmaticv1.ControllerRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	})
	for {
		response := <-stream.responses
		if response.GetOperationId() == "target" && response.GetEvent().HasRunning() {
			break
		}
	}
	stream.requests <- testRequest("cancel", func(request *programmaticv1.ControllerRequest) {
		payload := new(operationv1.CancelOperation)
		payload.SetTargetOperationId("target")
		request.SetCancel(payload)
	})
	<-cancellationAcceptedStarted

	// Act by completing target work before the cancellation operation can execute.
	close(releaseTarget)
	<-targetReturned
	close(releaseCancellationAccepted)
	var targetState operationv1.TerminalState
	cancellationCompleted := false
	for !cancellationCompleted {
		select {
		case response := <-stream.responses:
			if response.GetOperationId() == "cancel" && response.GetEvent().HasCompleted() {
				targetState = response.GetEvent().GetCompleted().GetCancel().GetTargetState()
				cancellationCompleted = true
			}
		case err := <-result:
			require.NoError(t, err)
			require.FailNow(t, "connection closed before cancellation completion")
		case <-time.After(time.Second):
			require.FailNow(t, "cancellation did not complete after target work")
		}
	}
	stream.closeSend()
	require.NoError(t, <-result)

	// Assert cancellation reports the target's actual terminal state.
	assert.Equal(t, operationv1.TerminalState_TERMINAL_STATE_COMPLETED, targetState)
}

// TestCancellationAdmitsTargetUntilTerminalDelivery verifies admission survives blocked terminal delivery.
func TestCancellationAdmitsTargetUntilTerminalDelivery(t *testing.T) {
	t.Parallel()

	// Arrange completed target work whose terminal Send remains blocked.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	target := operation.NewMockPrepared[AgentEvent, Response](controller)
	target.EXPECT().Run(gomock.Any(), gomock.Any()).Return(operation.Completed(testResponse(ResponseMessages)))
	target.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(target, nil)
	targetTerminalStarted := make(chan struct{})
	releaseTargetTerminal := make(chan struct{})
	cancellationProcessed := make(chan struct{})
	var terminalOnce sync.Once
	stream := newObservedStreamHarness(t, t.Context(), func(call int) {
		if call == 3 {
			close(cancellationProcessed)
		}
	}, func(response *programmaticv1.OpenResponse) {
		if response.GetOperationId() == "target" && response.GetEvent().HasCompleted() {
			terminalOnce.Do(func() { close(targetTerminalStarted) })
			<-releaseTargetTerminal
		}
	})
	service := New(t.Context(), host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream.stream) }()
	stream.requests <- testRequest("target", func(request *programmaticv1.ControllerRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	})
	<-targetTerminalStarted

	// Act by admitting cancellation before terminal Send can return.
	stream.requests <- testRequest("cancel", func(request *programmaticv1.ControllerRequest) {
		payload := new(operationv1.CancelOperation)
		payload.SetTargetOperationId("target")
		request.SetCancel(payload)
	})
	<-cancellationProcessed
	close(releaseTargetTerminal)
	order := make([]string, 0, 4)
	var targetState operationv1.TerminalState
	for {
		response := <-stream.responses
		switch {
		case response.GetOperationId() == "target" && response.GetEvent().HasCompleted():
			order = append(order, "target completed")
		case response.GetOperationId() == "cancel" && response.GetEvent().HasAccepted():
			order = append(order, "cancel accepted")
		case response.GetOperationId() == "cancel" && response.GetEvent().HasRunning():
			order = append(order, "cancel running")
		case response.GetOperationId() == "cancel" && response.GetEvent().HasCompleted():
			order = append(order, "cancel completed")
			targetState = response.GetEvent().GetCompleted().GetCancel().GetTargetState()
		case response.GetOperationId() == "cancel" && response.GetEvent().HasRejected():
			order = append(order, "cancel rejected")
		}
		if len(order) > 1 && (order[len(order)-1] == "cancel completed" || order[len(order)-1] == "cancel rejected") {
			break
		}
	}
	stream.closeSend()
	require.NoError(t, <-result)

	// Assert target delivery precedes cancellation completion with the delivered target state.
	assert.Equal(t, []string{"target completed", "cancel accepted", "cancel running", "cancel completed"}, order)
	assert.Equal(t, operationv1.TerminalState_TERMINAL_STATE_COMPLETED, targetState)
}

// TestTerminalDeliveryFailureDoesNotCompleteCancellation verifies failed Send publishes no delivered target state.
func TestTerminalDeliveryFailureDoesNotCompleteCancellation(t *testing.T) {
	t.Parallel()

	// Arrange completed target work whose terminal Send fails after cancellation admission.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	target := operation.NewMockPrepared[AgentEvent, Response](controller)
	target.EXPECT().Run(gomock.Any(), gomock.Any()).Return(operation.Completed(testResponse(ResponseMessages)))
	target.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(target, nil)
	stream := NewMockOpenStream(controller)
	requests := make(chan *programmaticv1.OpenRequest, 2)
	targetTerminalStarted := make(chan struct{})
	releaseTargetTerminal := make(chan struct{})
	cancellationProcessed := make(chan struct{})
	delivered := make([]*programmaticv1.OpenResponse, 0, 2)
	receiveCall := 0
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
		receiveCall++
		if receiveCall == 3 {
			close(cancellationProcessed)
		}
		request, open := <-requests
		if !open {
			return nil, io.EOF
		}
		return request, nil
	}).AnyTimes()
	sendErr := errors.New("target terminal Send failed")
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *programmaticv1.OpenResponse) error {
		if response.GetOperationId() == "target" && response.GetEvent().HasCompleted() {
			close(targetTerminalStarted)
			<-releaseTargetTerminal
			return sendErr
		}
		delivered = append(delivered, response)
		return nil
	}).AnyTimes()
	service := New(t.Context(), host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream) }()
	requests <- testRequest("target", func(request *programmaticv1.ControllerRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	})
	<-targetTerminalStarted
	requests <- testRequest("cancel", func(request *programmaticv1.ControllerRequest) {
		payload := new(operationv1.CancelOperation)
		payload.SetTargetOperationId("target")
		request.SetCancel(payload)
	})
	<-cancellationProcessed

	// Act by failing target terminal delivery.
	close(releaseTargetTerminal)
	err := <-result
	close(requests)

	// Assert connection failure joins ownership without publishing target or cancellation completion.
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))
	for _, response := range delivered {
		assert.False(t, response.GetOperationId() == "target" && response.GetEvent().HasCompleted())
		assert.False(t, response.GetOperationId() == "cancel" && response.GetEvent().HasCompleted())
	}
}

// TestDuplicateCancellationIdentifierPrecedesTargetAdmission verifies Owner identity validation has priority.
func TestDuplicateCancellationIdentifierPrecedesTargetAdmission(t *testing.T) {
	t.Parallel()

	// Arrange an active operation whose identifier is reused by a cancellation request.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	active := operation.NewMockPrepared[AgentEvent, Response](controller)
	releaseRun := make(chan struct{})
	active.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, operation.Reporter[AgentEvent]) operation.Outcome[Response] {
			<-releaseRun
			return operation.Completed(testResponse(ResponseMessages))
		},
	)
	active.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(active, nil)
	stream := newStreamHarness(t, t.Context())
	service := New(t.Context(), host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream.stream) }()
	stream.requests <- testRequest("duplicate", func(request *programmaticv1.ControllerRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	})
	for {
		response := <-stream.responses
		if response.GetOperationId() == "duplicate" && response.GetEvent().HasRunning() {
			break
		}
	}

	// Act by reusing the active identifier for cancellation of an inactive target.
	stream.requests <- testRequest("duplicate", func(request *programmaticv1.ControllerRequest) {
		payload := new(operationv1.CancelOperation)
		payload.SetTargetOperationId("inactive")
		request.SetCancel(payload)
	})
	var rejectionCode string
	for {
		response := <-stream.responses
		if response.GetOperationId() == "duplicate" && response.GetEvent().HasRejected() {
			rejectionCode = response.GetEvent().GetRejected().GetCode()
			break
		}
	}

	// Assert duplicate ownership is rejected before cancellation target admission.
	assert.Equal(t, RejectionCodeOperationIDInUse, rejectionCode)
	close(releaseRun)
	stream.closeSend()
	require.NoError(t, <-result)
}

// TestAcceptedFailureRemovesPreparedRegistryTarget verifies failed startup cleans metadata and waiters.
func TestAcceptedFailureRemovesPreparedRegistryTarget(t *testing.T) {
	t.Parallel()

	// Arrange successful preparation followed by a blocked Accepted send that fails.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	prepared := operation.NewMockPrepared[AgentEvent, Response](controller)
	prepared.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(prepared, nil)
	registry := newTargetRegistry()
	sendErr := errors.New("Accepted delivery failed")
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	writer := operation.NewWriter(func(*programmaticv1.OpenResponse) error {
		close(sendStarted)
		<-releaseSend
		return sendErr
	})
	delivery := &streamDelivery{
		context: t.Context(), writer: writer, registry: registry, fail: func(error) {},
	}
	owner := operation.NewOwner[AgentEvent, Response](t.Context(), delivery)
	writerResult := make(chan error, 1)
	go func() { writerResult <- writer.Run(t.Context()) }()
	stream := NewMockOpenStream(controller)
	finishReceive := make(chan struct{})
	request := testRequest("target", func(request *programmaticv1.ControllerRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	})
	gomock.InOrder(
		stream.EXPECT().Recv().Return(request, nil),
		stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
			<-finishReceive
			return nil, io.EOF
		}),
	)
	service := New(t.Context(), host)
	receiveResult := make(chan error, 1)
	closing := new(localClosingState)
	go func() { receiveResult <- service.receive(t.Context(), stream, owner, delivery, registry, closing) }()
	<-sendStarted
	target, active := registry.active("target")
	require.True(t, active)
	cancellation := &cancellationPrepared{owner: owner, targetID: "target", target: target}
	cancellationResult := make(chan operation.Outcome[Response], 1)
	go func() {
		var reporter operation.Reporter[AgentEvent]
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
	assert.Error(t, outcome.Err())
	assert.False(t, targetActive)
	assert.ErrorIs(t, <-writerResult, sendErr)
	assert.ErrorIs(t, <-receiveResult, io.EOF)
}

// TestCancellationCompletesAfterTargetTerminalOrder verifies targeted cancellation ordering.
func TestCancellationCompletesAfterTargetTerminalOrder(t *testing.T) {
	t.Parallel()

	// Arrange one operation that stops only after its context is canceled.
	controller := gomock.NewController(t)
	host := NewMockHostSession(controller)
	target := operation.NewMockPrepared[AgentEvent, Response](controller)
	target.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ operation.Reporter[AgentEvent]) operation.Outcome[Response] {
			<-ctx.Done()
			return operation.Canceled[Response]()
		},
	)
	target.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(target, nil)
	stream := newStreamHarness(t, t.Context())
	service := New(t.Context(), host)
	result := make(chan error, 1)
	go func() { result <- service.open(stream.stream) }()
	stream.requests <- testRequest("target", func(request *programmaticv1.ControllerRequest) {
		request.SetGetMessages(new(programmaticv1.GetMessages))
	})
	for {
		response := <-stream.responses
		if response.GetOperationId() == "target" && response.GetEvent().HasRunning() {
			break
		}
	}
	stream.requests <- testRequest("cancel", func(request *programmaticv1.ControllerRequest) {
		payload := new(operationv1.CancelOperation)
		payload.SetTargetOperationId("target")
		request.SetCancel(payload)
	})

	// Act by recording terminal event order.
	order := make([]string, 0, 2)
	var targetState operationv1.TerminalState
	for len(order) < 2 {
		select {
		case response := <-stream.responses:
			if response.GetOperationId() == "target" && response.GetEvent().HasCanceled() {
				order = append(order, "target")
			}
			if response.GetOperationId() == "cancel" && response.GetEvent().HasCompleted() {
				order = append(order, "cancel")
				targetState = response.GetEvent().GetCompleted().GetCancel().GetTargetState()
			}
		case <-time.After(time.Second):
			require.FailNow(t, "cancellation terminal events were not delivered")
		}
	}

	// Assert target terminal queue order precedes cancellation completion.
	assert.Equal(t, []string{"target", "cancel"}, order)
	assert.Equal(t, operationv1.TerminalState_TERMINAL_STATE_CANCELED, targetState)
	stream.closeSend()
	require.NoError(t, <-result)
}
