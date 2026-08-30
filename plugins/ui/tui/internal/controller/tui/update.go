package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

const (
	// slashCommandPrefix starts every slash command.
	slashCommandPrefix = "/"
	// slashCommandArgumentSeparator separates a command from its argument.
	slashCommandArgumentSeparator = " "
	// slashCommandValuePlaceholder describes one required command value.
	slashCommandValuePlaceholder = "<value>"
	// slashCommandNameUsage describes the name command argument.
	slashCommandNameUsage = "Usage: " + slashCommandName + slashCommandArgumentSeparator + slashCommandValuePlaceholder
)

const (
	// commandSendFailurePrefix prefixes a UI stream send failure.
	commandSendFailurePrefix = "Could not send command: "
)

const (
	// slashCommandModel opens configured model selection.
	slashCommandModel = "/model"
	// slashCommandNew creates a new session.
	slashCommandNew = "/new"
	// slashCommandResume opens stored session selection.
	slashCommandResume = "/resume"
	// slashCommandSession shows active session information.
	slashCommandSession = "/session"
	// slashCommandTree opens active session tree navigation.
	slashCommandTree = "/tree"
	// slashCommandFork opens user-message fork selection.
	slashCommandFork = "/fork"
	// slashCommandClone clones the active branch.
	slashCommandClone = "/clone"
	// slashCommandName reads or changes the active session name.
	slashCommandName = "/name"
)

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
	if treeEvent, present := event.TreeEvent.Get(); present {
		model = model.applyTreeEvent(event.Kind, treeEvent)
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
		presentationdomain.EventModelSelectionChanged,
		presentationdomain.EventSessionTree, presentationdomain.EventSessionTreeNavigation,
		presentationdomain.EventSessionTreeFailed, presentationdomain.EventSessionForked,
		presentationdomain.EventSessionCloned, presentationdomain.EventEntryLabelSet:
	}
	return model
}

// applyEmissionResult clears accepted input or renders a delivery failure.
func (model Model) applyEmissionResult(message emissionResultMsg) (tea.Model, tea.Cmd) {
	model.emitting = false
	if message.err != nil {
		if message.command.TreeCommand.IsSome() {
			model.treeAwaiting = presentationdomain.CommandUnspecified
			model.treeRequest = mo.None[presentationdomain.TreePurpose]()
			model.treeStatus = commandSendFailurePrefix + message.err.Error()
			return model, nil
		}
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
			Text:                 mo.Some(commandSendFailurePrefix + message.err.Error()),
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
			TreeEvent:            mo.None[presentationdomain.TreeEvent](),
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
			TreeEvent:            mo.None[presentationdomain.TreeEvent](),
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
		presentationdomain.CommandGetSessionInfo,
		presentationdomain.CommandGetSessionTree,
		presentationdomain.CommandNavigateSessionTree,
		presentationdomain.CommandForkSession,
		presentationdomain.CommandCloneSession,
		presentationdomain.CommandSetEntryLabel:
	}

	return model, nil
}

//nolint:gocyclo // The explicit flat switch mirrors the supported editor and command keys.
func (model Model) updateKey(key tea.Key) (tea.Model, tea.Cmd) {
	if updated, command, handled := model.updateFocusedTreeKey(key); handled {
		return updated, command
	}
	if isSelectionShortcut(key) {
		availability, ok := model.state.Availability.Get()
		if !ok || !availability.SelectionAllowed() {
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
		key.Code != tea.KeyEnter && key.Text != slashCommandPrefix {
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
	if updated, command, handled := model.updateTreeSlashCommand(text, availability); handled {
		return updated, command
	}
	switch text {
	case slashCommandModel:
		if availability != presentationdomain.AvailabilityIdle {
			return model, nil
		}
		model.input = nil
		model.cursor = 0
		return model.openSelector()
	case slashCommandNew:
		return model.emitSessionCommand(presentationdomain.CommandCreateSession, "", "")
	case slashCommandResume:
		return model.emitSessionCommand(presentationdomain.CommandListSessions, "", "")
	case slashCommandSession:
		return model.emitSessionCommand(presentationdomain.CommandGetSessionInfo, "", "")
	case slashCommandName:
		message := slashCommandNameUsage
		if info, present := model.state.SessionInfo.Get(); present && info.NamePresent {
			message = info.Name
		}
		model.state = model.apply(model.state, sessionInformationEvent(message))
		model.input = nil
		model.cursor = 0
		return model, nil
	}
	if after, ok := strings.CutPrefix(text, slashCommandName+slashCommandArgumentSeparator); ok {
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
		TreeCommand:     mo.None[presentationdomain.TreeCommand](),
	})
}

// isSelectionShortcut matches only the approved selection bindings.
func isSelectionShortcut(key tea.Key) bool {
	return key.Mod == tea.ModCtrl && (key.Code == 'l' || key.Code == 'p') ||
		key.Mod == tea.ModCtrl|tea.ModShift && key.Code == 'p' ||
		key.Mod == tea.ModShift && key.Code == tea.KeyTab
}

// emptyCommand creates a command without an operation-specific payload.
func emptyCommand(kind presentationdomain.CommandKind) presentationdomain.Command {
	return presentationdomain.Command{
		Kind:            kind,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TreeCommand:     mo.None[presentationdomain.TreeCommand](),
	}
}

// updateControlKey handles the exact control-key bindings.
func (model Model) updateControlKey(code rune) (tea.Model, tea.Cmd) {
	switch code {
	case 'q':
		return model.emitCommand(emptyCommand(presentationdomain.CommandQuit))
	case 'c':
		if availability, ok := model.state.Availability.Get(); ok &&
			availability == presentationdomain.AvailabilityRunning {
			return model.emitCommand(emptyCommand(presentationdomain.CommandStop))
		}
	case 'r':
		availability, ok := model.state.Availability.Get()
		if ok && availability == presentationdomain.AvailabilityAuthenticationFailed {
			return model.emitCommand(emptyCommand(presentationdomain.CommandRetryAuthentication))
		}
	case 'l':
		return model.openSelector()
	case 'p':
		return model.cycleModel(1)
	}
	return model, nil
}
