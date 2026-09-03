package uiv1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const (
	// connectionEventQueueCapacity bounds pending Host connection events.
	connectionEventQueueCapacity = 64
	// unknownSDKOperationKind identifies an unsupported SDK-owned request kind in logs.
	unknownSDKOperationKind = "unknown"
)

//go:generate go tool mockgen -source=service.go -destination=interfaces_mock_test.go -package=uiv1

// Service supplies UI-owned initialization and application behavior.
type Service interface {
	// PrepareInitialize performs bounded initialization validation and admission.
	PrepareInitialize(context.Context, *uiv1.Initialization) (InitializeOperation, error)
	// Run owns the UI application after initialization reaches its terminal result.
	Run(context.Context, *Host) error
	// Close releases initialization resources that Run did not consume.
	Close() error
}

// InitializeOperation owns one accepted initialization request.
type InitializeOperation interface {
	// Run initializes UI presentation resources.
	Run(context.Context) (*uiv1.Initialized, error)
	// Release frees initialization admission exactly once.
	Release()
}

// server owns the generated gRPC service and SDK operation runtime.
type server struct {
	// UnimplementedUIServiceServer rejects unknown future methods.
	uiv1.UnimplementedUIServiceServer
	// service supplies public UI behavior.
	service Service
}

var _ uiv1.UIServiceServer = (*server)(nil)

// newServer constructs the SDK-owned generated service.
func newServer(service Service) *server {
	if service == nil {
		panic("UI service is required")
	}
	return &server{UnimplementedUIServiceServer: uiv1.UnimplementedUIServiceServer{}, service: service}
}

// initializePrepared adapts public initialization work to the private operation owner.
type initializePrepared struct {
	// operation is implemented by external UI code.
	operation InitializeOperation
}

var _ operation.Prepared[struct{}, *uiv1.UICompleted] = (*initializePrepared)(nil)

// Run executes initialization and maps its public error semantics.
func (p *initializePrepared) Run(
	ctx context.Context,
	_ operation.Reporter[struct{}],
) operation.Outcome[*uiv1.UICompleted] {
	initialized, err := p.operation.Run(ctx)
	remainingErr := withoutClosureLeaves(err)
	if err != nil && remainingErr == nil {
		return operation.Canceled[*uiv1.UICompleted]()
	}
	if remainingErr != nil {
		if failure, ok := errors.AsType[*FailureError](remainingErr); ok {
			return operation.Failed[*uiv1.UICompleted](failure.Code(), remainingErr)
		}
		return operation.Failed[*uiv1.UICompleted]("INTERNAL", remainingErr)
	}
	if initialized == nil {
		return operation.Failed[*uiv1.UICompleted](
			"INTERNAL",
			errors.New("initialize UI: initialized result is required"),
		)
	}
	completed := new(uiv1.UICompleted)
	completed.SetInitialized(initialized)
	return operation.Completed(completed)
}

// Release frees external initialization admission.
func (p *initializePrepared) Release() { p.operation.Release() }

// cancellationPrepared owns one accepted cancellation operation.
type cancellationPrepared struct {
	// cancel requests target cancellation and waits for terminal delivery.
	cancel func(context.Context) (operation.TerminalState, error)
	// target identifies the target operation.
	target string
}

var _ operation.Prepared[struct{}, *uiv1.UICompleted] = (*cancellationPrepared)(nil)

// Run cancels the target and reports its terminal state.
func (p *cancellationPrepared) Run(
	ctx context.Context,
	_ operation.Reporter[struct{}],
) operation.Outcome[*uiv1.UICompleted] {
	state, err := p.cancel(ctx)
	remainingErr := withoutClosureLeaves(err)
	if err != nil && remainingErr == nil {
		return operation.Canceled[*uiv1.UICompleted]()
	}
	if remainingErr != nil {
		return operation.Failed[*uiv1.UICompleted](
			"INTERNAL", fmt.Errorf("cancel UI operation %q: %w", p.target, remainingErr),
		)
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
	completed := new(uiv1.UICompleted)
	completed.SetCancel(cancel)
	return operation.Completed(completed)
}

// Release has no separate cancellation admission to free.
func (*cancellationPrepared) Release() {}

// uiDelivery maps private owner lifecycle events to the public UI stream.
type uiDelivery struct {
	// ctx owns structured operation failure logs.
	ctx context.Context
	// writer serializes generated responses.
	writer *operation.Writer[*uiv1.OpenResponse]
	// fail closes the owning connection after outbound failure.
	fail func(error)
	// mutex protects accepted operation kinds.
	mutex sync.Mutex
	// kinds maps accepted identifiers to public request kinds.
	kinds map[string]string
}

var _ operation.Delivery[struct{}, *uiv1.UICompleted] = (*uiDelivery)(nil)

// Accepted queues one accepted event.
func (d *uiDelivery) Accepted(id string) (*operation.Acknowledgement, error) {
	event := new(uiv1.UIEvent)
	event.SetAccepted(new(operationv1.Accepted))
	return d.enqueueAcknowledged(uiEventResponse(id, event))
}

// Running queues one running event.
func (d *uiDelivery) Running(id string) error {
	event := new(uiv1.UIEvent)
	event.SetRunning(new(operationv1.Running))
	return d.enqueue(uiEventResponse(id, event))
}

// Progress rejects impossible initialization progress.
func (*uiDelivery) Progress(string, struct{}) error {
	return errors.New("UI initialization progress is unsupported")
}

// Terminal queues one terminal event.
func (d *uiDelivery) Terminal(
	id string,
	outcome operation.Outcome[*uiv1.UICompleted],
) (*operation.Acknowledgement, error) {
	event := new(uiv1.UIEvent)
	switch outcome.State() {
	case operation.TerminalStateCompleted:
		result, _ := outcome.Result()
		event.SetCompleted(result)
	case operation.TerminalStateCanceled:
		event.SetCanceled(new(operationv1.Canceled))
	case operation.TerminalStateFailed:
		slog.ErrorContext(d.ctx, "UI SDK operation failed",
			slog.String("operation_id", id), slog.String("operation_kind", d.takeKind(id)),
			slog.String("peer_kind", "host"), slog.String("category", outcome.Code()),
			slog.Any("error", outcome.Err()),
		)
		failed := operationv1.Failed_builder{Code: new(outcome.Code()), Message: new(outcome.Err().Error())}.Build()
		event.SetFailed(failed)
	}
	if outcome.State() != operation.TerminalStateFailed {
		d.takeKind(id)
	}
	return d.enqueueAcknowledged(uiEventResponse(id, event))
}

// setKind records one accepted operation kind before its worker starts.
func (d *uiDelivery) setKind(id, kind string) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.kinds[id] = kind
}

// takeKind removes and returns one accepted operation kind.
func (d *uiDelivery) takeKind(id string) string {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	kind := d.kinds[id]
	delete(d.kinds, id)
	return kind
}

// enqueue queues one UI message and closes the connection on failure.
func (d *uiDelivery) enqueue(response *uiv1.OpenResponse) error {
	err := d.writer.Enqueue(response)
	if err != nil {
		d.fail(err)
	}
	return err
}

// enqueueAcknowledged queues one acknowledged UI message and closes on failure.
func (d *uiDelivery) enqueueAcknowledged(
	response *uiv1.OpenResponse,
) (*operation.Acknowledgement, error) {
	acknowledgement, err := d.writer.EnqueueAcknowledged(response)
	if err != nil {
		d.fail(err)
	}
	return acknowledgement, err
}

// uiEventResponse constructs one UI-owned lifecycle response.
func uiEventResponse(id string, event *uiv1.UIEvent) *uiv1.OpenResponse {
	response := uiv1.OpenResponse_builder{OperationId: new(id), Event: event, Request: nil, Close: nil}.Build()
	return response
}

// Open runs one SDK-owned UI operation connection.
func (s *server) Open(stream grpc.BidiStreamingServer[uiv1.OpenRequest, uiv1.OpenResponse]) error {
	ctx, cancel := context.WithCancelCause(stream.Context())
	defer cancel(context.Canceled)
	serviceContext, cancelService := context.WithCancelCause(ctx)
	defer cancelService(context.Canceled)
	initialized := make(chan struct{})
	var initializedOnce sync.Once
	writer := operation.NewWriter(func(response *uiv1.OpenResponse) error {
		if err := sendUIResponse(ctx, response, stream.Send); err != nil {
			return err
		}
		if response.GetEvent().GetCompleted().GetInitialized() != nil {
			initializedOnce.Do(func() { close(initialized) })
		}
		return nil
	})
	delivery := &uiDelivery{
		ctx: ctx, writer: writer,
		fail: func(err error) {
			cancel(fmt.Errorf("deliver UI lifecycle event: %w", err))
		},
		mutex: sync.Mutex{}, kinds: make(map[string]string),
	}
	owner := operation.NewOwner[struct{}, *uiv1.UICompleted](ctx, delivery)
	tracker := operation.NewTracker[*uiv1.HostProgress, *uiv1.HostCompleted]()
	localClose := make(chan struct{}, 1)
	host := newHost(serviceContext, writer, tracker, func(err error) {
		cancel(fmt.Errorf("deliver UI request: %w", err))
	}, func() {
		select {
		case localClose <- struct{}{}:
		default:
		}
	})
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.Run(ctx) }()
	closing := new(atomic.Bool)
	peerClose := make(chan struct{}, 1)
	receiveDone := make(chan error, 1)
	go func() { receiveDone <- s.receive(ctx, stream, owner, delivery, host, closing, peerClose) }()
	runDone := make(chan error, 1)
	go func() {
		select {
		case <-initialized:
			runDone <- s.service.Run(serviceContext, host)
		case <-serviceContext.Done():
			runDone <- context.Cause(serviceContext)
		}
	}()

	exit := awaitServerLoopExit(ctx, writer, closing, localClose, peerClose, receiveDone, runDone, writerDone)
	if exit.requestedClose {
		cancelService(context.Canceled)
		owner.Close()
		writerFinished, drainErr := waitServerCloseDrain(ctx, receiveDone, writerDone)
		exit.err = errors.Join(exit.err, drainErr)
		exit.writerFinished = exit.writerFinished || writerFinished
	}
	cleanupErr := finishServerOpen(
		cancel, cancelService, owner, tracker, host, writer, runDone, writerDone,
		exit.runFinished, exit.writerFinished, exit.err,
	)
	cleanupErr = errors.Join(cleanupErr, s.service.Close())
	exitErr := withoutClosureLeaves(exit.err)
	if cleanupErr == nil && exitErr == nil {
		return nil
	}
	return streamStatus(fmt.Errorf("run UI operation stream: %w", errors.Join(exitErr, cleanupErr)))
}

// sendUIResponse lets connection cancellation release the writer from a blocked transport send.
func sendUIResponse(
	ctx context.Context,
	response *uiv1.OpenResponse,
	send func(*uiv1.OpenResponse) error,
) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	sent := make(chan error, 1)
	go func() { sent <- send(response) }()
	select {
	case err := <-sent:
		if err != nil {
			return fmt.Errorf("send UI response: %w", err)
		}
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// serverLoopExit describes the first SDK endpoint component that stopped.
type serverLoopExit struct {
	// err is the first loop, service, or caller failure.
	err error
	// runFinished reports that runDone was consumed.
	runFinished bool
	// writerFinished reports that writerDone was consumed.
	writerFinished bool
	// requestedClose distinguishes normal closure from failure cleanup.
	requestedClose bool
}

// awaitServerLoopExit waits for the first SDK closure or failure trigger.
func awaitServerLoopExit(
	ctx context.Context,
	writer *operation.Writer[*uiv1.OpenResponse],
	closing *atomic.Bool,
	localClose <-chan struct{},
	peerClose <-chan struct{},
	receiveDone <-chan error,
	runDone <-chan error,
	writerDone <-chan error,
) serverLoopExit {
	select {
	case err := <-receiveDone:
		return serverLoopExit{err: err, runFinished: false, writerFinished: false, requestedClose: false}
	case err := <-runDone:
		exit := serverLoopExit{err: err, runFinished: true, writerFinished: false, requestedClose: err == nil}
		if err == nil {
			closing.Store(true)
			exit.err = enqueueSDKClose(writer, "close UI connection")
		}
		return exit
	case <-localClose:
		closing.Store(true)
		return serverLoopExit{
			err: enqueueSDKClose(writer, "close UI connection"), runFinished: false,
			writerFinished: false, requestedClose: true,
		}
	case <-peerClose:
		closing.Store(true)
		return serverLoopExit{
			err: enqueueSDKClose(writer, "acknowledge UI connection close"), runFinished: false,
			writerFinished: false, requestedClose: true,
		}
	case err := <-writerDone:
		if err == nil {
			err = errors.New("UI writer stopped before connection closure")
		}
		return serverLoopExit{err: err, runFinished: false, writerFinished: true, requestedClose: false}
	case <-ctx.Done():
		return serverLoopExit{
			err: context.Cause(ctx), runFinished: false, writerFinished: false, requestedClose: false,
		}
	}
}

// enqueueSDKClose queues the endpoint's single close message.
func enqueueSDKClose(writer *operation.Writer[*uiv1.OpenResponse], action string) error {
	response := new(uiv1.OpenResponse)
	response.SetClose(new(operationv1.CloseConnection))
	if err := writer.Enqueue(response); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

// waitServerCloseDrain waits for request EOF or a transport failure during normal closure.
func waitServerCloseDrain(
	ctx context.Context,
	receiveDone <-chan error,
	writerDone <-chan error,
) (bool, error) {
	select {
	case err := <-receiveDone:
		remainingErr := withoutClosureLeaves(err)
		if err != nil && remainingErr == nil {
			return false, nil
		}
		return false, remainingErr
	case err := <-writerDone:
		return true, err
	case <-ctx.Done():
		return false, context.Cause(ctx)
	}
}

// finishServerOpen cancels and joins service-owned resources.
func finishServerOpen(
	cancel context.CancelCauseFunc,
	cancelService context.CancelCauseFunc,
	owner *operation.Owner[struct{}, *uiv1.UICompleted],
	tracker *operation.Tracker[*uiv1.HostProgress, *uiv1.HostCompleted],
	host *Host,
	writer *operation.Writer[*uiv1.OpenResponse],
	runDone <-chan error,
	writerDone <-chan error,
	runFinished bool,
	writerFinished bool,
	cause error,
) error {
	cancelService(context.Canceled)
	var result error
	if !runFinished {
		if err := withoutClosureLeaves(<-runDone); err != nil {
			result = errors.Join(result, err)
		}
	}
	if cause != nil {
		cancel(cause)
	}
	owner.Close()
	tracker.Close()
	host.closeEvents()
	writer.Close()
	if !writerFinished {
		if err := withoutClosureLeaves(<-writerDone); err != nil {
			result = errors.Join(result, err)
		}
	}
	cancel(context.Canceled)
	return result
}

// withoutClosureLeaves removes only pure cancellation and EOF leaves from joined errors.
func withoutClosureLeaves(err error) error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		remaining := make([]error, 0, len(joined.Unwrap()))
		for _, cause := range joined.Unwrap() {
			if filtered := withoutClosureLeaves(cause); filtered != nil {
				remaining = append(remaining, filtered)
			}
		}
		return errors.Join(remaining...)
	}
	cause := errors.Unwrap(err)
	if cause != nil {
		if !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) &&
			!errors.Is(cause, io.EOF) {
			return err
		}
		filtered := withoutClosureLeaves(cause)
		if filtered == nil {
			return nil
		}
		return fmt.Errorf("%s: %w", err.Error(), filtered)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// receive handles Host messages while accepted UI work runs independently.
func (s *server) receive(
	ctx context.Context,
	stream grpc.BidiStreamingServer[uiv1.OpenRequest, uiv1.OpenResponse],
	owner *operation.Owner[struct{}, *uiv1.UICompleted],
	delivery *uiDelivery,
	host *Host,
	closing *atomic.Bool,
	peerClose chan<- struct{},
) error {
	for {
		request, err := stream.Recv()
		if err != nil {
			return err
		}
		if request.GetClose() != nil {
			if request.GetOperationId() != "" {
				return protocolFault(errors.New("receive Host close: operation identifier must be empty"))
			}
			host.markPeerClosed()
			if closing.CompareAndSwap(false, true) {
				peerClose <- struct{}{}
			}
			continue
		}
		if request.GetConnectionEvent() != nil && request.GetOperationId() != "" {
			return protocolFault(errors.New("receive Host connection event: operation identifier must be empty"))
		}
		if request.GetRequest() != nil && request.GetOperationId() == "" {
			if handleErr := s.handleRequest(ctx, request, owner, delivery, host); handleErr != nil {
				return protocolFault(handleErr)
			}
			continue
		}
		if closing.Load() && request.GetRequest() != nil {
			return protocolFault(errors.New("receive Host operation: connection is closing"))
		}
		if handleErr := s.handleRequest(ctx, request, owner, delivery, host); handleErr != nil {
			return protocolFault(handleErr)
		}
	}
}

// handleRequest validates and dispatches one Host stream message.
func (s *server) handleRequest(
	ctx context.Context,
	message *uiv1.OpenRequest,
	owner *operation.Owner[struct{}, *uiv1.UICompleted],
	delivery *uiDelivery,
	host *Host,
) error {
	if message == nil {
		return errors.New("UI stream message is required")
	}
	switch message.WhichContent() {
	case uiv1.OpenRequest_Request_case:
		return s.handleHostOperation(ctx, message.GetOperationId(), message.GetRequest(), owner, delivery)
	case uiv1.OpenRequest_Event_case:
		return host.handleOperationEvent(message.GetOperationId(), message.GetEvent())
	case uiv1.OpenRequest_ConnectionEvent_case:
		return host.deliverConnectionEvent(message.GetConnectionEvent())
	case uiv1.OpenRequest_Close_case:
		host.markPeerClosed()
		return io.EOF
	case uiv1.OpenRequest_Content_not_set_case:
		return errors.New("UI stream message content is required")
	default:
		return errors.New("UI stream message content is unknown")
	}
}

// handleHostOperation prepares initialization or cancellation without blocking receipt.
func (s *server) handleHostOperation(
	ctx context.Context,
	id string,
	request *uiv1.HostRequest,
	owner *operation.Owner[struct{}, *uiv1.UICompleted],
	delivery *uiDelivery,
) error {
	if id == "" {
		return s.reject(id, "INVALID_ARGUMENT", errors.New("UI operation identifier is required"), delivery)
	}
	if request == nil {
		return s.reject(id, "INVALID_ARGUMENT", errors.New("UI operation request is required"), delivery)
	}
	var startErr error
	kind := unknownSDKOperationKind
	switch request.WhichRequest() {
	case uiv1.HostRequest_Initialize_case:
		kind = "initialize"
		delivery.setKind(id, kind)
		startErr = owner.Start(id, func() (operation.Prepared[struct{}, *uiv1.UICompleted], error) {
			prepared, err := s.service.PrepareInitialize(ctx, request.GetInitialize())
			if err != nil {
				return nil, err
			}
			return &initializePrepared{operation: prepared}, nil
		})
	case uiv1.HostRequest_Cancel_case:
		kind = "cancel"
		delivery.setKind(id, kind)
		target := request.GetCancel().GetTargetOperationId()
		if target == "" {
			startErr = Reject("INVALID_ARGUMENT", errors.New("UI cancellation target is required"))
		} else if cancelTarget, active := owner.Cancellation(target); !active {
			startErr = Reject("TARGET_NOT_ACTIVE", fmt.Errorf("UI operation %q is not active", target))
		} else {
			startErr = owner.Start(id, func() (operation.Prepared[struct{}, *uiv1.UICompleted], error) {
				return &cancellationPrepared{cancel: cancelTarget, target: target}, nil
			})
		}
	case uiv1.HostRequest_Request_not_set_case:
		startErr = Reject("INVALID_ARGUMENT", errors.New("UI operation kind is required"))
	default:
		startErr = Reject("INVALID_ARGUMENT", errors.New("UI operation kind is unknown"))
	}
	if startErr == nil {
		return nil
	}
	if kind != unknownSDKOperationKind {
		delivery.takeKind(id)
	}
	if rejection, ok := errors.AsType[*RejectionError](startErr); ok {
		return s.reject(id, rejection.Code(), startErr, delivery)
	}
	if errors.Is(startErr, operation.ErrIdentifierInUse) {
		return s.reject(id, "OPERATION_ID_IN_USE", startErr, delivery)
	}
	return fmt.Errorf("prepare UI operation %q: %w", id, startErr)
}

// reject sends a non-operation rejection through the writer queue.
func (s *server) reject(id, code string, cause error, delivery *uiDelivery) error {
	event := new(uiv1.UIEvent)
	event.SetRejected(operationv1.Rejected_builder{Code: new(code), Message: new(cause.Error())}.Build())
	return delivery.writer.Enqueue(uiEventResponse(id, event))
}

// mapHostEvent maps one generated lifecycle event into the shared tracker.
func mapHostEvent(id string, event *uiv1.HostEvent) operation.Event[*uiv1.HostProgress, *uiv1.HostCompleted] {
	mapped := operation.Event[*uiv1.HostProgress, *uiv1.HostCompleted]{
		ID: id, Kind: 0, Progress: nil, Result: nil, Code: "", Message: "",
	}
	if event == nil {
		return mapped
	}
	switch event.WhichEvent() {
	case uiv1.HostEvent_Accepted_case:
		mapped.Kind = operation.EventAccepted
	case uiv1.HostEvent_Running_case:
		mapped.Kind = operation.EventRunning
	case uiv1.HostEvent_Progress_case:
		mapped.Kind, mapped.Progress = operation.EventProgress, event.GetProgress()
	case uiv1.HostEvent_Completed_case:
		mapped.Kind, mapped.Result = operation.EventCompleted, event.GetCompleted()
	case uiv1.HostEvent_Canceled_case:
		mapped.Kind = operation.EventCanceled
	case uiv1.HostEvent_Failed_case:
		failure := event.GetFailed()
		mapped.Kind, mapped.Code, mapped.Message = operation.EventFailed, failure.GetCode(), failure.GetMessage()
	case uiv1.HostEvent_Rejected_case:
		rejection := event.GetRejected()
		mapped.Kind, mapped.Code, mapped.Message = operation.EventRejected, rejection.GetCode(), rejection.GetMessage()
	case uiv1.HostEvent_Event_not_set_case:
		mapped.Kind = 0
	default:
		mapped.Kind = 0
	}
	return mapped
}
