// Package host owns initialized SDK I/O for the standard TUI process.
package host

import (
	"context"
	"errors"
	"fmt"
	"sync"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/infra/terminal"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// Service binds SDK lifecycle, command transport, and notification reads.
type Service struct {
	// input decodes initialization and enters the application lifecycle.
	input *plugininput.Controller
	// mutex protects SDK binding while asynchronous commands execute.
	mutex sync.Mutex
	// host is the initialized SDK endpoint during Run.
	host *uisdk.Host
	// context is the active dispatch context.
	context context.Context
	// cancel ends command dispatch before terminal cleanup.
	cancel context.CancelFunc
}

var (
	_ uisdk.Service               = (*Service)(nil)
	_ presentation.Host           = (*Service)(nil)
	_ terminal.NotificationSource = (*Service)(nil)
)

// New creates the SDK adapter before application dependencies are bound.
func New() *Service {
	return &Service{input: nil, mutex: sync.Mutex{}, host: nil, context: nil, cancel: nil}
}

// BindInput binds the real input controller before SDK activation.
func (service *Service) BindInput(input *plugininput.Controller) { service.input = input }

// PrepareInitialize translates accepted application work to the SDK initialization contract.
func (service *Service) PrepareInitialize(
	_ context.Context,
	initialization *uiv1.Initialization,
) (uisdk.InitializeOperation, error) {
	work, err := service.input.PrepareInitialize(initialization)
	if err != nil {
		return nil, err
	}
	return &initializationWork{work: work}, nil
}

// initializationWork supplies the SDK result for successful application initialization.
type initializationWork struct {
	// work owns application admission and resource initialization.
	work plugininput.InitializationOperation
}

var _ uisdk.InitializeOperation = (*initializationWork)(nil)

// Run returns the SDK Initialized payload after application initialization succeeds.
func (work *initializationWork) Run(ctx context.Context) (*uiv1.Initialized, error) {
	if err := work.work.Run(ctx); err != nil {
		return nil, err
	}
	return new(uiv1.Initialized), nil
}

// Release returns the application initialization reservation.
func (work *initializationWork) Release() { work.work.Release() }

// Run retains the initialized SDK endpoint and context for command and notification I/O.
func (service *Service) Run(ctx context.Context, host *uisdk.Host) error {
	runCtx, cancel := context.WithCancel(ctx)
	service.mutex.Lock()
	service.host, service.context, service.cancel = host, runCtx, cancel
	service.mutex.Unlock()
	defer func() {
		cancel()
		service.mutex.Lock()
		service.host, service.context, service.cancel = nil, nil, nil
		service.mutex.Unlock()
	}()
	return service.input.Run(runCtx)
}

// Send encodes prepared commands and uses the application's captured cancellation target.
func (service *Service) Send(identifier string, command presentation.Command, target string) error {
	service.mutex.Lock()
	host, ctx := service.host, service.context
	service.mutex.Unlock()
	if host == nil || ctx == nil {
		return errors.New("send TUI command: no program is active")
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	switch command.Kind {
	case presentation.CommandQuit:
		return host.Close(ctx)
	case presentation.CommandStop:
		_, err := host.Cancel(ctx, identifier, target)
		return err
	case presentation.CommandUnspecified, presentation.CommandSubmit, presentation.CommandRetryAuthentication,
		presentation.CommandSelectModel, presentation.CommandSelectReasoningChoice, presentation.CommandCreateSession,
		presentation.CommandListSessions, presentation.CommandResumeSession, presentation.CommandSetSessionName,
		presentation.CommandGetSessionInfo, presentation.CommandGetSessionTree, presentation.CommandNavigateSessionTree,
		presentation.CommandForkSession, presentation.CommandCloneSession, presentation.CommandSetEntryLabel:
		request, err := mapCommand(command)
		if err != nil {
			return err
		}
		_, err = host.Start(ctx, identifier, request)
		return err
	default:
		return fmt.Errorf("unknown TUI command %d", command.Kind)
	}
}

// ReceiveNotification reads one SDK notification without touching application correlation or state.
func (service *Service) ReceiveNotification(ctx context.Context) (*uisdk.Notification, error) {
	service.mutex.Lock()
	host := service.host
	service.mutex.Unlock()
	if host == nil {
		return nil, errors.New("receive TUI notification: no program is active")
	}
	return host.Receive(ctx)
}

// CloseConnection closes the initialized Host connection under the caller's cleanup context.
func (service *Service) CloseConnection(ctx context.Context) error {
	service.mutex.Lock()
	host := service.host
	service.mutex.Unlock()
	if host == nil {
		return nil
	}
	return host.Close(ctx)
}

// StopDispatch cancels in-flight command transport before terminal cleanup.
func (service *Service) StopDispatch() {
	service.mutex.Lock()
	cancel := service.cancel
	service.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Close releases prepared application resources when the SDK never enters Run.
func (service *Service) Close() error { return service.input.Close() }
