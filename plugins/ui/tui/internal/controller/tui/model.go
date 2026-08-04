// Package tui owns the standard terminal presentation and Bubble Tea event loop.
//
//nolint:exhaustruct // The root model emits partial presentation events and commands for one active kind.
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// Apply projects one Host event into presentation state.
type Apply func(presentationdomain.State, presentationdomain.Event) presentationdomain.State

// Emit sends one accepted user command to the Host stream.
type Emit func(presentationdomain.Command) error

// Model is the single root Bubble Tea presentation and input model.
type Model struct {
	state    presentationdomain.State
	input    []rune
	cursor   int
	width    int
	height   int
	emitting bool
	apply    Apply
	emit     Emit
}

const fixedViewLineCount = 5

// emissionResultMsg returns command-delivery success or failure to the update loop.
type emissionResultMsg struct {
	command presentationdomain.Command
	err     error
}

// NewModel creates the root model from the initialization event.
func NewModel(initial presentationdomain.Event, apply Apply, emit Emit) Model {
	return Model{state: apply(presentationdomain.State{}, initial), apply: apply, emit: emit}
}

// Init starts no background presentation work.
func (Model) Init() tea.Cmd {
	return nil
}

// Update applies one Bubble Tea message.
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case presentationdomain.Event:
		model.state = model.apply(model.state, message)
		return model, nil
	case emissionResultMsg:
		return model.applyEmissionResult(message)
	case tea.WindowSizeMsg:
		model.width = message.Width
		model.height = message.Height
		return model, nil
	case tea.KeyPressMsg:
		return model.updateKey(message.Key())
	default:
		return model, nil
	}
}

// applyEmissionResult clears accepted input or renders a delivery failure.
func (model Model) applyEmissionResult(message emissionResultMsg) (tea.Model, tea.Cmd) {
	model.emitting = false
	if message.err != nil {
		model.state = model.apply(model.state, presentationdomain.Event{
			Kind: presentationdomain.EventError,
			Text: "Could not send command: " + message.err.Error(),
		})
		return model, nil
	}

	switch message.command.Kind {
	case presentationdomain.CommandSubmit:
		model.state = model.apply(model.state, presentationdomain.Event{
			Kind: presentationdomain.EventUserSubmitted,
			Text: message.command.Text,
		})
		model.input = nil
		model.cursor = 0
	case presentationdomain.CommandQuit:
		return model, tea.Quit
	case presentationdomain.CommandUnspecified,
		presentationdomain.CommandStop,
		presentationdomain.CommandRetryAuthentication:
	}

	return model, nil
}

//nolint:gocyclo // The explicit flat switch mirrors the supported editor and command keys.
func (model Model) updateKey(key tea.Key) (tea.Model, tea.Cmd) {
	if key.Mod == tea.ModCtrl {
		switch key.Code {
		case 'q':
			return model.emitCommand(presentationdomain.Command{Kind: presentationdomain.CommandQuit})
		case 'c':
			if model.state.Availability == presentationdomain.AvailabilityRunning {
				return model.emitCommand(presentationdomain.Command{Kind: presentationdomain.CommandStop})
			}
			return model, nil
		case 'r':
			if model.state.Availability == presentationdomain.AvailabilityAuthenticationFailed {
				return model.emitCommand(presentationdomain.Command{Kind: presentationdomain.CommandRetryAuthentication})
			}
			return model, nil
		}
	}

	if model.state.Availability != presentationdomain.AvailabilityIdle || model.emitting {
		return model, nil
	}

	switch key.Code {
	case tea.KeyEnter:
		text := strings.TrimSpace(string(model.input))
		if text == "" {
			return model, nil
		}
		return model.emitCommand(presentationdomain.Command{Kind: presentationdomain.CommandSubmit, Text: text})
	case tea.KeyLeft:
		if model.cursor > 0 {
			model.cursor--
		}
	case tea.KeyRight:
		if model.cursor < len(model.input) {
			model.cursor++
		}
	case tea.KeyHome:
		model.cursor = 0
	case tea.KeyEnd:
		model.cursor = len(model.input)
	case tea.KeyBackspace:
		if model.cursor > 0 {
			model.input = append(model.input[:model.cursor-1], model.input[model.cursor:]...)
			model.cursor--
		}
	case tea.KeyDelete:
		if model.cursor < len(model.input) {
			model.input = append(model.input[:model.cursor], model.input[model.cursor+1:]...)
		}
	default:
		if key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta) == 0 {
			model.insertText(key.Text)
		}
	}

	return model, nil
}

// emitCommand serializes one UI command without blocking the update loop.
func (model Model) emitCommand(command presentationdomain.Command) (tea.Model, tea.Cmd) {
	if model.emitting {
		return model, nil
	}
	model.emitting = true

	return model, func() tea.Msg {
		return emissionResultMsg{command: command, err: model.emit(command)}
	}
}

// insertText adds pasted or typed Unicode text at the rune cursor.
func (model *Model) insertText(text string) {
	text = strings.NewReplacer("\r", "", "\n", "").Replace(text)
	if text == "" {
		return
	}
	runes := []rune(text)
	model.input = append(model.input[:model.cursor], append(runes, model.input[model.cursor:]...)...)
	model.cursor += len(runes)
}

// View renders the current presentation as plain terminal text.
func (model Model) View() tea.View {
	body := model.visibleBodyLines()
	lines := make([]string, 0, fixedViewLineCount+len(body))
	lines = append(lines,
		"Glyph",
		"Status: "+availabilityText(model.state.Availability),
	)
	lines = append(lines, body...)
	lines = append(lines,
		"Request: "+string(model.input[:model.cursor])+"|"+string(model.input[model.cursor:]),
		fmt.Sprintf("Terminal: %dx%d", model.width, model.height),
		"Keys: Enter submit | Ctrl+C stop | Ctrl+R retry authentication | Ctrl+Q quit",
	)

	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true

	return view
}

// visibleBodyLines keeps the latest transcript while preserving the editor and help lines.
func (model Model) visibleBodyLines() []string {
	lines := make([]string, 0, len(model.state.Startup)+len(model.state.Transcript)+len(model.state.ActiveModel)+1)
	for _, line := range model.state.Startup {
		lines = append(lines, strings.Split(renderLine(line), "\n")...)
	}
	for _, line := range model.state.Transcript {
		lines = append(lines, strings.Split(renderLine(line), "\n")...)
	}
	positions := make([]int, 0, len(model.state.ActiveModel))
	for position := range model.state.ActiveModel {
		positions = append(positions, position)
	}
	sort.Ints(positions)
	for _, position := range positions {
		lines = append(lines, strings.Split("assistant: "+model.state.ActiveModel[position], "\n")...)
	}
	if model.state.AuthorizationURL != "" {
		lines = append(lines, "Authorization: "+model.state.AuthorizationURL)
	}

	if model.height <= 0 {
		return lines
	}
	capacity := model.height - fixedViewLineCount
	if capacity <= 0 {
		return nil
	}
	if len(lines) > capacity {
		return lines[len(lines)-capacity:]
	}
	return lines
}

// renderLine assigns one stable terminal prefix to each presentation line kind.
func renderLine(line presentationdomain.Line) string {
	var prefix string
	switch line.Kind {
	case presentationdomain.LineInformation:
		prefix = "[info]"
	case presentationdomain.LineError:
		prefix = "[error]"
	case presentationdomain.LineWarning:
		prefix = "[warning]"
	case presentationdomain.LineUser:
		prefix = "user:"
	case presentationdomain.LineModel:
		prefix = "assistant:"
	case presentationdomain.LineToolStatus:
		prefix = "[tool:status]"
	case presentationdomain.LineToolStdout:
		prefix = "[tool:stdout]"
	case presentationdomain.LineToolStderr:
		prefix = "[tool:stderr]"
	case presentationdomain.LineToolDone:
		prefix = "[tool:done]"
	case presentationdomain.LineToolError:
		prefix = "[tool:error]"
	case presentationdomain.LineUnspecified:
		prefix = "[info]"
	}

	parts := []string{prefix}
	if line.ToolName != "" {
		parts = append(parts, line.ToolName)
	}
	if line.Status != "" {
		parts = append(parts, "("+line.Status+")")
	}
	if line.Text != "" {
		parts = append(parts, line.Text)
	}

	return strings.Join(parts, " ")
}

var _ tea.Model = (*Model)(nil)

// availabilityText maps Host availability to concise terminal status text.
func availabilityText(availability presentationdomain.Availability) string {
	switch availability {
	case presentationdomain.AvailabilityChecking:
		return "Checking"
	case presentationdomain.AvailabilityAuthenticating:
		return "Authenticating"
	case presentationdomain.AvailabilityAuthenticationFailed:
		return "Authentication failed"
	case presentationdomain.AvailabilityIdle:
		return "Idle"
	case presentationdomain.AvailabilityRunning:
		return "Running"
	case presentationdomain.AvailabilityUnspecified:
		return "Unavailable"
	default:
		return "Unavailable"
	}
}
