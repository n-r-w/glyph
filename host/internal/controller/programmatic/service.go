package programmatic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// SessionCompletionCause identifies why the sole Programmatic session ended.
type SessionCompletionCause uint8

const (
	// SessionCompletionUnspecified identifies an unset completion cause.
	SessionCompletionUnspecified SessionCompletionCause = iota
	// SessionCompletionCleanClientClosure reports controller half-close.
	SessionCompletionCleanClientClosure
	// SessionCompletionApplicationCanceled reports Host-requested closure.
	SessionCompletionApplicationCanceled
	// SessionCompletionProtocolFailure reports a stream protocol failure.
	SessionCompletionProtocolFailure
	// SessionCompletionTransportFailure reports unavailable transport delivery.
	SessionCompletionTransportFailure
)

// SessionCompletion reports terminal state to application wiring.
type SessionCompletion struct {
	// Cause identifies why the session ended.
	Cause SessionCompletionCause
	// Err contains the terminal error when present.
	Err error
}

// Service owns the sole Programmatic Control connection.
type Service struct {
	// UnimplementedProgrammaticControlServiceServer supplies generated default gRPC methods.
	programmaticv1.UnimplementedProgrammaticControlServiceServer
	// applicationContext closes the session when the Host stops.
	applicationContext context.Context
	// session prepares and runs Programmatic operations.
	session HostSession
	// ownerClaimed prevents a second stream from claiming the sole controller.
	ownerClaimed atomic.Bool
	// completions reports the sole stream terminal result to application wiring.
	completions chan SessionCompletion
}

var _ programmaticv1.ProgrammaticControlServiceServer = (*Service)(nil)

// New creates the sole Programmatic Control stream controller.
func New(applicationContext context.Context, session HostSession) *Service {
	unimplemented := programmaticv1.UnimplementedProgrammaticControlServiceServer{}
	return &Service{
		UnimplementedProgrammaticControlServiceServer: unimplemented,

		applicationContext: applicationContext,
		session:            session,
		ownerClaimed:       atomic.Bool{},
		completions:        make(chan SessionCompletion, 1),
	}
}

// Completions reports the sole stream terminal result.
func (s *Service) Completions() <-chan SessionCompletion {
	return s.completions
}

// Open executes one asynchronous Programmatic operation stream.
func (s *Service) Open(stream programmaticv1.ProgrammaticControlService_OpenServer) error {
	return s.open(stream)
}

// open owns receipt, operation work, delivery, and closure for one stream.
func (s *Service) open(stream OpenStream) error {
	if !s.ownerClaimed.CompareAndSwap(false, true) {
		return status.Error(codes.FailedPrecondition, "a Programmatic Control stream already owns this process")
	}

	connectionContext, cancelConnection := context.WithCancelCause(stream.Context())
	defer cancelConnection(context.Canceled)
	registry := newTargetRegistry()
	writer := operation.NewWriter(stream.Send)
	var owner *operation.Owner[AgentEvent, Response]
	delivery := &streamDelivery{
		context:  connectionContext,
		writer:   writer,
		registry: registry,
		fail: func(err error) {
			cancelConnection(mapDeliveryError(err))
			if owner != nil {
				owner.Fail(mapDeliveryError(err))
			}
		},
	}
	owner = operation.NewOwner(connectionContext, delivery)

	writerResult := make(chan error, 1)
	go func() {
		writerResult <- writer.Run(connectionContext)
	}()
	closing := &localClosingState{started: atomic.Bool{}}
	receiveResult := make(chan error, 1)
	go func() {
		receiveResult <- s.receive(connectionContext, stream, owner, delivery, registry, closing)
	}()

	var completion SessionCompletion
	var rpcErr error
	select {
	case receiveErr := <-receiveResult:
		if errors.Is(receiveErr, io.EOF) {
			completion = SessionCompletion{Cause: SessionCompletionCleanClientClosure, Err: nil}
			owner.Close()
			registry.close()
			writer.Close()
			if writeErr := <-writerResult; writeErr != nil {
				completion = SessionCompletion{Cause: SessionCompletionTransportFailure, Err: writeErr}
				rpcErr = mapTransportError(writeErr)
			}
		} else {
			cancelConnection(receiveErr)
			owner.Fail(receiveErr)
			owner.Wait()
			registry.close()
			<-writerResult
			if isReceiveTransportFailure(receiveErr) {
				completion = SessionCompletion{
					Cause: SessionCompletionTransportFailure,
					Err:   receiveErr,
				}
				rpcErr = mapTransportError(receiveErr)
			} else {
				completion = SessionCompletion{
					Cause: SessionCompletionProtocolFailure,
					Err:   receiveErr,
				}
				rpcErr = mapReceiveError(receiveErr)
			}
		}
	case writeErr := <-writerResult:
		if writeErr == nil {
			writeErr = errors.New("programmatic writer stopped before stream closure")
		}
		owner.Fail(writeErr)
		owner.Wait()
		registry.close()
		completion = SessionCompletion{Cause: SessionCompletionTransportFailure, Err: writeErr}
		rpcErr = mapTransportError(writeErr)
	case <-connectionContext.Done():
		connectionErr := context.Cause(connectionContext)
		owner.Fail(connectionErr)
		owner.Wait()
		registry.close()
		<-writerResult
		completion = SessionCompletion{Cause: SessionCompletionTransportFailure, Err: connectionErr}
		rpcErr = mapDeliveryError(connectionErr)
	case <-s.applicationContext.Done():
		closing.started.Store(true)
		closeErr := delivery.closeConnection()
		owner.Close()
		registry.close()
		if closeErr != nil {
			<-writerResult
			completion = SessionCompletion{
				Cause: SessionCompletionTransportFailure,
				Err:   closeErr,
			}
			rpcErr = mapDeliveryError(closeErr)
			break
		}

		completion, rpcErr = waitForControllerHalfClose(
			connectionContext, s.applicationContext.Err(), cancelConnection, writer, receiveResult, writerResult,
		)
	}
	s.completions <- completion
	return rpcErr
}

// waitForControllerHalfClose keeps the response writer active until request EOF or stream failure.
func waitForControllerHalfClose(
	connectionContext context.Context,
	applicationErr error,
	cancelConnection context.CancelCauseFunc,
	writer *operation.Writer[*programmaticv1.OpenResponse],
	receiveResult <-chan error,
	writerResult <-chan error,
) (SessionCompletion, error) {
	select {
	case receiveErr := <-receiveResult:
		if !errors.Is(receiveErr, io.EOF) {
			cancelConnection(receiveErr)
			<-writerResult
			if isReceiveTransportFailure(receiveErr) {
				return SessionCompletion{
					Cause: SessionCompletionTransportFailure,
					Err:   receiveErr,
				}, mapTransportError(receiveErr)
			}
			return SessionCompletion{
				Cause: SessionCompletionProtocolFailure,
				Err:   receiveErr,
			}, mapReceiveError(receiveErr)
		}
		writer.Close()
		if writeErr := <-writerResult; writeErr != nil {
			return SessionCompletion{
				Cause: SessionCompletionTransportFailure,
				Err:   writeErr,
			}, mapTransportError(writeErr)
		}
		return SessionCompletion{
			Cause: SessionCompletionApplicationCanceled,
			Err:   applicationErr,
		}, nil
	case writeErr := <-writerResult:
		if writeErr == nil {
			writeErr = errors.New("programmatic writer stopped before controller half-close")
		}
		cancelConnection(writeErr)
		return SessionCompletion{
			Cause: SessionCompletionTransportFailure,
			Err:   writeErr,
		}, mapTransportError(writeErr)
	case <-connectionContext.Done():
		connectionErr := context.Cause(connectionContext)
		<-writerResult
		return SessionCompletion{
			Cause: SessionCompletionTransportFailure,
			Err:   connectionErr,
		}, mapDeliveryError(connectionErr)
	}
}

// localClosingState records when Host-requested closure stops request admission.
type localClosingState struct {
	// started is true after the Host begins the local close protocol.
	started atomic.Bool
}

// receive prepares requests without waiting for accepted operation work.
//
//nolint:gocognit,gocyclo // Receipt exhaustively handles cancellation, mapping, admission, and rejection.
func (s *Service) receive(
	ctx context.Context,
	stream OpenStream,
	owner *operation.Owner[AgentEvent, Response],
	delivery *streamDelivery,
	registry *targetRegistry,
	closing *localClosingState,
) error {
	for {
		request, err := stream.Recv()
		if err != nil {
			return err
		}
		if closing.started.Load() {
			return status.Error(codes.FailedPrecondition, "request received after CloseConnection")
		}
		if request != nil && request.HasRequest() &&
			request.GetRequest().WhichRequest() == programmaticv1.ControllerRequest_Cancel_case {
			if cancelErr := s.prepareCancellation(request, owner, delivery, registry); cancelErr != nil {
				if errors.Is(cancelErr, operation.ErrClosed) && closing.started.Load() {
					return status.Error(codes.FailedPrecondition, "request received after CloseConnection")
				}
				return cancelErr
			}
			continue
		}
		command, mapErr := mapOpenRequest(request)
		if mapErr != nil {
			operationID := ""
			if request != nil {
				operationID = request.GetOperationId()
			}
			if code, rejected := rejectionCode(mapErr); rejected {
				if rejectErr := delivery.reject(operationID, code); rejectErr != nil {
					return rejectErr
				}
				continue
			}
			return mapErr
		}
		err = owner.Start(command.OperationID, func() (operation.Prepared[AgentEvent, Response], error) {
			prepared, prepareErr := s.session.Prepare(ctx, command)
			if prepareErr != nil {
				return nil, prepareErr
			}
			target := registry.add(command.OperationID, command.Kind)
			return &registeredPrepared{
				id: command.OperationID, prepared: prepared, registry: registry, target: target,
				mutex: sync.Mutex{}, started: false, release: sync.Once{},
			}, nil
		})
		if err == nil {
			continue
		}
		if errors.Is(err, operation.ErrClosed) && closing.started.Load() {
			return status.Error(codes.FailedPrecondition, "request received after CloseConnection")
		}
		code, perRequest := rejectionCode(err)
		if errors.Is(err, operation.ErrIdentifierInUse) {
			code, perRequest = RejectionCodeOperationIDInUse, true
		}
		if perRequest {
			if rejectErr := delivery.reject(command.OperationID, code); rejectErr != nil {
				return rejectErr
			}
			continue
		}
		return status.Errorf(codes.Internal, "prepare Programmatic operation: %v", err)
	}
}

// prepareCancellation validates and starts one controller-owned cancellation operation.
func (s *Service) prepareCancellation(
	request *programmaticv1.OpenRequest,
	owner *operation.Owner[AgentEvent, Response],
	delivery *streamDelivery,
	registry *targetRegistry,
) error {
	operationID := request.GetOperationId()
	cancelRequest := request.GetRequest().GetCancel()
	if operationID == "" || cancelRequest == nil || !cancelRequest.HasTargetOperationId() ||
		cancelRequest.GetTargetOperationId() == "" {
		return delivery.reject(operationID, RejectionCodeInvalidArgument)
	}
	targetID := cancelRequest.GetTargetOperationId()
	err := owner.Start(operationID, func() (operation.Prepared[AgentEvent, Response], error) {
		target, active := registry.active(targetID)
		if !active {
			return nil, Reject(RejectionCodeTargetNotActive)
		}
		ownedTarget := registry.add(operationID, CommandCancel)
		prepared := &cancellationPrepared{owner: owner, targetID: targetID, target: target}
		return &registeredPrepared{
			id: operationID, prepared: prepared, registry: registry, target: ownedTarget,
			mutex: sync.Mutex{}, started: false, release: sync.Once{},
		}, nil
	})
	if errors.Is(err, operation.ErrIdentifierInUse) {
		return delivery.reject(operationID, RejectionCodeOperationIDInUse)
	}
	if code, rejected := rejectionCode(err); rejected {
		return delivery.reject(operationID, code)
	}
	return err
}

// mapDeliveryError maps local bounded delivery failures to stream status.
func mapDeliveryError(err error) error {
	if errors.Is(err, operation.ErrQueueFull) {
		return status.Error(codes.ResourceExhausted, "Programmatic delivery queue is full")
	}
	return mapTransportError(err)
}

// mapTransportError preserves gRPC status and classifies other transport failures.
func mapTransportError(err error) error {
	if err == nil {
		return nil
	}
	if _, present := status.FromError(err); present {
		return err
	}
	return status.Error(codes.Unavailable, fmt.Sprintf("Programmatic transport failed: %v", err))
}

// isReceiveTransportFailure identifies stream termination caused by the receive transport.
func isReceiveTransportFailure(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	switch status.Code(err) {
	case codes.Canceled, codes.DeadlineExceeded, codes.Unavailable:
		return true
	case codes.OK, codes.Unknown, codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.ResourceExhausted, codes.FailedPrecondition, codes.Aborted,
		codes.OutOfRange, codes.Unimplemented, codes.Internal, codes.DataLoss, codes.Unauthenticated:
		return false
	default:
		return false
	}
}

// mapReceiveError preserves incoming status and classifies undecodable frames.
func mapReceiveError(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}
	if _, present := status.FromError(err); present {
		return err
	}
	return status.Error(codes.InvalidArgument, fmt.Sprintf("receive Programmatic request: %v", err))
}
