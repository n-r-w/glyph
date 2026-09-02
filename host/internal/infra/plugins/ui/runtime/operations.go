package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// hostOperationResult contains one ordinary result or cancellation result.
type hostOperationResult struct {
	// frame contains an ordinary operation result.
	frame domainui.Frame
	// cancel contains a cancellation result.
	cancel *operationv1.CancelCompleted
}

// hostPrepared maps one use-case prepared operation into the transport result union.
type hostPrepared struct {
	// prepared owns the use-case operation and its reservation.
	prepared operation.Prepared[domainui.Frame, domainui.Frame]
}

var _ operation.Prepared[domainui.Frame, hostOperationResult] = (*hostPrepared)(nil)

// Run preserves the use-case terminal state while wrapping completed data.
func (prepared *hostPrepared) Run(
	ctx context.Context,
	reporter operation.Reporter[domainui.Frame],
) operation.Outcome[hostOperationResult] {
	outcome := prepared.prepared.Run(ctx, reporter)
	switch outcome.State() {
	case operation.TerminalStateCompleted:
		frame, _ := outcome.Result()
		return operation.Completed(hostOperationResult{frame: frame, cancel: nil})
	case operation.TerminalStateCanceled:
		return operation.Canceled[hostOperationResult]()
	case operation.TerminalStateFailed:
		return operation.Failed[hostOperationResult](outcome.Code(), outcome.Err())
	default:
		return operation.Failed[hostOperationResult](
			failureCodeInternal,
			errors.New("UI operation terminal state is invalid"),
		)
	}
}

// Release frees the use-case admission reservation.
func (prepared *hostPrepared) Release() { prepared.prepared.Release() }

// operationDelivery maps shared lifecycle events to the Host-to-UI stream.
type operationDelivery struct {
	// ctx owns structured operation failure logs.
	ctx context.Context
	// writer serializes every Host stream message.
	writer *operation.Writer[*uiv1.OpenRequest]
	// fail closes the owning connection after outbound delivery failure.
	fail func(error)
	// mutex protects accepted operation kinds.
	mutex sync.Mutex
	// kinds maps accepted identifiers to public request kinds.
	kinds map[string]string
	// failureSources retains failed operation causes until terminal transport delivery.
	failureSources map[string]error
}

var _ operation.Delivery[domainui.Frame, hostOperationResult] = (*operationDelivery)(nil)

// Accepted queues and acknowledges one accepted event.
func (delivery *operationDelivery) Accepted(id string) (*operation.Acknowledgement, error) {
	event := new(uiv1.HostEvent)
	event.SetAccepted(new(operationv1.Accepted))
	return delivery.enqueueAcknowledged(hostEventRequest(id, event))
}

// Running queues one running event.
func (delivery *operationDelivery) Running(id string) error {
	event := new(uiv1.HostEvent)
	event.SetRunning(new(operationv1.Running))
	return delivery.enqueue(hostEventRequest(id, event))
}

// Progress maps and queues one operation progress payload.
func (delivery *operationDelivery) Progress(id string, progress domainui.Frame) error {
	request, err := mapFrame(progress)
	if err != nil {
		return err
	}
	if request.GetEvent().GetProgress() == nil {
		return errors.New("map UI operation progress: progress payload is required")
	}
	request.SetOperationId(id)
	return delivery.enqueue(request)
}

// Terminal maps and queues one terminal lifecycle event.
func (delivery *operationDelivery) Terminal(
	id string,
	outcome operation.Outcome[hostOperationResult],
) (*operation.Acknowledgement, error) {
	var request *uiv1.OpenRequest
	switch outcome.State() {
	case operation.TerminalStateCompleted:
		result, _ := outcome.Result()
		if result.cancel != nil {
			completed := new(uiv1.HostCompleted)
			completed.SetCancel(result.cancel)
			event := new(uiv1.HostEvent)
			event.SetCompleted(completed)
			request = hostEventRequest(id, event)
		} else {
			mapped, err := mapFrame(result.frame)
			if err != nil {
				return nil, err
			}
			if mapped.GetEvent().GetCompleted() == nil {
				return nil, errors.New("map UI operation terminal: completed payload is required")
			}
			mapped.SetOperationId(id)
			request = mapped
		}
	case operation.TerminalStateCanceled:
		event := new(uiv1.HostEvent)
		event.SetCanceled(new(operationv1.Canceled))
		request = hostEventRequest(id, event)
	case operation.TerminalStateFailed:
		delivery.setFailureSource(id, outcome.Err())
		slog.ErrorContext(delivery.ctx, "Host UI operation failed",
			slog.String("operation_id", id), slog.String("operation_kind", delivery.takeKind(id)),
			slog.String("peer_kind", "ui"), slog.String("category", outcome.Code()),
			slog.Any("error", outcome.Err()),
		)
		event := new(uiv1.HostEvent)
		event.SetFailed(operationv1.Failed_builder{
			Code: new(outcome.Code()), Message: new(outcome.Err().Error()),
		}.Build())
		request = hostEventRequest(id, event)
	default:
		return nil, errors.New("map UI operation terminal: terminal state is required")
	}
	if outcome.State() != operation.TerminalStateFailed {
		delivery.takeKind(id)
	}
	return delivery.enqueueAcknowledged(request)
}

// setKind records one accepted operation kind before its worker starts.
func (delivery *operationDelivery) setKind(id, kind string) {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	delivery.kinds[id] = kind
}

// takeKind removes and returns one accepted operation kind.
func (delivery *operationDelivery) takeKind(id string) string {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	kind := delivery.kinds[id]
	delete(delivery.kinds, id)
	return kind
}

// setFailureSource retains one source until the writer resolves terminal transport delivery.
func (delivery *operationDelivery) setFailureSource(id string, source error) {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	delivery.failureSources[id] = source
}

// takeFailureSource removes one retained terminal source.
func (delivery *operationDelivery) takeFailureSource(id string) error {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	source := delivery.failureSources[id]
	delete(delivery.failureSources, id)
	return source
}

// enqueue queues one message and closes the connection on failure.
func (delivery *operationDelivery) enqueue(request *uiv1.OpenRequest) error {
	err := delivery.writer.Enqueue(request)
	if err != nil {
		delivery.fail(err)
	}
	return err
}

// enqueueAcknowledged queues one acknowledged message and closes the connection on failure.
func (delivery *operationDelivery) enqueueAcknowledged(
	request *uiv1.OpenRequest,
) (*operation.Acknowledgement, error) {
	acknowledgement, err := delivery.writer.EnqueueAcknowledged(request)
	if err != nil {
		err = errors.Join(delivery.takeFailureSource(request.GetOperationId()), err)
		delivery.fail(err)
	}
	return acknowledgement, err
}

// hostCancellationPrepared owns one accepted UI cancellation operation.
type hostCancellationPrepared struct {
	// cancel requests target cancellation and waits for its actual terminal state.
	cancel func(context.Context) (operation.TerminalState, error)
}

var _ operation.Prepared[domainui.Frame, hostOperationResult] = (*hostCancellationPrepared)(nil)

// Run cancels the target and returns its actual terminal state.
func (prepared *hostCancellationPrepared) Run(
	ctx context.Context,
	_ operation.Reporter[domainui.Frame],
) operation.Outcome[hostOperationResult] {
	state, err := prepared.cancel(ctx)
	remainingErr := withoutTransportClosureLeaves(err)
	if err != nil && remainingErr == nil {
		return operation.Canceled[hostOperationResult]()
	}
	if remainingErr != nil {
		return operation.Failed[hostOperationResult](failureCodeInternal, remainingErr)
	}
	mapped := operationv1.TerminalState_TERMINAL_STATE_UNSPECIFIED
	switch state {
	case operation.TerminalStateCompleted:
		mapped = operationv1.TerminalState_TERMINAL_STATE_COMPLETED
	case operation.TerminalStateCanceled:
		mapped = operationv1.TerminalState_TERMINAL_STATE_CANCELED
	case operation.TerminalStateFailed:
		mapped = operationv1.TerminalState_TERMINAL_STATE_FAILED
	}
	cancel := operationv1.CancelCompleted_builder{TargetState: new(mapped)}.Build()
	return operation.Completed(hostOperationResult{frame: domainui.NewFrame(0), cancel: cancel})
}

// Release has no separate cancellation reservation to free.
func (*hostCancellationPrepared) Release() {}

// RunOperations receives and executes prepared UI operations until closure.
func (c *channel) RunOperations(
	ctx context.Context,
	activate func(),
	prepare func(context.Context, domainui.Command) (operation.Prepared[domainui.Frame, domainui.Frame], error),
) error {
	connectionContext, cancelConnection := context.WithCancelCause(context.WithoutCancel(ctx))
	defer cancelConnection(context.Canceled)
	delivery := &operationDelivery{
		ctx: connectionContext, writer: nil,
		fail: func(err error) {
			cancelConnection(fmt.Errorf("deliver UI operation event: %w", err))
		},
		mutex: sync.Mutex{}, kinds: make(map[string]string), failureSources: make(map[string]error),
	}
	writer := operation.NewWriter(func(request *uiv1.OpenRequest) error {
		sendErr := c.stream.Send(request)
		var source error
		if request.GetEvent().GetFailed() != nil {
			source = delivery.takeFailureSource(request.GetOperationId())
		}
		if sendErr != nil {
			return errors.Join(source, sendErr)
		}
		return nil
	})
	delivery.writer = writer
	owner := operation.NewOwner[domainui.Frame, hostOperationResult](connectionContext, delivery)
	c.mutex.Lock()
	c.writer = writer
	c.failConnection = func(err error) {
		cancelConnection(fmt.Errorf("deliver UI connection event: %w", err))
	}
	c.mutex.Unlock()
	activate()
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.Run(connectionContext) }()
	closing := new(atomic.Bool)
	peerClose := make(chan struct{}, 1)
	receiveDone := make(chan error, 1)
	go func() {
		receiveDone <- c.receiveOperations(connectionContext, owner, delivery, prepare, closing, peerClose)
	}()

	exit := awaitOperationLoopExit(ctx, connectionContext, closing, peerClose, writerDone, receiveDone)
	var cleanupErr error
	if exit.requestedClose {
		cleanupErr = c.closeRequestedOperations(connectionContext, owner, writer, writerDone, receiveDone)
	} else {
		cleanupErr = c.closeFailedOperations(cancelConnection, owner, writer, writerDone, receiveDone, exit)
	}
	c.clearOperationBindings()
	exitErr := withoutTransportClosureLeaves(exit.err)
	if cleanupErr == nil && exitErr == nil {
		return nil
	}
	return classifyTransportError(errors.Join(exitErr, cleanupErr))
}

// operationLoopExit describes the first endpoint component that stopped.
type operationLoopExit struct {
	// err is the first loop or caller failure.
	err error
	// writerFinished reports that writerDone was consumed.
	writerFinished bool
	// receiveFinished reports that receiveDone was consumed.
	receiveFinished bool
	// requestedClose distinguishes normal closure from failure cleanup.
	requestedClose bool
}

// awaitOperationLoopExit waits for one closure or failure trigger.
func awaitOperationLoopExit(
	ctx context.Context,
	connectionContext context.Context,
	closing *atomic.Bool,
	peerClose <-chan struct{},
	writerDone <-chan error,
	receiveDone <-chan error,
) operationLoopExit {
	select {
	case err := <-receiveDone:
		return operationLoopExit{
			err: err, writerFinished: false, receiveFinished: true, requestedClose: false,
		}
	case <-peerClose:
		closing.Store(true)
		return operationLoopExit{
			err: nil, writerFinished: false, receiveFinished: false, requestedClose: true,
		}
	case err := <-writerDone:
		if err == nil {
			err = errors.New("UI Host writer stopped before connection closure")
		}
		return operationLoopExit{
			err: err, writerFinished: true, receiveFinished: false, requestedClose: false,
		}
	case <-ctx.Done():
		closing.Store(true)
		return operationLoopExit{
			err: context.Cause(ctx), writerFinished: false, receiveFinished: false,
			requestedClose: true,
		}
	case <-connectionContext.Done():
		return operationLoopExit{
			err: context.Cause(connectionContext), writerFinished: false, receiveFinished: false,
			requestedClose: false,
		}
	}
}

// closeRequestedOperations drains messages, half-closes the request stream, and waits for response EOF.
func (c *channel) closeRequestedOperations(
	connectionContext context.Context,
	owner *operation.Owner[domainui.Frame, hostOperationResult],
	writer *operation.Writer[*uiv1.OpenRequest],
	writerDone <-chan error,
	receiveDone <-chan error,
) error {
	var result error
	closeRequest := new(uiv1.OpenRequest)
	closeRequest.SetClose(new(operationv1.CloseConnection))
	acknowledgement, err := writer.EnqueueAcknowledged(closeRequest)
	if err != nil {
		result = errors.Join(result, fmt.Errorf("close UI connection: %w", err))
	}
	owner.Close()
	if acknowledgement != nil {
		if err = acknowledgement.Wait(connectionContext); err != nil {
			result = errors.Join(result, fmt.Errorf("deliver UI close request: %w", err))
		}
	}
	transportFailed := result != nil
	closeSendCalled, writerErr := c.finishRequestedWriter(writer, writerDone, false, transportFailed)
	result = errors.Join(result, writerErr)
	if !closeSendCalled {
		if err = c.stream.CloseSend(); err != nil {
			result = errors.Join(result, fmt.Errorf("close UI request stream: %w", err))
		}
	}
	if err = withoutTransportClosureLeaves(<-receiveDone); err != nil {
		result = errors.Join(result, err)
	}
	return result
}

// finishRequestedWriter stops failed transport before joining a blocked writer.
func (c *channel) finishRequestedWriter(
	writer *operation.Writer[*uiv1.OpenRequest],
	writerDone <-chan error,
	writerFinished bool,
	transportFailed bool,
) (bool, error) {
	writer.Close()
	var result error
	if transportFailed {
		if err := c.stream.CloseSend(); err != nil {
			result = errors.Join(result, fmt.Errorf("close UI request stream: %w", err))
		}
	}
	if !writerFinished {
		if err := withoutTransportClosureLeaves(<-writerDone); err != nil {
			result = errors.Join(result, err)
		}
	}
	return transportFailed, result
}

// closeFailedOperations cancels work and joins both transport loops.
func (c *channel) closeFailedOperations(
	cancelConnection context.CancelCauseFunc,
	owner *operation.Owner[domainui.Frame, hostOperationResult],
	writer *operation.Writer[*uiv1.OpenRequest],
	writerDone <-chan error,
	receiveDone <-chan error,
	exit operationLoopExit,
) error {
	if exit.err != nil {
		cancelConnection(exit.err)
	}
	owner.Close()
	writer.Close()
	var result error
	if err := c.stream.CloseSend(); err != nil {
		result = errors.Join(result, fmt.Errorf("close UI request stream: %w", err))
	}
	if !exit.writerFinished {
		if writerErr := withoutTransportClosureLeaves(<-writerDone); writerErr != nil {
			result = errors.Join(result, writerErr)
		}
	}
	if !exit.receiveFinished {
		if receiveErr := withoutTransportClosureLeaves(<-receiveDone); receiveErr != nil {
			result = errors.Join(result, receiveErr)
		}
	}
	return result
}

// clearOperationBindings removes connection-scoped delivery handles.
func (c *channel) clearOperationBindings() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.writer = nil
	c.progressReporter = operation.Reporter[domainui.Frame]{}
	c.progressBound = false
	c.failConnection = nil
}

// receiveOperations keeps request receipt active while owner workers execute admitted work.
func (c *channel) receiveOperations(
	ctx context.Context,
	owner *operation.Owner[domainui.Frame, hostOperationResult],
	delivery *operationDelivery,
	prepare func(context.Context, domainui.Command) (operation.Prepared[domainui.Frame, domainui.Frame], error),
	closing *atomic.Bool,
	peerClose chan<- struct{},
) error {
	peerCloseReceived := false
	for {
		response, err := c.stream.Recv()
		if err != nil {
			return err
		}
		err = handleOperationResponse(
			ctx, response, closing, &peerCloseReceived, owner, delivery, prepare, peerClose,
		)
		if err != nil {
			return err
		}
	}
}

// handleOperationResponse applies close state or starts one received request.
func handleOperationResponse(
	ctx context.Context,
	response *uiv1.OpenResponse,
	closing *atomic.Bool,
	peerCloseReceived *bool,
	owner *operation.Owner[domainui.Frame, hostOperationResult],
	delivery *operationDelivery,
	prepare func(context.Context, domainui.Command) (operation.Prepared[domainui.Frame, domainui.Frame], error),
	peerClose chan<- struct{},
) error {
	if response.GetClose() != nil {
		if response.GetOperationId() != "" {
			return status.Error(codes.FailedPrecondition, "receive UI close: operation identifier must be empty")
		}
		closing.Store(true)
		if !*peerCloseReceived {
			*peerCloseReceived = true
			peerClose <- struct{}{}
		}
		return nil
	}
	if response.GetRequest() == nil {
		return status.Error(codes.FailedPrecondition, "receive UI operation: request is required")
	}
	id := response.GetOperationId()
	if id == "" {
		return rejectPrepared(
			delivery, id, rejectionCodeInvalidArgument, errors.New("UI operation identifier is required"),
		)
	}
	if closing.Load() {
		return status.Error(codes.FailedPrecondition, "receive UI operation: connection is closing")
	}
	if response.GetRequest().GetCancel() != nil {
		delivery.setKind(id, "cancel")
		err := startHostCancellation(owner, delivery, id, response.GetRequest().GetCancel())
		if err != nil {
			delivery.takeKind(id)
		}
		return err
	}
	command, err := mapCommand(response)
	if err != nil {
		return rejectPrepared(delivery, id, rejectionCodeInvalidArgument, err)
	}
	delivery.setKind(id, hostRequestKind(response.GetRequest()))
	err = startPreparedHostOperation(ctx, owner, id, command, prepare)
	if err == nil {
		return nil
	}
	delivery.takeKind(id)
	if classified, ok := errors.AsType[interface {
		Code() string
		error
	}](err); ok {
		return rejectPrepared(delivery, id, classified.Code(), err)
	}
	if errors.Is(err, operation.ErrIdentifierInUse) {
		return rejectPrepared(delivery, id, "OPERATION_ID_IN_USE", err)
	}
	return &transportError{
		code: codes.Internal, cause: fmt.Errorf("prepare UI operation %q: %w", id, err),
	}
}

// unknownOperationKind identifies a missing or unsupported operation kind in logs.
const unknownOperationKind = "unknown"

// hostRequestKind returns the stable public kind of one Host-owned UI operation.
func hostRequestKind(request *uiv1.UIRequest) string {
	switch request.WhichRequest() {
	case uiv1.UIRequest_Submit_case:
		return "submit"
	case uiv1.UIRequest_RetryAuthentication_case:
		return "retry_authentication"
	case uiv1.UIRequest_SelectModel_case:
		return "select_model"
	case uiv1.UIRequest_SelectReasoningChoice_case:
		return "select_reasoning_choice"
	case uiv1.UIRequest_CreateSession_case, uiv1.UIRequest_ListSessions_case,
		uiv1.UIRequest_ResumeSession_case, uiv1.UIRequest_SetSessionName_case,
		uiv1.UIRequest_GetSessionInfo_case, uiv1.UIRequest_GetSessionTree_case,
		uiv1.UIRequest_NavigateSessionTree_case, uiv1.UIRequest_ForkSession_case,
		uiv1.UIRequest_CloneSession_case, uiv1.UIRequest_SetEntryLabel_case,
		uiv1.UIRequest_Cancel_case, uiv1.UIRequest_Request_not_set_case:
		return hostSessionRequestKind(request)
	default:
		return unknownOperationKind
	}
}

// hostSessionRequestKind returns one session or cancellation operation kind.
func hostSessionRequestKind(request *uiv1.UIRequest) string {
	switch request.WhichRequest() {
	case uiv1.UIRequest_CreateSession_case:
		return "create_session"
	case uiv1.UIRequest_ListSessions_case:
		return "list_sessions"
	case uiv1.UIRequest_ResumeSession_case:
		return "resume_session"
	case uiv1.UIRequest_SetSessionName_case:
		return "set_session_name"
	case uiv1.UIRequest_GetSessionInfo_case:
		return "get_session_info"
	case uiv1.UIRequest_GetSessionTree_case:
		return "get_session_tree"
	case uiv1.UIRequest_NavigateSessionTree_case:
		return "navigate_session_tree"
	case uiv1.UIRequest_ForkSession_case:
		return "fork_session"
	case uiv1.UIRequest_CloneSession_case:
		return "clone_session"
	case uiv1.UIRequest_SetEntryLabel_case:
		return "set_entry_label"
	case uiv1.UIRequest_Cancel_case:
		return "cancel"
	case uiv1.UIRequest_Request_not_set_case,
		uiv1.UIRequest_Submit_case, uiv1.UIRequest_RetryAuthentication_case,
		uiv1.UIRequest_SelectModel_case, uiv1.UIRequest_SelectReasoningChoice_case:
		return unknownOperationKind
	default:
		return unknownOperationKind
	}
}

// startPreparedHostOperation admits one mapped command through the operation owner.
func startPreparedHostOperation(
	ctx context.Context,
	owner *operation.Owner[domainui.Frame, hostOperationResult],
	id string,
	command domainui.Command,
	prepare func(context.Context, domainui.Command) (operation.Prepared[domainui.Frame, domainui.Frame], error),
) error {
	return owner.Start(id, func() (operation.Prepared[domainui.Frame, hostOperationResult], error) {
		prepared, err := prepare(ctx, command)
		if err != nil {
			return nil, err
		}
		return &hostPrepared{prepared: prepared}, nil
	})
}

// startHostCancellation validates and starts one cancellation operation.
func startHostCancellation(
	owner *operation.Owner[domainui.Frame, hostOperationResult],
	delivery *operationDelivery,
	id string,
	request *operationv1.CancelOperation,
) error {
	target := request.GetTargetOperationId()
	if target == "" {
		return rejectPrepared(
			delivery,
			id,
			rejectionCodeInvalidArgument,
			errors.New("UI cancellation target is required"),
		)
	}
	cancelTarget, active := owner.Cancellation(target)
	if !active {
		return rejectPrepared(delivery, id, "TARGET_NOT_ACTIVE", fmt.Errorf("UI operation %q is not active", target))
	}
	startErr := owner.Start(id, func() (operation.Prepared[domainui.Frame, hostOperationResult], error) {
		return &hostCancellationPrepared{cancel: cancelTarget}, nil
	})
	if errors.Is(startErr, operation.ErrIdentifierInUse) {
		return rejectPrepared(delivery, id, "OPERATION_ID_IN_USE", startErr)
	}
	return startErr
}

// rejectPrepared queues one request rejection without creating an operation.
func rejectPrepared(delivery *operationDelivery, id, code string, cause error) error {
	event := new(uiv1.HostEvent)
	event.SetRejected(operationv1.Rejected_builder{Code: new(code), Message: new(cause.Error())}.Build())
	return delivery.writer.Enqueue(hostEventRequest(id, event))
}
