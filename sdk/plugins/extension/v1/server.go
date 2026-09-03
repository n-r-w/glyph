package extensionv1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"google.golang.org/grpc/codes"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

//go:generate go tool mockgen -build_constraint=!integration -destination=stream_mock_test.go -package=extensionv1 github.com/n-r-w/glyph/pkg/plugins/extension/v1 ExtensionServiceClient,ExtensionService_OpenServer,ExtensionService_OpenClient

// server implements the generated extension operation stream.
type server struct {
	// UnimplementedExtensionServiceServer provides generated forward defaults.
	extensionpb.UnimplementedExtensionServiceServer
	// service prepares plugin-owned work.
	service Service
	// mutex protects startup state and registered handler kinds.
	mutex sync.RWMutex
	// registering reports that startup registration was accepted.
	registering bool
	// ready reports that registration completion reached the Host.
	ready bool
	// handlers maps registered handler identifiers to their payload kinds.
	handlers map[string]extensionpb.HandlerKind
}

var _ extensionpb.ExtensionServiceServer = (*server)(nil)

// newServer constructs the SDK-owned generated server.
func newServer(service Service) *server {
	return &server{
		UnimplementedExtensionServiceServer: extensionpb.UnimplementedExtensionServiceServer{},
		service:                             service,
		mutex:                               sync.RWMutex{},
		registering:                         false,
		ready:                               false,
		handlers:                            make(map[string]extensionpb.HandlerKind),
	}
}

// extensionDelivery maps shared lifecycle events to ExtensionEvent messages.
type extensionDelivery struct {
	// ctx owns structured failure logging.
	ctx context.Context
	// writer serializes every extension stream response.
	writer *operation.Writer[*extensionpb.OpenResponse]
	// fail closes the connection after outbound delivery failure.
	fail func(error)
	// mutex protects accepted operation kinds.
	mutex sync.Mutex
	// kinds maps accepted identifiers to their request kinds.
	kinds map[string]requestKind
}

var _ operation.Delivery[*extensionpb.ToolProgress, extensionResult] = (*extensionDelivery)(nil)

// Accepted queues and acknowledges one accepted event.
func (delivery *extensionDelivery) Accepted(id string) (*operation.Acknowledgement, error) {
	event := new(extensionpb.ExtensionEvent)
	event.SetAccepted(new(operationv1.Accepted))
	return delivery.enqueueAcknowledged(extensionEventResponse(id, event))
}

// Running queues one running event.
func (delivery *extensionDelivery) Running(id string) error {
	event := new(extensionpb.ExtensionEvent)
	event.SetRunning(new(operationv1.Running))
	return delivery.enqueue(extensionEventResponse(id, event))
}

// Progress queues one tool progress event.
func (delivery *extensionDelivery) Progress(id string, progress *extensionpb.ToolProgress) error {
	if err := validateToolProgress(progress); err != nil {
		return err
	}
	payload := new(extensionpb.ExtensionProgress)
	payload.SetTool(progress)
	event := new(extensionpb.ExtensionEvent)
	event.SetProgress(payload)
	return delivery.enqueue(extensionEventResponse(id, event))
}

// Terminal queues and acknowledges one terminal event.
func (delivery *extensionDelivery) Terminal(
	id string,
	outcome operation.Outcome[extensionResult],
) (*operation.Acknowledgement, error) {
	event := new(extensionpb.ExtensionEvent)
	switch outcome.State() {
	case operation.TerminalStateCompleted:
		result, _ := outcome.Result()
		if result.completed == nil {
			return delivery.terminalError(errors.New("map extension terminal: completed payload is required"))
		}
		event.SetCompleted(result.completed)
	case operation.TerminalStateCanceled:
		event.SetCanceled(new(operationv1.Canceled))
	case operation.TerminalStateFailed:
		kind := delivery.takeKind(id)
		slog.ErrorContext(delivery.ctx, "Extension operation failed",
			slog.String("operation_id", id),
			slog.String("operation_kind", kind.String()),
			slog.String("peer_kind", "host"),
			slog.String("category", outcome.Code()),
			slog.Any("error", outcome.Err()),
		)
		if err := validateFailureCode(outcome.Code()); err != nil {
			return delivery.terminalError(fmt.Errorf("map extension terminal: %w: %w", err, outcome.Err()))
		}
		event.SetFailed(operationv1.Failed_builder{
			Code: new(outcome.Code()), Message: new(outcome.Err().Error()),
		}.Build())
	default:
		return delivery.terminalError(errors.New("map extension terminal: terminal state is required"))
	}
	response := extensionEventResponse(id, event)
	acknowledgement, err := delivery.enqueueAcknowledged(response)
	if err != nil {
		return nil, err
	}
	if outcome.State() != operation.TerminalStateFailed {
		delivery.takeKind(id)
	}
	return acknowledgement, nil
}

// setKind records one accepted operation kind.
func (delivery *extensionDelivery) setKind(id string, kind requestKind) {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	delivery.kinds[id] = kind
}

// takeKind removes and returns one accepted operation kind.
func (delivery *extensionDelivery) takeKind(id string) requestKind {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	kind := delivery.kinds[id]
	delete(delivery.kinds, id)
	return kind
}

// terminalError closes the connection for a local terminal mapping invariant failure.
func (delivery *extensionDelivery) terminalError(err error) (*operation.Acknowledgement, error) {
	streamErr := newProtocolStatusError(codes.Internal, err.Error(), err)
	delivery.fail(streamErr)
	return nil, streamErr
}

// enqueue queues one response and reports delivery failure.
func (delivery *extensionDelivery) enqueue(response *extensionpb.OpenResponse) error {
	err := delivery.writer.Enqueue(response)
	if err != nil {
		mapped := mapDeliveryError(err)
		delivery.fail(mapped)
		return mapped
	}
	return nil
}

// enqueueAcknowledged queues one acknowledged response and reports delivery failure.
func (delivery *extensionDelivery) enqueueAcknowledged(
	response *extensionpb.OpenResponse,
) (*operation.Acknowledgement, error) {
	acknowledgement, err := delivery.writer.EnqueueAcknowledged(response)
	if err != nil {
		mapped := mapDeliveryError(err)
		delivery.fail(mapped)
		return nil, mapped
	}
	return acknowledgement, nil
}

// extensionEventResponse constructs one lifecycle response envelope.
func extensionEventResponse(id string, event *extensionpb.ExtensionEvent) *extensionpb.OpenResponse {
	return extensionpb.OpenResponse_builder{OperationId: new(id), Event: event}.Build()
}

// handleRequest validates and admits one Host request.
func (s *server) handleRequest(
	ctx context.Context,
	owner *operation.Owner[*extensionpb.ToolProgress, extensionResult],
	delivery *extensionDelivery,
	message *extensionpb.OpenRequest,
) error {
	if message == nil || message.GetRequest() == nil {
		cause := errors.New("extension stream message requires a request or close")
		return newProtocolStatusError(codes.FailedPrecondition, cause.Error(), cause)
	}
	id := message.GetOperationId()
	if id == "" {
		return s.reject(delivery, id, rejectionCodeInvalidArgument, errors.New("operation identifier is required"))
	}
	request := message.GetRequest()
	kind, classifyErr := classifyRequest(request)
	if classifyErr != nil {
		return s.reject(delivery, id, rejectionCodeInvalidArgument, classifyErr)
	}
	prepare := func() (operation.Prepared[*extensionpb.ToolProgress, extensionResult], error) {
		var prepared operation.Prepared[*extensionpb.ToolProgress, extensionResult]
		var err error
		if request.GetCancel() != nil {
			target := request.GetCancel().GetTargetOperationId()
			if target == "" {
				return nil, Reject(
					rejectionCodeInvalidArgument,
					errors.New("cancellation target identifier is required"),
				)
			}
			cancelTarget, active := owner.Cancellation(target)
			if !active {
				return nil, Reject(rejectionCodeTargetNotActive, fmt.Errorf("operation %q is not active", target))
			}
			prepared = &cancellationPrepared{cancel: cancelTarget}
		} else {
			prepared, err = s.prepare(ctx, request)
		}
		if err == nil {
			delivery.setKind(id, kind)
		}
		return prepared, err
	}
	if err := owner.Start(id, prepare); err != nil {
		if errors.Is(err, operation.ErrIdentifierInUse) {
			return s.reject(delivery, id, rejectionCodeOperationIDInUse, err)
		}
		delivery.takeKind(id)
		if rejection, ok := errors.AsType[*RejectionError](err); ok {
			if codeErr := validateRejectionCode(kind, rejection.Code()); codeErr != nil {
				cause := errors.Join(codeErr, rejection)
				return newProtocolStatusError(codes.Internal, cause.Error(), cause)
			}
			return s.reject(delivery, id, rejection.Code(), rejection)
		}
		return newProtocolStatusError(codes.Internal, err.Error(), err)
	}
	return nil
}

// prepare maps one public request to admitted plugin work.
func (s *server) prepare(
	ctx context.Context,
	request *extensionpb.HostRequest,
) (operation.Prepared[*extensionpb.ToolProgress, extensionResult], error) {
	switch request.WhichRequest() {
	case extensionpb.HostRequest_Register_case:
		if err := s.beginRegistration(); err != nil {
			return nil, err
		}
		admitted, err := s.service.PrepareRegister(ctx, request.GetRegister())
		if err != nil {
			s.resetRegistration()
			return nil, err
		}
		if admitted == nil {
			s.resetRegistration()
			return nil, errors.New("prepare registration: operation is required")
		}
		return &registerPrepared{operation: admitted}, nil
	case extensionpb.HostRequest_Handle_case:
		if err := s.validateHandle(request.GetHandle()); err != nil {
			return nil, err
		}
		admitted, err := s.service.PrepareHandle(ctx, request.GetHandle())
		if err != nil {
			return nil, err
		}
		if admitted == nil {
			return nil, errors.New("prepare handler: operation is required")
		}
		return &handlePrepared{operation: admitted}, nil
	case extensionpb.HostRequest_Execute_case:
		if err := s.validateExecute(request.GetExecute()); err != nil {
			return nil, err
		}
		admitted, err := s.service.PrepareExecute(ctx, request.GetExecute())
		if err != nil {
			return nil, err
		}
		if admitted == nil {
			return nil, errors.New("prepare tool: operation is required")
		}
		return &executePrepared{operation: admitted}, nil
	case extensionpb.HostRequest_Cancel_case:
		return nil, errors.New("cancellation is prepared by the operation owner")
	case extensionpb.HostRequest_Request_not_set_case:
		return nil, Reject(rejectionCodeInvalidArgument, errors.New("extension request payload is required"))
	default:
		return nil, Reject(rejectionCodeInvalidArgument, errors.New("extension request payload is unknown"))
	}
}

// reject sends one nonterminal request rejection without closing the stream.
func (s *server) reject(
	delivery *extensionDelivery,
	id string,
	code string,
	cause error,
) error {
	event := new(extensionpb.ExtensionEvent)
	event.SetRejected(operationv1.Rejected_builder{Code: new(code), Message: new(cause.Error())}.Build())
	return delivery.enqueue(extensionEventResponse(id, event))
}

// beginRegistration admits only the first startup request.
func (s *server) beginRegistration() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.registering || s.ready {
		return Reject(rejectionCodeBusy, errors.New("extension registration is already active or complete"))
	}
	s.registering = true
	return nil
}

// resetRegistration releases startup admission after preparation rejection.
func (s *server) resetRegistration() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.registering = false
}

// completeRegistration records handler kinds after registration reaches the Host.
func (s *server) completeRegistration(response *extensionpb.RegisterResponse) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.handlers = make(map[string]extensionpb.HandlerKind, len(response.GetHandlers()))
	for _, descriptor := range response.GetHandlers() {
		if descriptor != nil {
			s.handlers[descriptor.GetId()] = descriptor.GetKind()
		}
	}
	s.ready = true
}

// validateHandle checks readiness, handler identity, and matching payload shape.
func (s *server) validateHandle(request *extensionpb.HandleRequest) error {
	if request == nil || request.GetHandlerId() == "" {
		return Reject(rejectionCodeInvalidArgument, errors.New("handler identifier is required"))
	}
	s.mutex.RLock()
	ready := s.ready
	kind, known := s.handlers[request.GetHandlerId()]
	s.mutex.RUnlock()
	if !ready {
		return Reject(rejectionCodeNotReady, errors.New("extension registration is not complete"))
	}
	if !known {
		return Reject(rejectionCodeInvalidArgument, fmt.Errorf("handler %q is not registered", request.GetHandlerId()))
	}
	valid := kind == extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST &&
		request.GetSessionBeforeTreeRequest() != nil ||
		kind == extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_RESULT &&
			request.GetSessionBeforeTreeResult() != nil ||
		kind == extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE && request.GetSessionTree() != nil
	if !valid {
		return Reject(
			rejectionCodeInvalidArgument,
			fmt.Errorf("handler %q payload does not match its registered kind", request.GetHandlerId()),
		)
	}
	return nil
}

// validateExecute checks readiness and bounded request syntax.
func (s *server) validateExecute(request *extensionpb.ExecuteRequest) error {
	s.mutex.RLock()
	ready := s.ready
	s.mutex.RUnlock()
	if !ready {
		return Reject(rejectionCodeNotReady, errors.New("extension registration is not complete"))
	}
	if request == nil || request.GetToolName() == "" {
		return Reject(rejectionCodeInvalidArgument, errors.New("tool name is required"))
	}
	if !json.Valid(request.GetArgumentsJson()) {
		return Reject(rejectionCodeInvalidArgument, errors.New("tool arguments must contain valid JSON"))
	}
	return nil
}

// validateToolProgress checks the complete progress payload.
func validateToolProgress(progress *extensionpb.ToolProgress) error {
	if progress == nil {
		return errors.New("tool progress is required")
	}
	switch progress.GetChannel() {
	case extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS,
		extensionpb.ProgressChannel_PROGRESS_CHANNEL_STDOUT,
		extensionpb.ProgressChannel_PROGRESS_CHANNEL_STDERR:
		return nil
	case extensionpb.ProgressChannel_PROGRESS_CHANNEL_UNSPECIFIED:
		return errors.New("tool progress channel is required")
	default:
		return errors.New("tool progress channel is unknown")
	}
}
