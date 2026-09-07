package terminal

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// Model owns framework dimensions and immutable view data, not application state.
type Model struct {
	// snapshot is the coherent view published by the application.
	snapshot presentation.Snapshot
	// input decodes keys and command acknowledgements.
	input *tuiinput.Controller
	// plugin decodes SDK notifications on this event loop.
	plugin *plugininput.Controller
	// width is the terminal viewport width.
	width int
	// height is the terminal viewport height.
	height int
	// failure retains a notification or input-mapping failure for runtime shutdown.
	failure error
	// notificationEnded distinguishes source shutdown from local program termination.
	notificationEnded bool
}

var _ tea.Model = (*Model)(nil)

// sourceEnded returns receive-pump termination to the framework event loop.
type sourceEnded struct {
	// err is the complete SDK receive failure.
	err error
}

// Init starts no work before runtime-controlled pumps are active.
func (*Model) Init() tea.Cmd { return nil }

// Update serializes controller input and returns prepared I/O without mutating application state.
func (model *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case *uisdk.Notification:
		if err := model.plugin.Notify(message); err != nil {
			model.failure = err
			model.notificationEnded = true
			return model, tea.Quit
		}
	case tuiinput.Result:
		if model.input.Complete(message) {
			return model, tea.Quit
		}
	case tea.WindowSizeMsg:
		model.width, model.height = message.Width, message.Height
	case tea.KeyPressMsg:
		if work := model.input.Key(message.Key()); work != nil {
			return model, func() tea.Msg { return work.Execute() }
		}
	case sourceEnded:
		model.notificationEnded = true
		if !errors.Is(message.err, context.Canceled) {
			model.failure = message.err
		}
		return model, tea.Quit
	}
	return model, nil
}

const (
	// inactiveSelectorPrefix marks an unselected selector row.
	inactiveSelectorPrefix = "  "
	// activeSelectorPrefix marks a selected selector row.
	activeSelectorPrefix = "> "
	// fixedViewLineCount reserves non-selector viewport lines.
	fixedViewLineCount = 5
	// selectorFixedLineCount reserves selector title and help lines.
	selectorFixedLineCount = 2
	// maxVisibleSelectorRows bounds the selector window.
	maxVisibleSelectorRows = 5
	// selectorCenterDivisor centers the selected row in its window.
	selectorCenterDivisor = 2
)
