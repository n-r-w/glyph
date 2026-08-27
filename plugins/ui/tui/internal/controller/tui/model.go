// Package tui owns the standard terminal presentation and Bubble Tea event loop.
package tui

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// Apply projects one Host event into presentation state.
type Apply func(presentationdomain.State, presentationdomain.Event) presentationdomain.State

// Emit sends one accepted user command to the Host stream.
type Emit func(presentationdomain.Command) error

// Model is the single root Bubble Tea presentation and input model.
type Model struct {
	state  presentationdomain.State
	input  []rune
	cursor int
	width  int
	height int
	// emitting prevents overlapping commands until the current stream send returns.
	emitting bool
	// selectorOpen routes keys away from the editor into the visible selector.
	selectorOpen bool
	// sessionSelector distinguishes resume rows from model rows while reusing navigation state.
	sessionSelector bool
	// resumePending keeps one selected session stable until Host accepts or rejects replacement.
	resumePending bool
	// resumeStatus shows a Host rejection without adding it to the active transcript.
	resumeStatus string
	// selectorRow is the selected model or session row.
	selectorRow int
	// reasoningExpanded controls only local display and never changes Host selection.
	reasoningExpanded bool
	apply             Apply
	emit              Emit
}

var _ tea.Model = (*Model)(nil)

const (
	fixedViewLineCount     = 5
	selectorFixedLineCount = 2
	maxVisibleSelectorRows = 5
	selectorCenterDivisor  = 2
)

// emissionResultMsg returns command-delivery success or failure to the update loop.
type emissionResultMsg struct {
	command presentationdomain.Command
	err     error
}

// NewModel creates the root model from the initialization event.
func NewModel(initial presentationdomain.Event, apply Apply, emit Emit) Model {
	return Model{
		state:             apply(presentationdomain.State{}, initial),
		input:             nil,
		cursor:            0,
		width:             0,
		height:            0,
		emitting:          false,
		selectorOpen:      false,
		selectorRow:       0,
		reasoningExpanded: false,
		apply:             apply,
		emit:              emit,
		sessionSelector:   false,
		resumePending:     false,
		resumeStatus:      "",
	}
}

// Init starts no background presentation work.
func (Model) Init() tea.Cmd {
	return nil
}

// Update applies one Bubble Tea message.
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case presentationdomain.Event:
		return model.applyEvent(message), nil
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

// applyEvent updates presentation state and the editor after one Host event.
func (model Model) applyEvent(event presentationdomain.Event) Model {
	preserveRejectedResume := model.selectorOpen && model.sessionSelector && model.resumePending &&
		event.Kind == presentationdomain.EventInformation
	if !preserveRejectedResume {
		model.state = model.apply(model.state, event)
	}
	switch event.Kind {
	case presentationdomain.EventSessionList:
		// The list refreshes selection data, while confirmation or cancellation still owns the draft.
		model.resumePending = false
		model.resumeStatus = ""
		model.selectorOpen = len(model.state.Sessions) > 0
		model.sessionSelector = model.selectorOpen
		model.selectorRow = 0
	case presentationdomain.EventSessionChanged:
		// Replacement confirmation owns the point where the editor and selector can discard old-session input.
		model.resumePending = false
		model.resumeStatus = ""
		model.input = nil
		model.cursor = 0
		model.selectorOpen = false
		model.sessionSelector = false
	case presentationdomain.EventSessionInformation:
		// Information confirms /session or /name without replacing transcript ownership.
		if info, present := event.SessionInfo.Get(); present {
			model.state = model.apply(
				model.state,
				sessionInformationEvent(formatSessionInformation(info, event.SessionStatistics)),
			)
		}
		model.input = nil
		model.cursor = 0
	case presentationdomain.EventInformation:
		if preserveRejectedResume {
			model.resumePending = false
			model.resumeStatus, _ = event.Text.Get()
		}
	case presentationdomain.EventUnspecified, presentationdomain.EventInitialization,
		presentationdomain.EventUserSubmitted, presentationdomain.EventAvailability,
		presentationdomain.EventTurnStarted, presentationdomain.EventModelDelta,
		presentationdomain.EventModelEnd, presentationdomain.EventToolCallPreview,
		presentationdomain.EventToolCallFinal, presentationdomain.EventToolStarted,
		presentationdomain.EventToolProgress, presentationdomain.EventToolOutput,
		presentationdomain.EventToolEnded, presentationdomain.EventToolResult,
		presentationdomain.EventTurnEnded, presentationdomain.EventAgentSettled,
		presentationdomain.EventAuthorization, presentationdomain.EventError,
		presentationdomain.EventModelSelectionChanged:
	}
	return model
}

// applyEmissionResult clears accepted input or renders a delivery failure.
func (model Model) applyEmissionResult(message emissionResultMsg) (tea.Model, tea.Cmd) {
	model.emitting = false
	if message.err != nil {
		model.state = model.apply(model.state, presentationdomain.Event{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventError,
			Startup:              nil,
			Extensions:           nil,
			Availability:         mo.None[presentationdomain.Availability](),
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[presentationdomain.OutputStream](),
			Text:                 mo.Some("Could not send command: " + message.err.Error()),
			Contents:             mo.None[[]presentationdomain.Content](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			SessionInfo:          mo.None[presentationdomain.SessionInfo](),
			Sessions:             nil,
			SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		})
		return model, nil
	}

	switch message.command.Kind {
	case presentationdomain.CommandSubmit:
		if message.command.Text.IsNone() {
			return model, nil
		}
		model.state = model.apply(model.state, presentationdomain.Event{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventUserSubmitted,
			Startup:              nil,
			Extensions:           nil,
			Availability:         mo.None[presentationdomain.Availability](),
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[presentationdomain.OutputStream](),
			Text:                 message.command.Text,
			Contents:             mo.None[[]presentationdomain.Content](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			SessionInfo:          mo.None[presentationdomain.SessionInfo](),
			Sessions:             nil,
			SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		})
		model.input = nil
		model.cursor = 0
	case presentationdomain.CommandQuit:
		return model, tea.Quit
	case presentationdomain.CommandUnspecified,
		presentationdomain.CommandStop,
		presentationdomain.CommandRetryAuthentication,
		presentationdomain.CommandSelectModel,
		presentationdomain.CommandSelectReasoningChoice,
		presentationdomain.CommandCreateSession,
		presentationdomain.CommandListSessions,
		presentationdomain.CommandResumeSession,
		presentationdomain.CommandSetSessionName,
		presentationdomain.CommandGetSessionInfo:
	}

	return model, nil
}

//nolint:gocyclo // The explicit flat switch mirrors the supported editor and command keys.
func (model Model) updateKey(key tea.Key) (tea.Model, tea.Cmd) {
	if isSelectionShortcut(key) {
		availability, ok := model.state.Availability.Get()
		if !ok || !selectionAvailable(availability) {
			return model, nil
		}
	}
	if key.Mod == tea.ModCtrl && key.Code == 't' {
		model.reasoningExpanded = !model.reasoningExpanded
		return model, nil
	}
	if model.selectorOpen {
		return model.updateSelector(key)
	}
	if model.emitting {
		return model, nil
	}
	if key.Mod == tea.ModCtrl|tea.ModShift && key.Code == 'p' {
		return model.cycleModel(-1)
	}
	if key.Mod == tea.ModCtrl {
		return model.updateControlKey(key.Code)
	}
	if key.Mod == tea.ModShift && key.Code == tea.KeyTab {
		return model.cycleReasoning()
	}

	availability, ok := model.state.Availability.Get()
	if !ok || (availability != presentationdomain.AvailabilityIdle &&
		availability != presentationdomain.AvailabilityRunning) {
		return model, nil
	}
	if availability == presentationdomain.AvailabilityRunning && len(model.input) == 0 &&
		key.Code != tea.KeyEnter && key.Text != "/" {
		return model, nil
	}

	switch key.Code {
	case tea.KeyEnter:
		return model.updateEnter(availability)
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

// updateEnter interprets session commands before treating input as an agent request.
func (model Model) updateEnter(availability presentationdomain.Availability) (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(string(model.input))
	if text == "" {
		return model, nil
	}
	switch text {
	case "/model":
		if availability != presentationdomain.AvailabilityIdle {
			return model, nil
		}
		model.input = nil
		model.cursor = 0
		return model.openSelector()
	case "/new":
		return model.emitSessionCommand(presentationdomain.CommandCreateSession, "", "")
	case "/resume":
		return model.emitSessionCommand(presentationdomain.CommandListSessions, "", "")
	case "/session":
		return model.emitSessionCommand(presentationdomain.CommandGetSessionInfo, "", "")
	case "/name":
		message := "Usage: /name <value>"
		if info, present := model.state.SessionInfo.Get(); present && info.NamePresent {
			message = info.Name
		}
		model.state = model.apply(model.state, sessionInformationEvent(message))
		model.input = nil
		model.cursor = 0
		return model, nil
	}
	if after, ok := strings.CutPrefix(text, "/name "); ok {
		name := after
		return model.emitSessionCommand(presentationdomain.CommandSetSessionName, "", name)
	}
	if availability != presentationdomain.AvailabilityIdle {
		return model, nil
	}
	return model.emitCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSubmit,
		Text:            mo.Some(text),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	})
}

// isSelectionShortcut matches only the approved selection bindings.
func isSelectionShortcut(key tea.Key) bool {
	return key.Mod == tea.ModCtrl && (key.Code == 'l' || key.Code == 'p') ||
		key.Mod == tea.ModCtrl|tea.ModShift && key.Code == 'p' ||
		key.Mod == tea.ModShift && key.Code == tea.KeyTab
}

// selectionAvailable rejects only active authentication availability.
func selectionAvailable(availability presentationdomain.Availability) bool {
	return availability != presentationdomain.AvailabilityChecking &&
		availability != presentationdomain.AvailabilityAuthenticating
}

// updateControlKey handles the exact control-key bindings.
func (model Model) updateControlKey(code rune) (tea.Model, tea.Cmd) {
	switch code {
	case 'q':
		return model.emitCommand(presentationdomain.Command{
			Kind:            presentationdomain.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		})
	case 'c':
		if availability, ok := model.state.Availability.Get(); ok && availability == presentationdomain.AvailabilityRunning {
			return model.emitCommand(presentationdomain.Command{
				Kind:            presentationdomain.CommandStop,
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			})
		}
	case 'r':
		availability, ok := model.state.Availability.Get()
		if ok && availability == presentationdomain.AvailabilityAuthenticationFailed {
			return model.emitCommand(presentationdomain.Command{
				Kind:            presentationdomain.CommandRetryAuthentication,
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			})
		}
	case 'l':
		return model.openSelector()
	case 'p':
		return model.cycleModel(1)
	}
	return model, nil
}

// openSelector highlights the current model without changing editor or transcript state.
func (model Model) openSelector() (tea.Model, tea.Cmd) {
	if len(model.state.Models) == 0 {
		return model, nil
	}
	model.selectorOpen = true
	model.selectorRow = model.currentModelIndex()
	return model, nil
}

// updateSelector handles only modal navigation, confirmation, and cancellation.
func (model Model) updateSelector(key tea.Key) (tea.Model, tea.Cmd) {
	rowCount := len(model.state.Models)
	if model.sessionSelector {
		rowCount = len(model.state.Sessions)
	}
	if rowCount == 0 {
		if key.Code == tea.KeyEscape {
			model = model.cancelSelector()
		}
		return model, nil
	}
	if model.sessionSelector && model.resumePending {
		if key.Code == tea.KeyEscape {
			model = model.cancelSelector()
		}
		return model, nil
	}
	switch key.Code {
	case tea.KeyUp:
		model.selectorRow = (model.selectorRow - 1 + rowCount) % rowCount
	case tea.KeyDown:
		model.selectorRow = (model.selectorRow + 1) % rowCount
	case tea.KeyEnter:
		if model.sessionSelector {
			selected := model.state.Sessions[model.selectorRow]
			// SessionChanged or Escape owns selector closure so a rejected resume preserves user state.
			model.resumePending = true
			model.resumeStatus = ""
			return model.emitSessionCommand(presentationdomain.CommandResumeSession, selected.Info.ID, "")
		}
		selected := model.state.Models[model.selectorRow]
		model.selectorOpen = false
		return model.emitCommand(presentationdomain.Command{
			Kind:            presentationdomain.CommandSelectModel,
			Text:            mo.None[string](),
			ProviderID:      mo.Some(selected.ProviderID),
			ModelID:         mo.Some(selected.ModelID),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		})
	case tea.KeyEscape:
		model = model.cancelSelector()
	}
	return model, nil
}

// cancelSelector discards a resume draft only when the user cancels its selector.
func (model Model) cancelSelector() Model {
	if model.sessionSelector {
		model.input = nil
		model.cursor = 0
	}
	model.selectorOpen = false
	model.sessionSelector = false
	model.resumePending = false
	model.resumeStatus = ""
	return model
}

// cycleModel emits the configured neighbor of the Host-confirmed model.
func (model Model) cycleModel(direction int) (tea.Model, tea.Cmd) {
	if len(model.state.Models) <= 1 {
		return model, nil
	}
	index := (model.currentModelIndex() + direction + len(model.state.Models)) % len(model.state.Models)
	selected := model.state.Models[index]
	return model.emitCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSelectModel,
		Text:            mo.None[string](),
		ProviderID:      mo.Some(selected.ProviderID),
		ModelID:         mo.Some(selected.ModelID),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	})
}

// cycleReasoning emits the next configured level for the Host-confirmed model.
func (model Model) cycleReasoning() (tea.Model, tea.Cmd) {
	if len(model.state.Models) == 0 {
		return model, nil
	}
	configured := model.state.Models[model.currentModelIndex()]
	if len(configured.Reasoning.Choices) <= 1 {
		return model, nil
	}
	index := 0
	selection, ok := model.state.ModelSelection.Get()
	if !ok {
		return model, nil
	}
	for current, level := range configured.Reasoning.Choices {
		if level == selection.ReasoningChoice {
			index = (current + 1) % len(configured.Reasoning.Choices)
			break
		}
	}
	return model.emitCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSelectReasoningChoice,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.Some(configured.Reasoning.Choices[index]),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	})
}

// currentModelIndex resolves the Host-confirmed selection in configured order.
func (model Model) currentModelIndex() int {
	selection, ok := model.state.ModelSelection.Get()
	if !ok {
		return 0
	}
	index := slices.IndexFunc(model.state.Models, func(configured presentationdomain.ConfiguredModel) bool {
		return configured.ProviderID == selection.ProviderID && configured.ModelID == selection.ModelID
	})
	return max(index, 0)
}

// emitCommand serializes one UI command without blocking the update loop.
func (model Model) emitCommand(command presentationdomain.Command) (tea.Model, tea.Cmd) {
	if model.emitting {
		return model, nil
	}
	model.emitting = true

	return model, func() tea.Msg {
		return emissionResultMsg{
			command: command,
			err:     model.emit(command),
		}
	}
}

// emitSessionCommand preserves the editor until the Host confirms or rejects the lifecycle operation.
func (model Model) emitSessionCommand(kind presentationdomain.CommandKind, id, name string) (tea.Model, tea.Cmd) {
	command := presentationdomain.Command{
		Kind:            kind,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.EmptyableToOption(id),
		SessionName:     mo.EmptyableToOption(name),
	}
	if kind == presentationdomain.CommandSetSessionName {
		command.SessionName = mo.Some(name)
	}
	// Keep the editor unchanged until a Host frame confirms the lifecycle operation.
	return model.emitCommand(command)
}

// formatSessionInformation renders metadata, message counts, and optional token usage.
func formatSessionInformation(
	info presentationdomain.SessionInfo,
	statistics mo.Option[presentationdomain.SessionStatistics],
) string {
	lines := []string{formatSessionInfo(info)}
	if values, present := statistics.Get(); present {
		lines = append(lines,
			fmt.Sprintf(
				"Messages: %d user, %d model, %d tool results, %d total",
				values.UserMessages, values.ModelResponses, values.ToolResults, values.TotalMessages,
			),
			fmt.Sprintf("Tool calls: %d", values.ToolCalls),
		)
		if tokens, available := values.TokenUsage.Get(); available {
			lines = append(lines,
				fmt.Sprintf(
					"Tokens: %d input, %d output, %d cache read, %d cache write, %d total",
					tokens.InputTokens, tokens.OutputTokens, tokens.CacheReadTokens,
					tokens.CacheWriteTokens, tokens.TotalTokens,
				),
				fmt.Sprintf("Reasoning tokens: %d, included in output", tokens.ReasoningTokens),
			)
		} else {
			lines = append(lines, "Tokens: unavailable")
		}
	}
	return strings.Join(lines, "\n")
}

// formatSessionInfo renders lifecycle-only session metadata as safe presentation text.
func formatSessionInfo(info presentationdomain.SessionInfo) string {
	name := "<absent>"
	if info.NamePresent {
		name = info.Name
	}
	storagePath := "<absent>"
	if info.StoragePresent {
		storagePath = info.StoragePath
	}
	return strings.Join([]string{
		"Session ID: " + info.ID,
		"Name: " + name,
		"Working directory: " + info.WorkingDirectory,
		"Storage path: " + storagePath,
		"Created: " + info.CreatedAt.UTC().Format(time.RFC3339Nano),
		"Updated: " + info.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, "\n")
}

// sessionInformationEvent adapts formatted session metadata to a non-session-changing information event.
func sessionInformationEvent(text string) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInformation,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.Some(text),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
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
	selector := model.visibleSelectorLines()
	body := model.visibleBodyLines(len(selector))
	lines := make([]string, 0, fixedViewLineCount+len(body)+len(selector))
	lines = append(
		lines,
		"Glyph",
		"Status: "+availabilityText(model.state.Availability)+" | "+selectionText(model.state.ModelSelection),
	)
	lines = append(lines, body...)
	lines = append(lines, selector...)
	selectionKeys := "Keys: Enter submit | Ctrl+L models | Ctrl+P next model | Shift+Ctrl+P previous model"
	if model.reasoningSelectionVisible() {
		selectionKeys += " | Shift+Tab reasoning"
	}
	lines = append(
		lines,
		"Request: "+string(model.input[:model.cursor])+"|"+string(model.input[model.cursor:]),
		fmt.Sprintf("Terminal: %dx%d", model.width, model.height),
		selectionKeys+" | Ctrl+T reasoning display | Ctrl+C stop | Ctrl+R retry authentication | Ctrl+Q quit",
	)

	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true

	return view
}

// reasoningSelectionVisible hides the shortcut when the selected model has no effective alternative.
func (model Model) reasoningSelectionVisible() bool {
	if len(model.state.Models) == 0 {
		return true
	}
	configured := model.state.Models[model.currentModelIndex()]
	return len(configured.Reasoning.Choices) > 1
}

// visibleSelectorLines renders a bounded window around the highlighted model.
func (model Model) visibleSelectorLines() []string {
	rowCount := len(model.state.Models)
	title := "Models:"
	if model.sessionSelector {
		title = "Sessions:"
		rowCount = len(model.state.Sessions)
	}
	if !model.selectorOpen || rowCount == 0 {
		return nil
	}
	statusLineCount := 0
	if model.sessionSelector && model.resumeStatus != "" {
		statusLineCount = 1
	}
	capacity := min(maxVisibleSelectorRows, rowCount)
	if model.height > 0 {
		capacity = min(capacity, max(1, model.height-fixedViewLineCount-selectorFixedLineCount-statusLineCount))
	}
	start := model.selectorRow - capacity/selectorCenterDivisor
	start = max(0, min(start, rowCount-capacity))
	lines := make([]string, 0, selectorFixedLineCount+statusLineCount+capacity)
	lines = append(lines, title)
	for index := start; index < start+capacity; index++ {
		prefix := "  "
		if index == model.selectorRow {
			prefix = "> "
		}
		if model.sessionSelector {
			summary := model.state.Sessions[index]
			label := summary.Info.ID
			if summary.Info.NamePresent {
				label = summary.Info.Name
			} else if summary.TextPresent {
				label = summary.FirstUserText
			}
			row := fmt.Sprintf(
				"%s | %s | %d messages", label, summary.Info.UpdatedAt.Format(time.RFC3339), summary.TotalMessages,
			)
			lines = append(lines, prefix+ellipsize(row, max(1, model.width-len(prefix))))
			continue
		}
		configured := model.state.Models[index]
		lines = append(lines, prefix+configured.ProviderID+" / "+configured.ModelID)
	}
	if statusLineCount > 0 {
		lines = append(lines, ellipsize("Session status: "+model.resumeStatus, max(1, model.width)))
	}
	return append(lines, "Selector: Up/Down navigate | Enter confirm | Escape cancel")
}

// ellipsize keeps selector rows single-line and rune-safe within the available terminal width.
func ellipsize(value string, width int) string {
	normalized := strings.Join(strings.Fields(value), " ")
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(normalized) <= width {
		return normalized
	}
	return ansi.Truncate(normalized, width, "…")
}

// visibleBodyLines keeps the latest transcript after reserving fixed and selector lines.
//
//nolint:gocyclo // The flat branches preserve transcript order across visible content kinds.
func (model Model) visibleBodyLines(reservedLines int) []string {
	estimatedLines := len(model.state.Startup) + len(model.state.Transcript) +
		len(model.state.ActiveModel) + len(model.state.ActiveToolCalls) + 1
	lines := make([]string, 0, estimatedLines)
	for _, line := range model.state.Startup {
		lines = appendWrappedBodyLine(lines, renderLine(line), model.width)
	}
	for _, line := range model.state.Transcript {
		if line.Kind == presentationdomain.LineReasoning && !model.reasoningExpanded {
			lines = appendWrappedBodyLine(lines, "Reasoning: [collapsed]", model.width)
			continue
		}
		lines = appendWrappedBodyLine(lines, renderLine(line), model.width)
	}
	positions := make([]int, 0, len(model.state.ActiveModel))
	for position := range model.state.ActiveModel {
		positions = append(positions, position)
	}
	slices.Sort(positions)
	for _, position := range positions {
		content := model.state.ActiveModel[position]
		kind := presentationdomain.LineModel
		contentKind, kindOK := content.Kind.Get()
		if !kindOK {
			contentKind = presentationdomain.ModelContentUnspecified
		}
		switch contentKind {
		case presentationdomain.ModelContentRefusal:
			kind = presentationdomain.LineRefusal
		case presentationdomain.ModelContentReasoning:
			kind = presentationdomain.LineReasoning
		case presentationdomain.ModelContentText, presentationdomain.ModelContentUnspecified:
		}
		if kind == presentationdomain.LineReasoning && !model.reasoningExpanded {
			lines = appendWrappedBodyLine(lines, "Reasoning: [collapsed]", model.width)
			continue
		}
		lines = appendWrappedBodyLine(
			lines,
			renderLine(presentationdomain.Line{
				Kind:     kind,
				Text:     content.Text,
				ToolName: mo.None[string](),
				Status:   mo.None[string](),
				Contents: mo.None[[]presentationdomain.Content](),
			}),
			model.width,
		)
	}
	calls := make([]presentationdomain.ToolCallState, 0, len(model.state.ActiveToolCalls))
	for _, call := range model.state.ActiveToolCalls {
		calls = append(calls, call)
	}
	slices.SortFunc(calls, func(left, right presentationdomain.ToolCallState) int {
		return cmp.Compare(left.Position, right.Position)
	})
	for _, call := range calls {
		lines = appendWrappedBodyLine(lines, renderToolCall(call), model.width)
	}
	if authorizationURL, ok := model.state.AuthorizationURL.Get(); ok {
		lines = appendWrappedBodyLine(lines, "Authorization: "+authorizationURL, model.width)
	}

	if model.height <= 0 {
		return lines
	}
	capacity := model.height - fixedViewLineCount - reservedLines
	if capacity <= 0 {
		return nil
	}
	if len(lines) > capacity {
		return lines[len(lines)-capacity:]
	}
	return lines
}

// appendWrappedBodyLine converts one logical body line into readable terminal-width visual lines.
func appendWrappedBodyLine(lines []string, line string, width int) []string {
	if width <= 0 {
		return append(lines, strings.Split(line, "\n")...)
	}

	return append(lines, strings.Split(ansi.Wrap(line, width, ""), "\n")...)
}

func renderToolCall(call presentationdomain.ToolCallState) string {
	status := "final"
	if call.Provisional {
		status = "provisional"
	}
	parts := make([]string, 0, len(call.Fields)+1)
	for _, field := range call.Fields {
		if value, ok := field.Value.Get(); ok {
			encoded, _ := json.Marshal(value)
			parts = append(parts, field.Name+"="+string(encoded))
		} else if prefix, prefixOK := field.Prefix.Get(); prefixOK {
			parts = append(parts, field.Name+"="+prefix)
		}
	}
	if !call.Provisional && call.Arguments != nil {
		arguments, _ := json.Marshal(call.Arguments)
		parts = []string{string(arguments)}
	}
	line := "[tool:call] " + call.Name + " (" + status + ")"
	if len(parts) > 0 {
		line += " " + strings.Join(parts, " ")
	}
	return line
}

// linePrefix assigns a stable prefix to non-tool presentation lines.
func linePrefix(kind presentationdomain.LineKind) string {
	switch kind {
	case presentationdomain.LineInformation, presentationdomain.LineUnspecified:
		return "[info]"
	case presentationdomain.LineError:
		return "[error]"
	case presentationdomain.LineWarning:
		return "[warning]"
	case presentationdomain.LineUser:
		return "user:"
	case presentationdomain.LineModel:
		return "assistant:"
	case presentationdomain.LineRefusal:
		return "[refusal]"
	case presentationdomain.LineReasoning:
		return "reasoning:"
	case presentationdomain.LineToolStatus, presentationdomain.LineToolStdout,
		presentationdomain.LineToolStderr, presentationdomain.LineToolDone,
		presentationdomain.LineToolError:
		return toolLinePrefix(kind)
	default:
		return ""
	}
}

// toolLinePrefix assigns a stable prefix to tool presentation lines.
func toolLinePrefix(kind presentationdomain.LineKind) string {
	switch kind {
	case presentationdomain.LineToolStatus:
		return "[tool:status]"
	case presentationdomain.LineToolStdout:
		return "[tool:stdout]"
	case presentationdomain.LineToolStderr:
		return "[tool:stderr]"
	case presentationdomain.LineToolDone:
		return "[tool:done]"
	case presentationdomain.LineToolError:
		return "[tool:error]"
	case presentationdomain.LineUnspecified, presentationdomain.LineInformation,
		presentationdomain.LineError, presentationdomain.LineWarning, presentationdomain.LineUser,
		presentationdomain.LineModel, presentationdomain.LineRefusal, presentationdomain.LineReasoning:
		return ""
	}
	return ""
}

// renderLine assigns one stable terminal prefix to each presentation line kind.
func renderLine(line presentationdomain.Line) string {
	prefix := linePrefix(line.Kind)

	parts := []string{prefix}
	if toolName, ok := line.ToolName.Get(); ok && toolName != "" {
		parts = append(parts, toolName)
	}
	if status, ok := line.Status.Get(); ok && status != "" {
		parts = append(parts, "("+status+")")
	}
	if text, ok := line.Text.Get(); ok && text != "" {
		parts = append(parts, text)
	}

	return strings.Join(parts, " ")
}

// selectionText renders only the Host-confirmed selection.
func selectionText(selectionOption mo.Option[presentationdomain.ModelSelection]) string {
	selection, ok := selectionOption.Get()
	if !ok || selection.ProviderID == "" || selection.ModelID == "" {
		return "model unavailable"
	}
	return fmt.Sprintf("%s / %s / %s", selection.ProviderID, selection.ModelID, reasoningText(selection.ReasoningChoice))
}

// reasoningText maps the closed reasoning set to its configured spelling.
func reasoningText(level presentationdomain.ReasoningChoice) string {
	switch level {
	case presentationdomain.ReasoningChoiceOff:
		return "off"
	case presentationdomain.ReasoningChoiceOn:
		return "on"
	case presentationdomain.ReasoningChoiceMinimal:
		return "minimal"
	case presentationdomain.ReasoningChoiceLow:
		return "low"
	case presentationdomain.ReasoningChoiceMedium:
		return "medium"
	case presentationdomain.ReasoningChoiceHigh:
		return "high"
	case presentationdomain.ReasoningChoiceXHigh:
		return "xhigh"
	case presentationdomain.ReasoningChoiceMax:
		return "max"
	case presentationdomain.ReasoningChoiceUnspecified:
		return "unspecified"
	default:
		return "unspecified"
	}
}

// availabilityText maps Host availability to concise terminal status text.
func availabilityText(availabilityOption mo.Option[presentationdomain.Availability]) string {
	availability, ok := availabilityOption.Get()
	if !ok {
		return "Unavailable"
	}
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
