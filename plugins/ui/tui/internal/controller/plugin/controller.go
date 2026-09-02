// Package plugin maps the public UI SDK to the standard terminal presentation.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const operationIDTemplate = "tui-%d"

// initializedApplication contains presentation state validated by initialization work.
type initializedApplication struct {
	// initial is the mapped startup presentation event.
	initial presentationdomain.Event
	// terminalSession is the opened presentation resource awaiting Service.Run ownership.
	terminalSession TerminalSession
}

// Controller owns standard TUI initialization, operation identifiers, and foreground tracking.
type Controller struct {
	// terminal opens controlling-terminal sessions.
	terminal Terminal
	// programs creates terminal presentation programs.
	programs ProgramFactory
	// mutex protects initialization and foreground ownership.
	mutex sync.Mutex
	// preparing reserves the single initialization operation.
	preparing bool
	// initialized contains resources opened by successful initialization.
	initialized *initializedApplication
	// foreground identifies the current cancelable Host operation.
	foreground string
	// sequence assigns unique operation identifiers.
	sequence atomic.Uint64
}

var _ uisdk.Service = (*Controller)(nil)

// New creates the standard TUI plugin controller.
func New(terminal Terminal, programs ProgramFactory) *Controller {
	return &Controller{
		terminal: terminal, programs: programs, mutex: sync.Mutex{}, preparing: false,
		initialized: nil, foreground: "", sequence: atomic.Uint64{},
	}
}

// initializeOperation owns one admitted TUI initialization.
type initializeOperation struct {
	// controller receives the opened application resources.
	controller *Controller
	// initial contains fully mapped Host startup state.
	initial presentationdomain.Event
	// release limits admission release to one call.
	release sync.Once
}

var _ uisdk.InitializeOperation = (*initializeOperation)(nil)

// PrepareInitialize reserves the one TUI initialization without opening terminal resources.
func (controller *Controller) PrepareInitialize(
	_ context.Context,
	initialization *uiv1.Initialization,
) (uisdk.InitializeOperation, error) {
	if initialization == nil {
		return nil, uisdk.Reject("INVALID_ARGUMENT", errors.New("TUI initialization is required"))
	}
	initial, err := mapInitializationRequest(initialization)
	if err != nil {
		return nil, uisdk.Reject("INVALID_ARGUMENT", fmt.Errorf("map TUI initialization: %w", err))
	}
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	if controller.preparing || controller.initialized != nil {
		return nil, uisdk.Reject("BUSY", errors.New("TUI initialization is already active"))
	}
	controller.preparing = true
	return &initializeOperation{controller: controller, initial: initial, release: sync.Once{}}, nil
}

// Run commits prepared initialization without repeating bounded validation.
func (operation *initializeOperation) Run(context.Context) (*uiv1.Initialized, error) {
	terminalSession, err := operation.controller.terminal.Open()
	if err != nil {
		return nil, fmt.Errorf("open TUI terminal: %w", err)
	}
	application := &initializedApplication{initial: operation.initial, terminalSession: terminalSession}
	operation.controller.mutex.Lock()
	operation.controller.initialized = application
	operation.controller.mutex.Unlock()
	return new(uiv1.Initialized), nil
}

// Release frees the bounded initialization reservation once.
func (operation *initializeOperation) Release() {
	operation.release.Do(func() {
		operation.controller.mutex.Lock()
		operation.controller.preparing = false
		operation.controller.mutex.Unlock()
	})
}

// Run owns the TUI program and every operation started through the SDK Host.
func (controller *Controller) Run(ctx context.Context, host *uisdk.Host) (returnErr error) {
	controller.mutex.Lock()
	application := controller.initialized
	var terminalSession TerminalSession
	if application != nil {
		terminalSession = application.terminalSession
		application.terminalSession = nil
	}
	controller.mutex.Unlock()
	if application == nil || terminalSession == nil {
		return errors.New("run TUI: initialization did not open application resources")
	}
	defer func() {
		if closeErr := terminalSession.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close TUI terminal: %w", closeErr))
		}
	}()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var work sync.WaitGroup
	var program Program
	programReady := make(chan struct{})
	emit := func(command presentationdomain.Command) error {
		select {
		case <-programReady:
			return controller.emit(runCtx, host, program, &work, command)
		case <-runCtx.Done():
			return context.Cause(runCtx)
		}
	}
	program = controller.programs.New(
		application.initial, terminalSession.Input(), terminalSession.Output(), emit,
	)
	close(programReady)
	connectionDone := make(chan error, 1)
	go func() { connectionDone <- controller.receiveConnectionEvents(runCtx, host, program) }()
	programDone := make(chan error, 1)
	go func() { programDone <- program.Run() }()

	select {
	case err := <-programDone:
		returnErr = err
		if closeErr := host.Close(context.WithoutCancel(ctx)); closeErr != nil {
			returnErr = errors.Join(returnErr, closeErr)
		}
	case err := <-connectionDone:
		if !errors.Is(err, context.Canceled) {
			returnErr = err
		}
		program.Quit()
		returnErr = errors.Join(returnErr, <-programDone)
	case <-ctx.Done():
		program.Quit()
		returnErr = errors.Join(context.Cause(ctx), <-programDone)
	}
	cancel()
	work.Wait()
	return returnErr
}

// Close releases an initialized terminal session that Service.Run did not consume.
func (controller *Controller) Close() error {
	controller.mutex.Lock()
	application := controller.initialized
	controller.initialized = nil
	controller.mutex.Unlock()
	if application == nil || application.terminalSession == nil {
		return nil
	}
	if err := application.terminalSession.Close(); err != nil {
		return fmt.Errorf("close prepared TUI terminal: %w", err)
	}
	return nil
}

// emit translates one presentation intent into SDK-owned operation behavior.
func (controller *Controller) emit(
	ctx context.Context,
	host *uisdk.Host,
	program Program,
	work *sync.WaitGroup,
	command presentationdomain.Command,
) error {
	switch command.Kind {
	case presentationdomain.CommandQuit:
		return host.Close(ctx)
	case presentationdomain.CommandStop:
		controller.mutex.Lock()
		target := controller.foreground
		controller.mutex.Unlock()
		if target == "" {
			return errors.New("cancel TUI foreground operation: no operation is active")
		}
		identifier := controller.nextOperationID()
		cancellation, err := host.Cancel(ctx, identifier, target)
		if err != nil {
			return err
		}
		work.Go(func() {
			if _, waitErr := cancellation.Wait(ctx); waitErr != nil && !errors.Is(waitErr, context.Canceled) {
				program.Send(textEvent(presentationdomain.EventError, waitErr.Error()))
			}
		})
		return nil
	case presentationdomain.CommandUnspecified, presentationdomain.CommandSubmit,
		presentationdomain.CommandRetryAuthentication, presentationdomain.CommandSelectModel,
		presentationdomain.CommandSelectReasoningChoice, presentationdomain.CommandCreateSession,
		presentationdomain.CommandListSessions, presentationdomain.CommandResumeSession,
		presentationdomain.CommandSetSessionName, presentationdomain.CommandGetSessionInfo,
		presentationdomain.CommandGetSessionTree, presentationdomain.CommandNavigateSessionTree,
		presentationdomain.CommandForkSession, presentationdomain.CommandCloneSession,
		presentationdomain.CommandSetEntryLabel:
		request, err := mapCommand(command)
		if err != nil {
			return err
		}
		identifier := controller.nextOperationID()
		started, err := host.Start(ctx, identifier, request)
		if err != nil {
			return err
		}
		if command.Kind == presentationdomain.CommandSubmit ||
			command.Kind == presentationdomain.CommandNavigateSessionTree {
			controller.mutex.Lock()
			if controller.foreground == "" {
				controller.foreground = identifier
			}
			controller.mutex.Unlock()
		}
		work.Add(1)
		go controller.waitOperation(ctx, identifier, command, started, program, work)
		return nil
	default:
		return fmt.Errorf("unknown TUI command %d", command.Kind)
	}
}

// waitOperation projects lifecycle data and clears foreground ownership after terminal receipt.
func (controller *Controller) waitOperation(
	ctx context.Context,
	identifier string,
	command presentationdomain.Command,
	started *uisdk.Operation,
	program Program,
	work *sync.WaitGroup,
) {
	defer work.Done()
	defer func() {
		controller.mutex.Lock()
		if controller.foreground == identifier {
			controller.foreground = ""
		}
		controller.mutex.Unlock()
	}()
	completed, err := started.Wait(ctx, func(progress *uiv1.HostProgress) {
		event, mapErr := mapHostProgress(progress)
		if mapErr != nil {
			program.Send(textEvent(presentationdomain.EventError, mapErr.Error()))
			return
		}
		program.Send(event)
	})
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			program.Send(operationErrorEvent(command, err))
		}
		return
	}
	if event, present, mapErr := mapCompleted(completed); mapErr != nil {
		program.Send(textEvent(presentationdomain.EventError, mapErr.Error()))
	} else if present {
		program.Send(event)
	}
}

// operationErrorEvent maps one operation failure to its presentation-owned pending state.
func operationErrorEvent(command presentationdomain.Command, err error) presentationdomain.Event {
	if command.Kind == presentationdomain.CommandResumeSession {
		return textEvent(presentationdomain.EventInformation, err.Error())
	}
	if command.TreeCommand.IsSome() {
		return treeEvent(presentationdomain.EventTreeOperationFailed, presentationdomain.TreeEvent{
			Tree:             mo.None[presentationdomain.SessionTree](),
			NavigationStatus: presentationdomain.TreeNavigationUnspecified,
			SessionInfo:      mo.None[presentationdomain.SessionInfo](), RestoredTranscript: nil,
			NextInput: mo.None[string](), Issues: nil, FailureMessage: mo.Some(err.Error()),
		})
	}
	return textEvent(presentationdomain.EventError, err.Error())
}

// receiveConnectionEvents projects Host connection events until closure.
func (*Controller) receiveConnectionEvents(ctx context.Context, host *uisdk.Host, program Program) error {
	for {
		connection, err := host.Receive(ctx)
		if err != nil {
			return err
		}
		event, err := mapConnectionEvent(connection)
		if err != nil {
			return err
		}
		program.Send(event)
	}
}

// nextOperationID assigns one process-local operation identifier.
func (controller *Controller) nextOperationID() string {
	return fmt.Sprintf(operationIDTemplate, controller.sequence.Add(1))
}
