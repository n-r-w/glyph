// Package runtime owns the selected UI process, its single stream, and ordered output.
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	"github.com/n-r-w/glyph/internal/operation"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// Service retains the candidate selected by Host policy and its output state.
type Service struct {
	// client owns the selected process connection.
	client *uisdk.Client
	// openOnce limits the selected process to one stream.
	openOnce sync.Once
	// openErr retains the stream acquisition failure.
	openErr error
	// stream is the generated bidirectional UI stream.
	stream uiv1.UIService_OpenClient
	// cancel stops the opened stream context.
	cancel context.CancelFunc
	// closed prevents duplicate stream cancellation.
	closed atomic.Bool
	// mutex protects output readiness and reporter binding.
	mutex sync.Mutex
	// ready reports successful initialization.
	ready bool
	// writer serializes operation and connection output.
	writer *operation.Writer[*uiv1.OpenRequest]
	// progressReporter belongs to the active prepared application operation.
	progressReporter operation.Reporter[controllerui.Frame]
	// progressBound reports that the operation reporter is attached.
	progressBound bool
	// failConnection reports asynchronous output failure to the controller.
	failConnection func(error)
}

var (
	_ hostui.Runtime            = (*Service)(nil)
	_ hostui.Output             = (*Service)(nil)
	_ controllerui.StreamSource = (*Service)(nil)
)

// New creates the selected-process owner before candidate selection.
func New() *Service {
	return &Service{
		client: nil, openOnce: sync.Once{}, openErr: nil, stream: nil, cancel: nil,
		closed: atomic.Bool{}, mutex: sync.Mutex{}, ready: false, writer: nil,
		progressReporter: operation.Reporter[controllerui.Frame]{}, progressBound: false, failConnection: nil,
	}
}

// Start launches one candidate without opening its stream.
func (s *Service) Start(ctx context.Context, candidate hostui.Candidate) error {
	//nolint:gosec // The catalog contains trusted local UI plugin executables.
	command := exec.CommandContext(context.WithoutCancel(ctx), candidate.Path)
	client, err := uisdk.Connect(ctx, command)
	if err != nil {
		return fmt.Errorf("start UI %q: %w", candidate.ID, err)
	}
	s.client = client
	return nil
}

// Open acquires the selected stream once and never restarts or selects a process.
func (s *Service) Open(ctx context.Context) (controllerui.Connection, error) {
	s.openOnce.Do(func() {
		streamContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
		stream, err := s.client.Service().Open(streamContext)
		if err != nil {
			cancel()
			s.openErr = fmt.Errorf("open UI stream: %w", err)
			return
		}
		s.stream, s.cancel = stream, cancel
	})
	if s.openErr != nil {
		return nil, s.openErr
	}
	return s, nil
}

// Close closes a successful probe or stops the selected process and its opened stream.
func (s *Service) Close() {
	if s.cancel != nil && s.closed.CompareAndSwap(false, true) {
		s.cancel()
	}
	if s.client != nil {
		s.client.Close()
		s.client = nil
	}
}
