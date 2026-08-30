// Package plugin maps the public UI contract to the standard terminal presentation.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// Controller maps the public UI stream to one terminal presentation program.
type Controller struct {
	// UnimplementedUIServiceServer provides forward-compatible gRPC defaults.
	uiv1.UnimplementedUIServiceServer

	// terminal opens controlling-terminal sessions.
	terminal Terminal
	// programs creates terminal presentation programs.
	programs ProgramFactory
}

var _ uiv1.UIServiceServer = (*Controller)(nil)

// New creates the standard TUI plugin controller.
func New(terminal Terminal, programs ProgramFactory) *Controller {
	return &Controller{
		UnimplementedUIServiceServer: uiv1.UnimplementedUIServiceServer{},
		terminal:                     terminal,
		programs:                     programs,
	}
}

// GetCapabilities returns immutable capabilities without opening the terminal.
func (*Controller) GetCapabilities(
	context.Context,
	*uiv1.GetCapabilitiesRequest,
) (*uiv1.GetCapabilitiesResponse, error) {
	return uiv1.GetCapabilitiesResponse_builder{
		ControlsTerminal: new(true),
	}.Build(), nil
}

// Open runs one initialized Host-to-TUI stream until either side completes.
func (controller *Controller) Open(
	stream grpc.BidiStreamingServer[uiv1.OpenRequest, uiv1.OpenResponse],
) (returnErr error) {
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, "receive initialization: %v", err)
	}
	if first.GetInitialization() == nil {
		return status.Error(codes.FailedPrecondition, "initialization must be the first UI frame")
	}
	initial, err := mapRequest(first)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "map initialization: %v", err)
	}

	terminalSession, err := controller.terminal.Open()
	if err != nil {
		return status.Errorf(codes.Internal, "open terminal: %v", err)
	}
	defer func() {
		if closeErr := terminalSession.Close(); closeErr != nil && returnErr == nil {
			returnErr = status.Errorf(codes.Internal, "close terminal: %v", closeErr)
		}
	}()

	var sendMutex sync.Mutex
	emit := func(command presentationdomain.Command) error {
		response, mapErr := mapCommand(command)
		if mapErr != nil {
			return mapErr
		}
		sendMutex.Lock()
		defer sendMutex.Unlock()
		if sendErr := stream.Send(response); sendErr != nil {
			return fmt.Errorf("send UI command: %w", sendErr)
		}
		return nil
	}
	program := controller.programs.New(initial, terminalSession.Input(), terminalSession.Output(), emit)
	programResult := make(chan error, 1)
	go func() {
		programResult <- program.Run()
	}()
	receiveResult := make(chan error, 1)
	go receiveFrames(stream, program, receiveResult)

	select {
	case runErr := <-programResult:
		if runErr != nil {
			return status.Errorf(codes.Internal, "run TUI: %v", runErr)
		}
		return nil
	case receiveErr := <-receiveResult:
		program.Quit()
		runErr := <-programResult
		if runErr != nil {
			return status.Errorf(codes.Internal, "run TUI: %v", runErr)
		}
		if errors.Is(receiveErr, io.EOF) {
			return nil
		}
		return receiveErr
	}
}

// receiveFrames maps the ordered Host stream into the active presentation program.
func receiveFrames(
	stream grpc.BidiStreamingServer[uiv1.OpenRequest, uiv1.OpenResponse],
	program Program,
	result chan<- error,
) {
	for {
		request, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				result <- io.EOF
			} else {
				result <- status.Errorf(codes.Unavailable, "receive Host frame: %v", err)
			}
			return
		}
		event, err := mapRequest(request)
		if err != nil {
			result <- status.Errorf(codes.InvalidArgument, "map Host frame: %v", err)
			return
		}
		if event.Kind == presentationdomain.EventInitialization {
			result <- status.Error(codes.FailedPrecondition, "initialization may only be sent once")
			return
		}
		program.Send(event)
	}
}
