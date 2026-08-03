package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	protocolv1 "github.com/n-r-w/glyph/experiments/plugin-runtime-spike/protocol/v1"
)

// terminalSize is one size notification observed inside the UI process.
type terminalSize struct {
	width  int
	height int
}

// terminalModel reports Bubble Tea resize events without owning Host behavior.
type terminalModel struct {
	sizes chan<- terminalSize
}

// Init performs no work because terminal initialization precedes the first update.
func (_ terminalModel) Init() tea.Cmd {
	return nil
}

// Update forwards size changes so the gRPC stream can prove resize delivery.
func (m terminalModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := message.(tea.WindowSizeMsg); ok {
		select {
		case m.sizes <- terminalSize{width: size.Width, height: size.Height}:
		default:
		}
	}
	return m, nil
}

// View renders a visible alternate-screen marker during the terminal check.
func (_ terminalModel) View() tea.View {
	view := tea.NewView("Glyph plugin runtime spike\n")
	view.AltScreen = true
	return view
}

// uiService implements the persistent UI lifecycle stream.
type uiService struct {
	protocolv1.UnimplementedUIServiceServer
	terminal bool
}

// Describe reports terminal ownership without opening the terminal or lifecycle stream.
func (s *uiService) Describe(
	_ context.Context,
	_ *protocolv1.DescribeRequest,
) (*protocolv1.DescribeResponse, error) {
	return &protocolv1.DescribeResponse{UsesTerminal: s.terminal}, nil
}

// Open selects the headless transport check or the controlling-terminal check.
func (s *uiService) Open(stream protocolv1.UIService_OpenServer) error {
	if !s.terminal {
		return s.openWithoutTerminal(stream)
	}
	return s.openWithTerminal(stream)
}

// openWithoutTerminal proves bidirectional stream behavior without touching a TTY.
func (_ *uiService) openWithoutTerminal(stream protocolv1.UIService_OpenServer) error {
	if err := stream.Send(&protocolv1.OpenResponse{Event: protocolv1.UIEvent_UI_EVENT_READY}); err != nil {
		return fmt.Errorf("send UI ready event: %w", err)
	}

	for {
		request, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("receive UI command: %w", err)
		}

		switch request.Command {
		case protocolv1.UICommand_UI_COMMAND_ECHO:
			if err := stream.Send(&protocolv1.OpenResponse{
				Event: protocolv1.UIEvent_UI_EVENT_ECHOED,
				Text:  "echo",
			}); err != nil {
				return fmt.Errorf("send UI echo event: %w", err)
			}
		case protocolv1.UICommand_UI_COMMAND_QUIT:
			if err := stream.Send(&protocolv1.OpenResponse{Event: protocolv1.UIEvent_UI_EVENT_EXITED}); err != nil {
				return fmt.Errorf("send UI exit event: %w", err)
			}
			return nil
		case protocolv1.UICommand_UI_COMMAND_CRASH:
			os.Exit(crashExitCode)
		default:
			return fmt.Errorf("receive UI command: unknown command %s", request.Command)
		}
	}
}

// openWithTerminal proves that Bubble Tea owns the controlling terminal inside the plugin process.
func (_ *uiService) openWithTerminal(stream protocolv1.UIService_OpenServer) (returnErr error) {
	input, output, err := tea.OpenTTY()
	if err != nil {
		return fmt.Errorf("open controlling terminal: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, closeTerminalFiles(input, output))
	}()

	sizes := make(chan terminalSize, 4)
	program := tea.NewProgram(
		terminalModel{sizes: sizes},
		tea.WithInput(input),
		tea.WithOutput(output),
	)
	runErrors := make(chan error, 1)
	go func() {
		_, runErr := program.Run()
		runErrors <- runErr
	}()

	requests := make(chan *protocolv1.OpenRequest)
	receiveErrors := make(chan error, 1)
	go receiveUIRequests(stream, requests, receiveErrors)

	ready := false
	for {
		select {
		case size := <-sizes:
			if !ready {
				if err := stream.Send(&protocolv1.OpenResponse{Event: protocolv1.UIEvent_UI_EVENT_READY}); err != nil {
					program.Kill()
					return fmt.Errorf("send terminal UI ready event: %w", err)
				}
				ready = true
			}
			if err := stream.Send(&protocolv1.OpenResponse{
				Event: protocolv1.UIEvent_UI_EVENT_RESIZED,
				Text:  fmt.Sprintf("%dx%d", size.width, size.height),
			}); err != nil {
				program.Kill()
				return fmt.Errorf("send terminal UI size event: %w", err)
			}
		case request := <-requests:
			switch request.Command {
			case protocolv1.UICommand_UI_COMMAND_ECHO:
				if err := stream.Send(&protocolv1.OpenResponse{
					Event: protocolv1.UIEvent_UI_EVENT_ECHOED,
					Text:  "echo",
				}); err != nil {
					program.Kill()
					return fmt.Errorf("send terminal UI echo event: %w", err)
				}
			case protocolv1.UICommand_UI_COMMAND_QUIT:
				program.Quit()
				if err := <-runErrors; err != nil {
					return fmt.Errorf("stop terminal UI: %w", err)
				}
				if err := stream.Send(&protocolv1.OpenResponse{Event: protocolv1.UIEvent_UI_EVENT_EXITED}); err != nil {
					return fmt.Errorf("send terminal UI exit event: %w", err)
				}
				return nil
			case protocolv1.UICommand_UI_COMMAND_CRASH:
				// os.Exit bypasses Bubble Tea cleanup and exposes terminal behavior after hard failure.
				os.Exit(crashExitCode)
			default:
				program.Kill()
				return fmt.Errorf("receive terminal UI command: unknown command %s", request.Command)
			}
		case err := <-receiveErrors:
			program.Kill()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("receive terminal UI command: %w", err)
		case err := <-runErrors:
			if err != nil {
				return fmt.Errorf("run terminal UI: %w", err)
			}
			return fmt.Errorf("run terminal UI: program exited before a Host command")
		case <-stream.Context().Done():
			program.Kill()
			return fmt.Errorf("run terminal UI: %w", stream.Context().Err())
		}
	}
}

// receiveUIRequests serializes stream receives into the terminal event loop.
func receiveUIRequests(
	stream protocolv1.UIService_OpenServer,
	requests chan<- *protocolv1.OpenRequest,
	errorsChannel chan<- error,
) {
	for {
		request, err := stream.Recv()
		if err != nil {
			errorsChannel <- err
			return
		}
		select {
		case requests <- request:
		case <-stream.Context().Done():
			return
		}
	}
}

// closeTerminalFiles releases both controlling-terminal descriptors after Bubble Tea shutdown.
func closeTerminalFiles(input, output *os.File) error {
	return errors.Join(closeTerminalFile(input), closeTerminalFile(output))
}

// closeTerminalFile makes cleanup idempotent because Bubble Tea can close its input reader first.
func closeTerminalFile(file *os.File) error {
	err := file.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}
