// Package presentation owns TUI application state and serialized interaction transitions.
package presentation

import (
	"context"
	"errors"
	"fmt"
	"sync"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
)

// Service owns application projection, interaction, pending commands, and runtime admission.
type Service struct {
	// model contains the private transcript, editor, selector, and tree state.
	model interaction
	// host executes immutable prepared commands.
	host Host
	// display receives snapshots without access to model state.
	display Display
	// runtime owns the event loop and terminal resources.
	runtime Runtime
	// pending retains command policy until acknowledgement and terminal data are consumed.
	pending map[string]pendingCommand
	// foreground identifies the first active cancelable command.
	foreground string
	// sequence assigns process-local command identifiers on the event loop.
	sequence uint64
	// body is the detached immutable projection reused during editor-only transitions.
	body DisplayBody
	// lifecycle protects initialization admission before event-loop ownership starts.
	lifecycle sync.Mutex
	// preparing reserves one admitted initialization.
	preparing bool
	// initialized records successful resource initialization.
	initialized bool
	// running transfers terminal cleanup to Run.
	running bool
}

var (
	_ plugininput.Presentation = (*Service)(nil)
	_ tuiinput.Interaction     = (*Service)(nil)
)

// New constructs one TUI application before SDK activation.
func New(host Host, display Display, runtime Runtime) *Service {
	return &Service{
		model: interaction{}, host: host, display: display, runtime: runtime,
		pending: make(map[string]pendingCommand), foreground: "", sequence: 0, body: DisplayBody{},
		lifecycle: sync.Mutex{}, preparing: false, initialized: false, running: false,
	}
}

// PrepareInitialize reserves startup without opening terminal resources before acceptance.
func (service *Service) PrepareInitialize(
	initial plugininput.Initialization,
) (plugininput.InitializationOperation, error) {
	service.lifecycle.Lock()
	defer service.lifecycle.Unlock()
	if service.preparing || service.initialized {
		return nil, plugininput.ErrBusy
	}
	service.preparing = true
	return &initializationWork{service: service, initial: initializationEvent(initial), release: sync.Once{}}, nil
}

// initializationWork owns one admitted startup and its reservation.
type initializationWork struct {
	// service receives the opened terminal and initial state.
	service *Service
	// initial is a detached private startup transition.
	initial event
	// release limits admission release to one attempt.
	release sync.Once
}

var _ plugininput.InitializationOperation = (*initializationWork)(nil)

// Run opens the terminal and publishes initial application state before the event loop starts.
func (work *initializationWork) Run(context.Context) error {
	if err := work.service.runtime.Open(); err != nil {
		return fmt.Errorf("open TUI terminal: %w", err)
	}
	work.service.model = newInteraction(work.initial)
	work.service.publish()
	work.service.lifecycle.Lock()
	work.service.initialized = true
	work.service.lifecycle.Unlock()
	return nil
}

// Release frees the initialization reservation once.
func (work *initializationWork) Release() {
	work.release.Do(func() {
		work.service.lifecycle.Lock()
		work.service.preparing = false
		work.service.lifecycle.Unlock()
	})
}

// Run transfers terminal lifetime to the event loop and preserves close/error ordering.
func (service *Service) Run(ctx context.Context) (returnErr error) {
	service.lifecycle.Lock()
	if !service.initialized || service.running {
		service.lifecycle.Unlock()
		return errors.New("run TUI: initialization did not open application resources")
	}
	service.running = true
	service.lifecycle.Unlock()
	defer func() {
		service.host.StopDispatch()
		if err := service.runtime.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close TUI terminal: %w", err))
		}
	}()
	result := service.runtime.Run(ctx)
	returnErr = result.Err
	if result.ProgramExited {
		returnErr = errors.Join(returnErr, service.host.CloseConnection(context.WithoutCancel(ctx)))
	}
	return returnErr
}

// Close releases prepared terminal resources when the SDK does not enter Run.
func (service *Service) Close() error {
	service.lifecycle.Lock()
	closePrepared := service.initialized && !service.running
	service.initialized = false
	service.running = false
	service.lifecycle.Unlock()
	if closePrepared {
		if err := service.runtime.Close(); err != nil {
			return fmt.Errorf("close prepared TUI terminal: %w", err)
		}
	}
	return nil
}
