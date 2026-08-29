package tui

import (
	"io"

	tea "charm.land/bubbletea/v2"

	plugincontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// Factory creates Bubble Tea programs with the accepted terminal files.
type Factory struct {
	apply Apply
}

var (
	_ plugincontroller.ProgramFactory = (*Factory)(nil)
	_ plugincontroller.Program        = (*program)(nil)
)

// NewFactory creates a Bubble Tea program factory.
func NewFactory(apply Apply) *Factory {
	return &Factory{apply: apply}
}

// New creates one program initialized before the event loop starts.
func (factory *Factory) New(
	initial presentationdomain.Event,
	input io.Reader,
	output io.Writer,
	emit plugincontroller.Emit,
) plugincontroller.Program {
	model := NewModel(initial, factory.apply, Emit(emit))

	const fps = 120 // Otherwise, there is a lag when entering the test
	return &program{
		tea: tea.NewProgram(
			model,
			tea.WithInput(input),
			tea.WithOutput(output),
			tea.WithFPS(fps),
		),
	}
}

// program adapts one Bubble Tea program to the plugin controller boundary.
type program struct {
	tea *tea.Program
}

// Send delivers one ordered Host event to the running Bubble Tea program.
func (program *program) Send(event presentationdomain.Event) {
	program.tea.Send(event)
}

// Quit requests normal Bubble Tea termination.
func (program *program) Quit() {
	program.tea.Quit()
}

// Run owns the Bubble Tea event loop until normal termination or failure.
func (program *program) Run() error {
	_, err := program.tea.Run()
	return err
}
