package ui

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/samber/mo"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// OperationResult contains one ordinary result or cancellation result.
type OperationResult struct {
	// Frame contains an ordinary operation result.
	Frame Frame
	// Cancel contains the target operation terminal state.
	Cancel mo.Option[operation.TerminalState]
}

// hostPrepared maps one use-case prepared operation into the transport result union.
type hostPrepared struct {
	// prepared owns the use-case operation and its reservation.
	prepared operation.Prepared[Frame, Frame]
}

var _ operation.Prepared[Frame, OperationResult] = (*hostPrepared)(nil)

// Run preserves the use-case terminal state while wrapping completed data.
func (prepared *hostPrepared) Run(
	ctx context.Context,
	reporter operation.Reporter[Frame],
) operation.Outcome[OperationResult] {
	outcome := prepared.prepared.Run(ctx, reporter)
	switch outcome.State() {
	case operation.TerminalStateCompleted:
		frame, _ := outcome.Result()
		return operation.CompletedWithSource(
			OperationResult{Frame: frame, Cancel: mo.None[operation.TerminalState]()}, outcome.SourceError(),
		)
	case operation.TerminalStateCanceled:
		return operation.Canceled[OperationResult]()
	case operation.TerminalStateFailed:
		return operation.Failed[OperationResult](outcome.Code(), outcome.Err())
	default:
		return operation.Failed[OperationResult](
			FailureCodeInternal,
			errors.New("UI operation terminal state is invalid"),
		)
	}
}

// Release frees the use-case admission reservation.
func (prepared *hostPrepared) Release() { prepared.prepared.Release() }

// hostCancellationPrepared owns one accepted UI cancellation operation.
type hostCancellationPrepared struct {
	// cancel requests target cancellation and waits for its actual terminal state.
	cancel func(context.Context) (operation.TerminalState, error)
}

var _ operation.Prepared[Frame, OperationResult] = (*hostCancellationPrepared)(nil)

// Run cancels the target and returns its actual terminal state.
func (prepared *hostCancellationPrepared) Run(
	ctx context.Context,
	_ operation.Reporter[Frame],
) operation.Outcome[OperationResult] {
	state, err := prepared.cancel(ctx)
	remainingErr := WithoutTransportClosureLeaves(err)
	if err != nil && remainingErr == nil {
		return operation.Canceled[OperationResult]()
	}
	if remainingErr != nil {
		return operation.Failed[OperationResult](FailureCodeInternal, remainingErr)
	}
	return operation.Completed(OperationResult{Frame: NewFrame(0), Cancel: mo.Some(state)})
}

// Release has no separate cancellation reservation to free.
func (*hostCancellationPrepared) Release() {}

// runOperations receives and executes prepared UI operations until closure.
func (c *Service) runOperations(ctx context.Context, session Session) error {
	connectionContext, cancelConnection := context.WithCancelCause(context.WithoutCancel(ctx))
	defer cancelConnection(context.Canceled)
	writer, delivery := c.connection.AttachOutput(connectionContext, cancelConnection)
	owner := operation.NewOwner[Frame, OperationResult](connectionContext, delivery)
	stopActivation := session.Activate(ctx)
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.Run(connectionContext) }()
	closing := new(atomic.Bool)
	peerClose := make(chan struct{}, 1)
	receiveDone := make(chan error, 1)
	go func() {
		receiveDone <- c.receiveOperations(connectionContext, owner, delivery, session, closing, peerClose)
	}()

	exit := awaitOperationLoopExit(ctx, connectionContext, closing, peerClose, writerDone, receiveDone)
	// Drain asynchronous error producers while the output attachment can still retain their sources.
	stopActivation()
	var cleanupErr error
	if exit.requestedClose {
		cleanupErr = c.closeRequestedOperations(connectionContext, owner, delivery, writer, writerDone, receiveDone)
	} else {
		cleanupErr = c.closeFailedOperations(cancelConnection, owner, writer, writerDone, receiveDone, exit)
	}
	exitErr := WithoutTransportClosureLeaves(exit.err)
	result := JoinOutputSources(errors.Join(exitErr, cleanupErr), owner.SourceErrors(), writer.SourceErrors())
	c.connection.ClearOutput()
	return result
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
func (c *Service) closeRequestedOperations(
	connectionContext context.Context,
	owner *operation.Owner[Frame, OperationResult],
	delivery OperationOutput,
	writer *operation.Writer[*uiv1.OpenRequest],
	writerDone <-chan error,
	receiveDone <-chan error,
) error {
	var result error
	acknowledgement, err := delivery.CloseConnection()
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
		if err = c.connection.CloseSend(); err != nil {
			result = errors.Join(result, fmt.Errorf("close UI request stream: %w", err))
		}
	}
	if err = WithoutTransportClosureLeaves(<-receiveDone); err != nil {
		result = errors.Join(result, err)
	}
	return result
}

// finishRequestedWriter stops failed transport before joining a blocked writer.
func (c *Service) finishRequestedWriter(
	writer *operation.Writer[*uiv1.OpenRequest],
	writerDone <-chan error,
	writerFinished bool,
	transportFailed bool,
) (bool, error) {
	writer.Close()
	var result error
	if transportFailed {
		if err := c.connection.CloseSend(); err != nil {
			result = errors.Join(result, fmt.Errorf("close UI request stream: %w", err))
		}
	}
	if !writerFinished {
		if err := WithoutTransportClosureLeaves(<-writerDone); err != nil {
			result = errors.Join(result, err)
		}
	}
	return transportFailed, result
}

// closeFailedOperations cancels work and joins both transport loops.
func (c *Service) closeFailedOperations(
	cancelConnection context.CancelCauseFunc,
	owner *operation.Owner[Frame, OperationResult],
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
	if err := c.connection.CloseSend(); err != nil {
		result = errors.Join(result, fmt.Errorf("close UI request stream: %w", err))
	}
	if !exit.writerFinished {
		if writerErr := WithoutTransportClosureLeaves(<-writerDone); writerErr != nil {
			result = errors.Join(result, writerErr)
		}
	}
	if !exit.receiveFinished {
		if receiveErr := WithoutTransportClosureLeaves(<-receiveDone); receiveErr != nil {
			result = errors.Join(result, receiveErr)
		}
	}
	return result
}

// receiveOperations keeps request receipt active while owner workers execute admitted work.
func (c *Service) receiveOperations(
	ctx context.Context,
	owner *operation.Owner[Frame, OperationResult],
	delivery OperationOutput,
	session Session,
	closing *atomic.Bool,
	peerClose chan<- struct{},
) error {
	peerCloseReceived := false
	for {
		// Failure cleanup half-closes client requests before joining this receive and its transport result.
		response, err := c.connection.Recv()
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		default:
		}
		err = handleOperationResponse(
			ctx, response, closing, &peerCloseReceived, owner, delivery, session, peerClose,
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
	owner *operation.Owner[Frame, OperationResult],
	delivery OperationOutput,
	session Session,
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
		return delivery.Reject(
			id, RejectionCodeInvalidArgument, errors.New("UI operation identifier is required"),
		)
	}
	if closing.Load() {
		return status.Error(codes.FailedPrecondition, "receive UI operation: connection is closing")
	}
	if response.GetRequest().GetCancel() != nil {
		delivery.SetKind(id, operationKindCancel)
		err := startHostCancellation(owner, delivery, id, response.GetRequest().GetCancel())
		if err != nil {
			delivery.TakeKind(id)
		}
		return err
	}
	command, err := mapCommand(response)
	if err != nil {
		return delivery.Reject(id, RejectionCodeInvalidArgument, err)
	}
	delivery.SetKind(id, hostRequestKind(response.GetRequest()))
	err = startPreparedHostOperation(ctx, owner, id, command, session)
	if err == nil {
		return nil
	}
	delivery.TakeKind(id)
	if classified, ok := errors.AsType[PreparationFailure](err); ok {
		return delivery.Reject(id, classified.PreparationCode(), err)
	}
	if errors.Is(err, operation.ErrIdentifierInUse) {
		return delivery.Reject(id, RejectionCodeOperationIDInUse, err)
	}
	return &transportError{
		code: codes.Internal, cause: fmt.Errorf("prepare UI operation %q: %w", id, err),
	}
}

// unknownOperationKind identifies a missing or unsupported operation kind in logs.
const unknownOperationKind = "unknown"

const (
	// operationKindCancel identifies cancel operations.
	operationKindCancel = "cancel"
	// operationKindCloneSession identifies clone session operations.
	operationKindCloneSession = "clone_session"
	// operationKindCreateSession identifies create session operations.
	operationKindCreateSession = "create_session"
	// operationKindForkSession identifies fork session operations.
	operationKindForkSession = "fork_session"
	// operationKindGetSessionInfo identifies get session info operations.
	operationKindGetSessionInfo = "get_session_info"
	// operationKindGetSessionTree identifies get session tree operations.
	operationKindGetSessionTree = "get_session_tree"
	// operationKindListSessions identifies list sessions operations.
	operationKindListSessions = "list_sessions"
	// operationKindNavigateSessionTree identifies navigate session tree operations.
	operationKindNavigateSessionTree = "navigate_session_tree"
	// operationKindResumeSession identifies resume session operations.
	operationKindResumeSession = "resume_session"
	// operationKindRetryAuthentication identifies retry authentication operations.
	operationKindRetryAuthentication = "retry_authentication"
	// operationKindSelectModel identifies select model operations.
	operationKindSelectModel = "select_model"
	// operationKindSelectReasoningChoice identifies select reasoning choice operations.
	operationKindSelectReasoningChoice = "select_reasoning_choice"
	// operationKindSetEntryLabel identifies set entry label operations.
	operationKindSetEntryLabel = "set_entry_label"
	// operationKindSetSessionName identifies set session name operations.
	operationKindSetSessionName = "set_session_name"
	// operationKindSubmit identifies submit operations.
	operationKindSubmit = "submit"
)

// hostRequestKind returns the stable public kind of one Host-owned UI operation.
func hostRequestKind(request *uiv1.UIRequest) string {
	switch request.WhichRequest() {
	case uiv1.UIRequest_Submit_case:
		return operationKindSubmit
	case uiv1.UIRequest_RetryAuthentication_case:
		return operationKindRetryAuthentication
	case uiv1.UIRequest_SelectModel_case:
		return operationKindSelectModel
	case uiv1.UIRequest_SelectReasoningChoice_case:
		return operationKindSelectReasoningChoice
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
		return operationKindCreateSession
	case uiv1.UIRequest_ListSessions_case:
		return operationKindListSessions
	case uiv1.UIRequest_ResumeSession_case:
		return operationKindResumeSession
	case uiv1.UIRequest_SetSessionName_case:
		return operationKindSetSessionName
	case uiv1.UIRequest_GetSessionInfo_case:
		return operationKindGetSessionInfo
	case uiv1.UIRequest_GetSessionTree_case:
		return operationKindGetSessionTree
	case uiv1.UIRequest_NavigateSessionTree_case:
		return operationKindNavigateSessionTree
	case uiv1.UIRequest_ForkSession_case:
		return operationKindForkSession
	case uiv1.UIRequest_CloneSession_case:
		return operationKindCloneSession
	case uiv1.UIRequest_SetEntryLabel_case:
		return operationKindSetEntryLabel
	case uiv1.UIRequest_Cancel_case:
		return operationKindCancel
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
	owner *operation.Owner[Frame, OperationResult],
	id string,
	command Command,
	session Session,
) error {
	return owner.Start(id, func() (operation.Prepared[Frame, OperationResult], error) {
		prepared, err := session.Prepare(ctx, command)
		if err != nil {
			return nil, err
		}
		return &hostPrepared{prepared: prepared}, nil
	})
}

// startHostCancellation validates and starts one cancellation operation.
func startHostCancellation(
	owner *operation.Owner[Frame, OperationResult],
	delivery OperationOutput,
	id string,
	request *operationv1.CancelOperation,
) error {
	target := request.GetTargetOperationId()
	if target == "" {
		return delivery.Reject(

			id,
			RejectionCodeInvalidArgument,
			errors.New("UI cancellation target is required"),
		)
	}
	cancelTarget, active := owner.Cancellation(target)
	if !active {
		return delivery.Reject(id, RejectionCodeTargetNotActive, fmt.Errorf("UI operation %q is not active", target))
	}
	startErr := owner.Start(id, func() (operation.Prepared[Frame, OperationResult], error) {
		return &hostCancellationPrepared{cancel: cancelTarget}, nil
	})
	if errors.Is(startErr, operation.ErrIdentifierInUse) {
		return delivery.Reject(id, RejectionCodeOperationIDInUse, startErr)
	}
	return startErr
}
