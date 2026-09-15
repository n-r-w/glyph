package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const (
	// initializationCancellationOperationID identifies Host-requested initialization cancellation.
	initializationCancellationOperationID = "host-initialize-cancel"
	// unsuccessfulInitializationGracePeriod bounds initialization cancellation and startup-close cleanup together.
	unsuccessfulInitializationGracePeriod = time.Second
	// unsuccessfulInitializationTimeoutFormat preserves the public startup cleanup timeout diagnostic.
	unsuccessfulInitializationTimeoutFormat = "UI unsuccessful initialization cleanup timed out after %s: %w"
)

// initializationReceive carries one asynchronous stream receive result.
type initializationReceive struct {
	// response is the received UI stream message.
	response *uiv1.OpenResponse
	// err is the stream receive result.
	err error
}

// Initialize maps and runs startup through the normal writer and tracker.
func (c *Service) Initialize(ctx context.Context, initialization hostui.Initialization) error {
	request, err := mapInitializationFrame(c.buildInitialization(initialization))
	if err != nil {
		return err
	}
	if request.GetRequest().GetInitialize() == nil {
		return errors.New("initialize UI: initialization request is required")
	}
	request.SetOperationId(initializationOperationID)

	// graceContext coordinates every unsuccessful initialization wait without inheriting RPC cancellation.
	graceContext, cancelGrace := context.WithCancelCause(context.WithoutCancel(c.stream.Context()))
	// timeoutResult lets the caller join a timer callback that has already started.
	timeoutResult := make(chan bool, 1)
	// graceTimer remains nil until the first unsuccessful initialization signal.
	var graceTimer *time.Timer
	// startGrace starts the one shared grace at most once on the initialization owner goroutine.
	startGrace := func() {
		if graceTimer != nil {
			return
		}
		graceTimer = time.AfterFunc(unsuccessfulInitializationGracePeriod, func() {
			// Only grace expiry cancels the actual RPC and releases blocked raw transport calls.
			c.Cancel()
			cancelGrace(context.DeadlineExceeded)
			timeoutResult <- errors.Is(context.Cause(graceContext), context.DeadlineExceeded)
		})
	}
	// finishGrace stops a pending callback or joins an active callback before return.
	finishGrace := func() bool {
		if graceTimer == nil || graceTimer.Stop() {
			cancelGrace(context.Canceled)
			return false
		}
		timedOut := <-timeoutResult
		cancelGrace(context.Canceled)
		return timedOut
	}

	initializationErr := errors.Join(c.initialize(ctx, request, startGrace), context.Cause(ctx))
	if initializationErr == nil {
		finishGrace()
		c.selectionIssues = nil
		return nil
	}
	startGrace()
	closeErr := c.closeUnsuccessfulInitialization(graceContext)
	if finishGrace() {
		closeErr = errors.Join(closeErr, fmt.Errorf(
			unsuccessfulInitializationTimeoutFormat,
			unsuccessfulInitializationGracePeriod,
			context.DeadlineExceeded,
		))
	}
	return controllerui.ClassifyTransportError(errors.Join(initializationErr, closeErr))
}

// closeUnsuccessfulInitialization reuses the remaining initialization grace for the close handshake.
func (c *Service) closeUnsuccessfulInitialization(graceContext context.Context) error {
	writer := operation.NewWriter(c.stream.Send)
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.Run(c.stream.Context()) }()
	closeRequest := new(uiv1.OpenRequest)
	closeRequest.SetClose(new(operationv1.CloseConnection))
	acknowledgement, result := writer.EnqueueAcknowledged(closeRequest, nil)
	if result == nil {
		if waitErr := acknowledgement.Wait(graceContext); waitErr != nil &&
			!errors.Is(context.Cause(graceContext), context.DeadlineExceeded) {
			result = errors.Join(result, waitErr)
		}
	}
	writer.Close()
	var writerErr error
	select {
	case writerErr = <-writerDone:
	case <-graceContext.Done():
		writerErr = <-writerDone
	}
	if remainingErr := controllerui.WithoutTransportClosureLeaves(writerErr); remainingErr != nil {
		result = errors.Join(result, remainingErr)
	}
	if closeSendErr := c.stream.CloseSend(); closeSendErr != nil {
		result = errors.Join(result, fmt.Errorf("close UI initialization request stream: %w", closeSendErr))
	}
	receiveDone := make(chan error, 1)
	go func() { receiveDone <- c.receiveUnsuccessfulInitializationClose() }()
	var receiveErr error
	select {
	case receiveErr = <-receiveDone:
	case <-graceContext.Done():
		receiveErr = <-receiveDone
	}
	return errors.Join(result, receiveErr)
}

// receiveUnsuccessfulInitializationClose drains responses until the peer completes cleanup.
func (c *Service) receiveUnsuccessfulInitializationClose() error {
	for {
		_, receiveErr := c.stream.Recv()
		if receiveErr == nil {
			continue
		}
		remainingErr := controllerui.WithoutTransportClosureLeaves(receiveErr)
		if remainingErr == nil {
			return nil
		}
		return fmt.Errorf("receive UI initialization close: %w", remainingErr)
	}
}

// initialize tracks initialization and optional cancellation on one stream.
func (c *Service) initialize(
	ctx context.Context,
	request *uiv1.OpenRequest,
	startGrace func(),
) (returnErr error) {
	writerContext, cancelWriter := context.WithCancelCause(c.stream.Context())
	defer cancelWriter(context.Canceled)
	writer := operation.NewWriter(c.stream.Send)
	tracker := operation.NewTracker[struct{}, *uiv1.UICompleted]()
	initializeEvents, err := tracker.Track(initializationOperationID)
	if err != nil {
		return fmt.Errorf("track UI initialization: %w", err)
	}
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.Run(writerContext) }()
	var receiveDone chan initializationReceive
	defer func() {
		returnErr = finishInitialization(returnErr, tracker, writer, writerDone, receiveDone)
	}()
	if err = writer.Enqueue(request, c.startupSources()); err != nil {
		return fmt.Errorf("send UI initialization: %w", err)
	}

	var cancellationEvents <-chan operation.Event[struct{}, *uiv1.UICompleted]
	ctxDone := ctx.Done()
	initializationTerminal := false
	cancellationTerminal := false
	for !initializationTerminal || cancellationEvents != nil && !cancellationTerminal {
		if receiveDone == nil {
			receiveDone = make(chan initializationReceive, 1)
			go func(result chan<- initializationReceive) {
				response, receiveErr := c.stream.Recv()
				result <- initializationReceive{response: response, err: receiveErr}
			}(receiveDone)
		}
		select {
		case <-ctxDone:
			ctxDone = nil
			returnErr = context.Cause(ctx)
			startGrace()
			cancellationEvents, err = c.startInitializationCancellation(writer, tracker)
			if err != nil {
				return err
			}
		case received := <-receiveDone:
			receiveDone = nil
			trackedEvent, isCancellation, receiveErr := processInitializationResponse(
				writerContext, received, writer, tracker, initializeEvents, cancellationEvents,
			)
			if receiveErr != nil {
				return receiveErr
			}
			if !isTerminalEvent(trackedEvent.Kind) {
				continue
			}
			if isCancellation {
				cancellationTerminal = true
				continue
			}
			initializationTerminal = true
			returnErr = c.applyInitializationTerminal(trackedEvent, cancellationEvents == nil)
		case writerErr := <-writerDone:
			startGrace()
			writerDone <- writerErr
			if writerErr == nil {
				return errors.New("UI initialization writer stopped before terminal event")
			}
			return writerErr
		}
	}
	return returnErr
}

// finishInitialization closes startup components and preserves writer failure.
func finishInitialization(
	result error,
	tracker *operation.Tracker[struct{}, *uiv1.UICompleted],
	writer *operation.Writer[*uiv1.OpenRequest],
	writerDone <-chan error,
	receiveDone <-chan initializationReceive,
) error {
	tracker.Close()
	writer.Close()
	if writerErr := controllerui.WithoutTransportClosureLeaves(<-writerDone); writerErr != nil {
		result = errors.Join(result, fmt.Errorf("run UI initialization writer: %w", writerErr))
	}
	if receiveDone != nil {
		received := <-receiveDone
		if receiveErr := controllerui.WithoutTransportClosureLeaves(received.err); receiveErr != nil {
			result = errors.Join(result, fmt.Errorf("receive UI initialization lifecycle: %w", receiveErr))
		}
	}
	return controllerui.JoinOutputSources(result, writer.SourceErrors())
}

// processInitializationResponse validates and tracks one startup stream response.
func processInitializationResponse(
	ctx context.Context,
	received initializationReceive,
	writer *operation.Writer[*uiv1.OpenRequest],
	tracker *operation.Tracker[struct{}, *uiv1.UICompleted],
	initializeEvents <-chan operation.Event[struct{}, *uiv1.UICompleted],
	cancellationEvents <-chan operation.Event[struct{}, *uiv1.UICompleted],
) (operation.Event[struct{}, *uiv1.UICompleted], bool, error) {
	if received.err != nil {
		return operation.Event[struct{}, *uiv1.UICompleted]{}, false,
			fmt.Errorf("receive UI initialization lifecycle: %w", received.err)
	}
	if received.response.GetRequest() != nil {
		err := controllerui.RejectStartupRequest(&operationDelivery{
			ctx: ctx, writer: writer, fail: nil, mutex: sync.Mutex{}, kinds: nil,
		}, received.response)
		return operation.Event[struct{}, *uiv1.UICompleted]{}, false, err
	}
	if received.response.GetClose() != nil {
		if received.response.GetOperationId() != "" {
			return operation.Event[struct{}, *uiv1.UICompleted]{}, false,
				status.Error(codes.FailedPrecondition, "receive UI close: operation identifier must be empty")
		}
		return operation.Event[struct{}, *uiv1.UICompleted]{}, false, io.EOF
	}
	if received.response.GetEvent() == nil {
		return operation.Event[struct{}, *uiv1.UICompleted]{}, false,
			status.Error(codes.FailedPrecondition, "receive UI initialization: operation event is required")
	}
	if received.response.GetOperationId() == "" {
		return operation.Event[struct{}, *uiv1.UICompleted]{}, false,
			status.Error(codes.FailedPrecondition, "receive UI initialization: operation identifier is required")
	}
	kind := requestInitialization
	isCancellation := received.response.GetOperationId() == initializationCancellationOperationID
	if isCancellation {
		kind = requestInitializationCancellation
	}
	if err := validateUIOperationPayload(kind, received.response.GetEvent()); err != nil {
		return operation.Event[struct{}, *uiv1.UICompleted]{}, false,
			status.Error(codes.FailedPrecondition, err.Error())
	}
	if err := tracker.Handle(mapUIOperationEvent(received.response)); err != nil {
		return operation.Event[struct{}, *uiv1.UICompleted]{}, false,
			status.Error(codes.FailedPrecondition, err.Error())
	}
	trackedEvents := initializeEvents
	if isCancellation {
		trackedEvents = cancellationEvents
	}
	trackedEvent, open := <-trackedEvents
	if !open {
		return operation.Event[struct{}, *uiv1.UICompleted]{}, false, operation.ErrClosed
	}
	return trackedEvent, isCancellation, nil
}

// applyInitializationTerminal updates readiness and returns the target operation result.
func (c *Service) applyInitializationTerminal(
	event operation.Event[struct{}, *uiv1.UICompleted],
	activate bool,
) error {
	switch event.Kind {
	case operation.EventCompleted:
		if activate {
			c.mutex.Lock()
			c.ready = true
			c.mutex.Unlock()
		}
		return nil
	case operation.EventRejected, operation.EventFailed:
		return newInitializationError(event.Code, event.Message)
	case operation.EventCanceled:
		return context.Canceled
	case operation.EventAccepted, operation.EventRunning, operation.EventProgress:
		return nil
	}
	return errors.New("UI initialization terminal event is unknown")
}

// startInitializationCancellation starts one separate cancellation operation.
func (c *Service) startInitializationCancellation(
	writer *operation.Writer[*uiv1.OpenRequest],
	tracker *operation.Tracker[struct{}, *uiv1.UICompleted],
) (<-chan operation.Event[struct{}, *uiv1.UICompleted], error) {
	events, err := tracker.Track(initializationCancellationOperationID)
	if err != nil {
		return nil, fmt.Errorf("track UI initialization cancellation: %w", err)
	}
	cancel := operationv1.CancelOperation_builder{TargetOperationId: new(initializationOperationID)}.Build()
	request := new(uiv1.HostRequest)
	request.SetCancel(cancel)
	if enqueueErr := writer.Enqueue(uiv1.OpenRequest_builder{
		OperationId: new(initializationCancellationOperationID), Request: request,
		Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build(), nil); enqueueErr != nil {
		return nil, fmt.Errorf("send UI initialization cancellation: %w", enqueueErr)
	}
	return events, nil
}

// initializationRequestKind identifies Host-owned startup operations.
type initializationRequestKind uint8

const (
	// requestInitialization identifies UI initialization.
	requestInitialization initializationRequestKind = iota + 1
	// requestInitializationCancellation identifies initialization cancellation.
	requestInitializationCancellation
)

// validateUIOperationPayload correlates UI completion with the Host request kind.
func validateUIOperationPayload(kind initializationRequestKind, event *uiv1.UIEvent) error {
	if event == nil {
		return errors.New("UI operation event is required")
	}
	completed := event.GetCompleted()
	if completed == nil {
		return nil
	}
	if kind == requestInitialization && completed.GetInitialized() == nil {
		return errors.New("UI completed payload does not match initialization request")
	}
	if kind == requestInitializationCancellation {
		if completed.GetCancel() == nil {
			return errors.New("UI completed payload does not match initialization cancellation request")
		}
		if !completed.GetCancel().HasTargetState() ||
			completed.GetCancel().GetTargetState() == operationv1.TerminalState_TERMINAL_STATE_UNSPECIFIED {
			return errors.New("UI initialization cancellation target state is required")
		}
	}
	return nil
}

// mapUIOperationEvent maps one UI lifecycle event into tracker input.
func mapUIOperationEvent(response *uiv1.OpenResponse) operation.Event[struct{}, *uiv1.UICompleted] {
	mapped := operation.Event[struct{}, *uiv1.UICompleted]{
		ID: response.GetOperationId(), Kind: 0, Progress: struct{}{}, Result: nil, Code: "", Message: "",
	}
	event := response.GetEvent()
	switch event.WhichEvent() {
	case uiv1.UIEvent_Accepted_case:
		mapped.Kind = operation.EventAccepted
	case uiv1.UIEvent_Running_case:
		mapped.Kind = operation.EventRunning
	case uiv1.UIEvent_Completed_case:
		mapped.Kind, mapped.Result = operation.EventCompleted, event.GetCompleted()
	case uiv1.UIEvent_Canceled_case:
		mapped.Kind = operation.EventCanceled
	case uiv1.UIEvent_Failed_case:
		mapped.Kind, mapped.Code, mapped.Message = operation.EventFailed,
			event.GetFailed().GetCode(), event.GetFailed().GetMessage()
	case uiv1.UIEvent_Rejected_case:
		mapped.Kind, mapped.Code, mapped.Message = operation.EventRejected,
			event.GetRejected().GetCode(), event.GetRejected().GetMessage()
	case uiv1.UIEvent_Event_not_set_case:
	}
	return mapped
}

// isTerminalEvent reports whether one tracker event is terminal.
func isTerminalEvent(kind operation.EventKind) bool {
	return kind == operation.EventCompleted || kind == operation.EventCanceled ||
		kind == operation.EventFailed || kind == operation.EventRejected
}
