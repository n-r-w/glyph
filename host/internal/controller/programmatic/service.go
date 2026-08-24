package programmatic

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// SessionCompletionCause identifies the owning stream terminal cause.
type SessionCompletionCause uint8

// Session completion causes classify owner termination.
const (
	SessionCompletionUnspecified SessionCompletionCause = iota
	SessionCompletionCleanClientClosure
	SessionCompletionApplicationCanceled
	SessionCompletionProtocolFailure
	SessionCompletionTransportFailure
	SessionCompletionCleanupFailure
)

// SessionCompletion reports one owner result after all controller work joins.
type SessionCompletion struct {
	Cause      SessionCompletionCause
	Err        error
	CleanupErr error
}

// Service owns one Programmatic Control stream and its serialized sends.
type Service struct {
	programmaticv1.UnimplementedProgrammaticControlServiceServer

	applicationContext context.Context
	session            HostSession
	ownerClaimed       atomic.Bool
	sendMutex          sync.Mutex
	completions        chan SessionCompletion
}

var _ programmaticv1.ProgrammaticControlServiceServer = (*Service)(nil)

// New creates a Programmatic Control gRPC service.
func New(applicationContext context.Context, session HostSession) *Service {
	return &Service{
		UnimplementedProgrammaticControlServiceServer: programmaticv1.UnimplementedProgrammaticControlServiceServer{},
		applicationContext:                            applicationContext,
		session:                                       session,
		ownerClaimed:                                  atomic.Bool{},
		sendMutex:                                     sync.Mutex{},
		completions:                                   make(chan SessionCompletion, 1),
	}
}

// Completions returns the single owner session result stream.
func (s *Service) Completions() <-chan SessionCompletion {
	return s.completions
}

// Open serves the single owning Programmatic Control stream.
func (s *Service) Open(stream grpc.BidiStreamingServer[
	programmaticv1.OpenRequest,
	programmaticv1.OpenResponse,
]) error {
	return s.open(stream)
}

type terminalResult struct {
	cause       SessionCompletionCause
	err         error
	clean       bool
	passthrough bool
}

func (s *Service) open(stream OpenStream) error {
	if !s.ownerClaimed.CompareAndSwap(false, true) {
		return status.Error(codes.FailedPrecondition, "a Programmatic Control stream already owns this process")
	}

	controllerContext, cancelController := context.WithCancel(stream.Context())
	eventTerminals := make(chan terminalResult, 1)
	receiveTerminals := make(chan terminalResult, 1)
	var commandWork sync.Mutex
	var eventWork sync.WaitGroup
	if s.applicationContext.Err() == nil && stream.Context().Err() == nil {
		go func() {
			receiveTerminals <- s.receive(
				controllerContext, stream, eventTerminals, &commandWork, &eventWork,
			)
		}()
	}

	terminal := s.waitForTerminal(stream.Context(), eventTerminals, receiveTerminals)
	cancelController()
	commandWork.Lock()
	// Cleanup uses a context that remains active after application cancellation.
	cleanupErr := s.session.CancelAndWait(context.WithoutCancel(s.applicationContext))
	commandWork.Unlock()
	eventWork.Wait()
	completion, rpcErr := s.complete(stream.Context(), terminal, cleanupErr)
	s.completions <- completion
	return rpcErr
}

func (s *Service) waitForTerminal(
	streamContext context.Context,
	eventTerminals <-chan terminalResult,
	receiveTerminals <-chan terminalResult,
) terminalResult {
	var selected terminalResult
	select {
	case <-s.applicationContext.Done():
		selected = terminalResult{
			cause: SessionCompletionApplicationCanceled,
			err:   s.applicationContext.Err(), clean: false, passthrough: true,
		}
	case <-streamContext.Done():
		selected = terminalResult{
			cause: SessionCompletionCleanClientClosure, err: nil, clean: true, passthrough: false,
		}
	case selected = <-eventTerminals:
	case selected = <-receiveTerminals:
	}
	return s.applyTerminalPrecedence(streamContext, selected, receiveTerminals)
}

func (s *Service) applyTerminalPrecedence(
	streamContext context.Context,
	selected terminalResult,
	receiveTerminals <-chan terminalResult,
) terminalResult {
	if err := s.applicationContext.Err(); err != nil {
		return terminalResult{
			cause: SessionCompletionApplicationCanceled, err: err, clean: false, passthrough: true,
		}
	}
	if selected.clean || streamContext.Err() != nil {
		return terminalResult{
			cause: SessionCompletionCleanClientClosure, err: nil, clean: true, passthrough: false,
		}
	}
	select {
	case received := <-receiveTerminals:
		if received.clean {
			return received
		}
	default:
	}
	return selected
}

func (s *Service) receive(
	controllerContext context.Context,
	stream OpenStream,
	eventTerminals chan terminalResult,
	commandWork *sync.Mutex,
	eventWork *sync.WaitGroup,
) terminalResult {
	for {
		request, recvErr := stream.Recv()
		if recvErr != nil {
			return s.receiveError(stream.Context(), recvErr)
		}

		commandWork.Lock()
		if controllerContext.Err() != nil {
			commandWork.Unlock()
			return terminalResult{
				cause: SessionCompletionCleanClientClosure, err: nil, clean: true, passthrough: false,
			}
		}
		terminal, done := s.handleRequest(
			controllerContext, stream, request, eventTerminals, eventWork,
		)
		commandWork.Unlock()
		if done {
			return terminal
		}
	}
}

func (s *Service) handleRequest(
	controllerContext context.Context,
	stream OpenStream,
	request *programmaticv1.OpenRequest,
	eventTerminals chan terminalResult,
	eventWork *sync.WaitGroup,
) (terminalResult, bool) {
	command, err := mapOpenRequest(request)
	if err != nil {
		return terminalResult{
			cause: SessionCompletionProtocolFailure, err: err, clean: false,
			passthrough: status.Code(err) == codes.InvalidArgument,
		}, true
	}
	response, operation, err := s.session.Handle(controllerContext, command)
	if err != nil {
		return terminalResult{
			cause: SessionCompletionProtocolFailure, err: err, clean: false, passthrough: false,
		}, true
	}
	if (response.Kind == ResponseUserRequestAccepted) != (operation != nil) {
		return terminalResult{
			cause: SessionCompletionProtocolFailure,
			err:   errors.New("host acceptance and operation presence differ"),
			clean: false, passthrough: false,
		}, true
	}
	var events <-chan AgentEvent
	if operation != nil {
		events = operation.Events()
		if events == nil {
			return terminalResult{
				cause: SessionCompletionProtocolFailure,
				err:   errors.New("host operation returned a nil event stream"),
				clean: false, passthrough: false,
			}, true
		}
	}
	sendTerminal := s.sendResponse(stream, response)
	if sendTerminal.err != nil {
		return sendTerminal, true
	}
	if operation == nil {
		return terminalResult{cause: 0, err: nil, clean: false, passthrough: false}, false
	}
	eventWork.Add(1)
	go s.consumeEvents(stream, events, eventTerminals, eventWork)
	// Acceptance is sent and the event consumer is ready before execution starts.
	operation.Start()
	return terminalResult{cause: 0, err: nil, clean: false, passthrough: false}, false
}

func (s *Service) receiveError(streamContext context.Context, recvErr error) terminalResult {
	if errors.Is(recvErr, io.EOF) || streamContext.Err() != nil {
		return terminalResult{
			cause: SessionCompletionCleanClientClosure, err: nil, clean: true, passthrough: false,
		}
	}
	return terminalResult{
		cause: SessionCompletionTransportFailure, err: recvErr, clean: false, passthrough: true,
	}
}

func (s *Service) consumeEvents(
	stream OpenStream,
	events <-chan AgentEvent,
	terminals chan<- terminalResult,
	work *sync.WaitGroup,
) {
	defer work.Done()
	for event := range events {
		mapped, err := mapEvent(event)
		if err != nil {
			s.publishEventTerminal(terminals, terminalResult{
				cause: SessionCompletionProtocolFailure, err: err, clean: false, passthrough: false,
			})
			return
		}
		if err = s.send(stream, mapped); err != nil {
			s.publishEventTerminal(terminals, terminalResult{
				cause: SessionCompletionTransportFailure, err: err, clean: false, passthrough: true,
			})
			return
		}
	}
}

func (*Service) publishEventTerminal(terminals chan<- terminalResult, terminal terminalResult) {
	select {
	case terminals <- terminal:
	default:
	}
}

func (s *Service) sendResponse(stream OpenStream, response Response) terminalResult {
	mapped, err := mapResponse(response)
	if err != nil {
		return terminalResult{
			cause: SessionCompletionProtocolFailure, err: err, clean: false, passthrough: false,
		}
	}
	if err = s.send(stream, mapped); err != nil {
		return terminalResult{
			cause: SessionCompletionTransportFailure, err: err, clean: false, passthrough: true,
		}
	}
	return terminalResult{cause: 0, err: nil, clean: false, passthrough: false}
}

func (s *Service) send(stream OpenStream, response *programmaticv1.OpenResponse) error {
	s.sendMutex.Lock()
	defer s.sendMutex.Unlock()
	return stream.Send(response)
}

func (s *Service) complete(
	streamContext context.Context,
	terminal terminalResult,
	cleanupErr error,
) (SessionCompletion, error) {
	completion := SessionCompletion{Cause: terminal.cause, Err: terminal.err, CleanupErr: cleanupErr}
	if err := s.applicationContext.Err(); err != nil {
		completion.Cause = SessionCompletionApplicationCanceled
		completion.Err = err
		return completion, status.FromContextError(err).Err()
	}
	if streamContext.Err() != nil || terminal.clean {
		if cleanupErr != nil {
			completion.Cause = SessionCompletionCleanupFailure
			completion.Err = cleanupErr
			return completion, status.Error(codes.Internal, "clean up Programmatic Control session")
		}
		completion.Cause = SessionCompletionCleanClientClosure
		completion.Err = nil
		return completion, nil
	}
	if terminal.cause == SessionCompletionTransportFailure && terminal.passthrough {
		if _, ok := status.FromError(terminal.err); ok {
			return completion, terminal.err
		}
	}
	if terminal.cause == SessionCompletionProtocolFailure && terminal.passthrough {
		return completion, terminal.err
	}
	return completion, status.Error(codes.Internal, "Programmatic Control controller failed")
}
