package extensionv1

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// serverReceive contains one asynchronous stream receive result.
type serverReceive struct {
	// request contains one received stream message.
	request *extensionpb.OpenRequest
	// err contains the receive result.
	err error
}

// Open receives Host requests without waiting for accepted operation work.
func (s *server) Open(stream extensionpb.ExtensionService_OpenServer) error {
	ctx, cancel := context.WithCancelCause(stream.Context())
	defer cancel(context.Canceled)
	var failConnection func(error)
	writer := operation.NewWriter(func(response *extensionpb.OpenResponse) error {
		if err := stream.Send(response); err != nil {
			mapped := mapTransportError(err)
			if failConnection != nil {
				failConnection(mapped)
			}
			return mapped
		}
		if registration := response.GetEvent().GetCompleted().GetRegister(); registration != nil {
			s.completeRegistration(registration)
		}
		return nil
	})
	var owner *operation.Owner[*extensionpb.ToolProgress, extensionResult]
	fail := func(err error) {
		owner.Fail(err)
		cancel(err)
	}
	failConnection = fail
	delivery := &extensionDelivery{
		ctx: ctx, writer: writer, fail: fail,
		mutex: sync.Mutex{}, invocations: make(map[string]invocationLog),
		initiator: newContextInitiator(ctx, writer, fail),
	}
	owner = operation.NewOwner[*extensionpb.ToolProgress, extensionResult](ctx, delivery)

	writerResult := make(chan error, 1)
	go func() { writerResult <- writer.Run(ctx) }()
	receiveResult := make(chan serverReceive, 1)
	go receiveServerRequests(ctx, stream, receiveResult)

	var closeOnce sync.Once
	ownerClosed := make(chan struct{})
	startOwnerClose := func() {
		closeOnce.Do(func() {
			go func() {
				delivery.initiator.close(context.Canceled)
				owner.Close()
				close(ownerClosed)
			}()
		})
	}

	connectionLoop := &serverConnectionLoop{
		server: s, ctx: ctx, cancel: cancel, owner: owner, delivery: delivery, writer: writer,
		writerResult: writerResult, receiveResult: receiveResult,
		startOwnerClose: startOwnerClose, ownerClosed: ownerClosed,
	}
	result := connectionLoop.run()
	return joinOutputSources(result, owner.SourceErrors(), writer.SourceErrors())
}

// serverConnectionLoop coordinates receive, writer, and operation-owner completion.
type serverConnectionLoop struct {
	// server validates and admits received requests.
	server *server
	// ctx carries the first connection failure cause.
	ctx context.Context
	// cancel stops all connection work.
	cancel context.CancelCauseFunc
	// owner owns plugin operations.
	owner *operation.Owner[*extensionpb.ToolProgress, extensionResult]
	// delivery maps operation lifecycle responses.
	delivery *extensionDelivery
	// writer owns response transport.
	writer *operation.Writer[*extensionpb.OpenResponse]
	// writerResult reports writer completion.
	writerResult <-chan error
	// receiveResult reports request receipt.
	receiveResult <-chan serverReceive
	// startOwnerClose starts operation cancellation and joining once.
	startOwnerClose func()
	// ownerClosed closes after all plugin work stops.
	ownerClosed <-chan struct{}
}

// run processes connection events until clean EOF or failure.
func (loop *serverConnectionLoop) run() error {
	closing := false
	for {
		select {
		case writerErr := <-loop.writerResult:
			if writerErr == nil {
				writerErr = errors.New("extension writer stopped before connection closure")
			}
			return finishServerFailure(
				writerErr,
				loop.owner,
				loop.cancel,
				loop.startOwnerClose,
				loop.ownerClosed,
				loop.writer,
				nil,
			)
		case received := <-loop.receiveResult:
			if connectionErr := context.Cause(loop.ctx); connectionErr != nil {
				return finishServerFailure(
					connectionErr,
					loop.owner,
					loop.cancel,
					loop.startOwnerClose,
					loop.ownerClosed,
					loop.writer,
					loop.writerResult,
				)
			}
			if errors.Is(received.err, io.EOF) {
				return finishServerEOF(loop.startOwnerClose, loop.ownerClosed, loop.writer, loop.writerResult)
			}
			if received.err != nil {
				receiveErr := mapServerReceiveError(received.err)
				return finishServerFailure(
					receiveErr,
					loop.owner,
					loop.cancel,
					loop.startOwnerClose,
					loop.ownerClosed,
					loop.writer,
					loop.writerResult,
				)
			}
			var err error
			closing, err = loop.routeEnvelope(received.request, closing)
			if err != nil {
				return finishServerFailure(
					err,
					loop.owner,
					loop.cancel,
					loop.startOwnerClose,
					loop.ownerClosed,
					loop.writer,
					loop.writerResult,
				)
			}
		}
	}
}

// routeEnvelope validates one envelope while keeping operation execution off stream receipt.
func (loop *serverConnectionLoop) routeEnvelope(request *extensionpb.OpenRequest, closing bool) (bool, error) {
	if request.GetEvent() != nil {
		// Close settles pending waits; already-queued Host events cannot reactivate them.
		if closing {
			return true, nil
		}
		err := loop.delivery.initiator.handle(request.GetOperationId(), request.GetEvent())
		if err == nil {
			return false, nil
		}
		if errors.Is(err, operation.ErrQueueFull) {
			return false, mapDeliveryError(err)
		}
		return false, newProtocolStatusError(codes.FailedPrecondition, err.Error(), err)
	}
	if closing {
		cause := errors.New("extension request received after close")
		return true, newProtocolStatusError(codes.FailedPrecondition, cause.Error(), cause)
	}
	if request.GetClose() != nil {
		if request.GetOperationId() != "" || request.GetRequest() != nil {
			cause := errors.New("extension close message is invalid")
			return false, newProtocolStatusError(codes.FailedPrecondition, cause.Error(), cause)
		}
		loop.startOwnerClose()
		return true, nil
	}
	return false, loop.server.handleRequest(loop.ctx, loop.owner, loop.delivery, request)
}

// finishServerFailure stops transport work and joins all SDK-owned operations.
func finishServerFailure(
	err error,
	owner *operation.Owner[*extensionpb.ToolProgress, extensionResult],
	cancel context.CancelCauseFunc,
	startOwnerClose func(),
	ownerClosed <-chan struct{},
	writer *operation.Writer[*extensionpb.OpenResponse],
	writerResult <-chan error,
) error {
	owner.Fail(err)
	cancel(err)
	startOwnerClose()
	<-ownerClosed
	writer.Close()
	if writerResult != nil {
		if writerErr := <-writerResult; writerErr != nil && !errors.Is(err, writerErr) {
			err = errors.Join(err, writerErr)
		}
	}
	return err
}

// finishServerEOF joins operations and drains queued responses after clean request EOF.
func finishServerEOF(
	startOwnerClose func(),
	ownerClosed <-chan struct{},
	writer *operation.Writer[*extensionpb.OpenResponse],
	writerResult <-chan error,
) error {
	startOwnerClose()
	<-ownerClosed
	writer.Close()
	writerErr := <-writerResult
	if errors.Is(writerErr, context.Canceled) {
		return withoutClosureLeaves(writerErr)
	}
	return writerErr
}

// receiveServerRequests keeps transport receipt independent from operation work and writer completion.
func receiveServerRequests(
	ctx context.Context,
	stream extensionpb.ExtensionService_OpenServer,
	results chan<- serverReceive,
) {
	for {
		request, err := stream.Recv()
		select {
		case results <- serverReceive{request: request, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

// mapServerReceiveError preserves statuses and classifies plain decode and transport errors.
func mapServerReceiveError(err error) error {
	if _, present := status.FromError(err); present {
		return err
	}
	if strings.HasPrefix(err.Error(), "proto:") {
		return newProtocolStatusError(codes.InvalidArgument, err.Error(), err)
	}
	return mapDeliveryError(err)
}

// mapTransportError preserves statuses and maps plain transport failures to Unavailable.
func mapTransportError(err error) error { return mapDeliveryError(err) }
