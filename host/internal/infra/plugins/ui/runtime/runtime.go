// Package runtime adapts the public UI SDK to Host UI lifecycle contracts.
package runtime

import (
	"context"

	"fmt"

	"os/exec"

	"sync"
	"sync/atomic"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"

	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// Factory starts UI candidates through the public SDK.
type Factory struct{}

var _ hostui.RuntimeFactory = (*Factory)(nil)

// Runtime owns one connected UI process and its single stream.
type Runtime struct {
	// client owns the UI process connection.
	client *uisdk.Client
	// openOnce limits the runtime to one stream.
	openOnce sync.Once
	// channel contains the opened provider-neutral UI stream.
	channel hostui.Channel
	// openErr retains the stream opening result.
	openErr error
}

var _ hostui.Runtime = (*Runtime)(nil)

var _ hostui.Channel = (*channel)(nil)

// NewFactory creates a UI runtime factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Start launches one trusted local UI candidate and validates its fixed capabilities.
func (*Factory) Start(ctx context.Context, candidate domainui.Candidate) (hostui.Runtime, error) {
	//nolint:gosec // The catalog contains trusted local UI plugin executables.
	command := exec.CommandContext(context.WithoutCancel(ctx), candidate.Path)
	client, err := uisdk.Connect(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start UI %q: %w", candidate.ID, err)
	}
	return &Runtime{
		client:   client,
		openOnce: sync.Once{},
		channel:  nil,
		openErr:  nil,
	}, nil
}

// Capabilities returns the immutable capabilities retrieved before stream creation.
func (r *Runtime) Capabilities() domainui.Capabilities {
	return domainui.Capabilities{
		ControlsTerminal: r.client.Capabilities().GetControlsTerminal(),
	}
}

// Open opens and reuses the one persistent UI lifecycle stream.
func (r *Runtime) Open(ctx context.Context) (hostui.Channel, error) {
	r.openOnce.Do(func() {
		streamContext, cancel := context.WithCancel(ctx)
		stream, err := r.client.Service().Open(streamContext)
		if err != nil {
			cancel()
			r.openErr = fmt.Errorf("open UI stream: %w", err)
			return
		}
		r.channel = &channel{
			stream: stream,
			cancel: cancel,
			closed: atomic.Bool{},
			mutex:  sync.Mutex{},
		}
	})
	return r.channel, r.openErr
}

// Close stops the selected UI process once.
func (r *Runtime) Close() {
	r.client.Close()
}
