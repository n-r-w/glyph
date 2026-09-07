package terminal

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// Runtime implements terminal resources, immutable display output, and framework execution.
type Runtime struct {
	// device opens independent terminal files.
	device Device
	// files contains the initialized terminal resources until cleanup.
	files Files
	// model contains only framework state and the latest application display snapshot.
	model *Model
	// source supplies SDK notifications without deciding application transitions.
	source NotificationSource
}

var (
	_ presentation.Display = (*Runtime)(nil)
	_ presentation.Runtime = (*Runtime)(nil)
)

// NewRuntime constructs terminal output before application and SDK activation.
func NewRuntime(device Device) *Runtime {
	return &Runtime{device: device, files: nil, model: &Model{
		snapshot: presentation.Snapshot{}, input: nil, plugin: nil, width: 0, height: 0,
		failure: nil, notificationEnded: false,
	}, source: nil}
}

// BindInput connects the actual input controllers and notification source before startup.
func (runtime *Runtime) BindInput(
	input *tuiinput.Controller,
	plugin *plugininput.Controller,
	source NotificationSource,
) {
	runtime.model.input = input
	runtime.model.plugin = plugin
	runtime.source = source
}

// Open acquires terminal resources after application initialization is accepted.
func (runtime *Runtime) Open() error {
	files, err := runtime.device.Open()
	if err != nil {
		return err
	}
	runtime.files = files
	// Each initialized connection gets fresh framework state while retaining its bound input owners.
	runtime.model = &Model{
		snapshot: presentation.Snapshot{}, input: runtime.model.input, plugin: runtime.model.plugin,
		width: 0, height: 0, failure: nil, notificationEnded: false,
	}
	return nil
}

// Publish replaces the immutable display snapshot on the application event loop.
func (runtime *Runtime) Publish(snapshot presentation.Snapshot) {
	runtime.model.snapshot = snapshot
}

// Run pumps notifications into the existing event loop and joins program shutdown.
func (runtime *Runtime) Run(ctx context.Context) presentation.RuntimeResult {
	// fps keeps input rendering responsive during rapid typing.
	const fps = 120
	program := tea.NewProgram(runtime.model, tea.WithInput(runtime.files.Input()),
		tea.WithOutput(runtime.files.Output()), tea.WithFPS(fps))
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	notificationDone := make(chan struct{})
	go func() {
		defer close(notificationDone)
		for {
			notification, err := runtime.source.ReceiveNotification(runCtx)
			if err != nil {
				program.Send(sourceEnded{err: err})
				return
			}
			program.Send(notification)
		}
	}()
	programDone := make(chan error, 1)
	go func() { _, err := program.Run(); programDone <- err }()
	var result presentation.RuntimeResult
	select {
	case err := <-programDone:
		result = presentation.RuntimeResult{
			ProgramExited: !runtime.model.notificationEnded,
			Err:           errors.Join(runtime.model.failure, err),
		}
	case <-ctx.Done():
		program.Quit()
		result = presentation.RuntimeResult{ProgramExited: false, Err: errors.Join(context.Cause(ctx), <-programDone)}
	}
	cancel()
	<-notificationDone
	return result
}

// Close releases initialized terminal files exactly once.
func (runtime *Runtime) Close() error {
	files := runtime.files
	runtime.files = nil
	if files == nil {
		return nil
	}
	return files.Close()
}
