package presentation

import (
	"strings"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

	"github.com/samber/mo"
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

// applyEvent updates presentation state and the editor after one Host event.
func (model interaction) applyEvent(event event) interaction {
	preserveRejectedResume := model.selectorOpen && model.sessionSelector && model.resumePending &&
		event.Kind == eventInformation
	if !preserveRejectedResume {
		model.applyProjection(event)
	}
	if treeUpdate, present := event.treeEvent.Get(); present {
		model = model.applyTreeEvent(event.Kind, treeUpdate)
	}
	switch event.Kind {
	case eventSessionList:
		// The list refreshes selection data, while confirmation or cancellation still owns the draft.
		model.resumePending = false
		model.resumeStatus = ""
		model.selectorOpen = len(model.state.Sessions) > 0
		model.sessionSelector = model.selectorOpen
		model.selectorRow = 0
	case eventSessionChanged:
		// Replacement confirmation owns the point where the editor and selector can discard old-session input.
		model.resumePending = false
		model.resumeStatus = ""
		model.input = nil
		model.cursor = 0
		model.selectorOpen = false
		model.sessionSelector = false
	case eventSessionInformation:
		// Information confirms /session or /name without replacing transcript ownership.
		if info, present := event.SessionInfo.Get(); present {
			model.applyProjection(
				sessionInformationEvent(formatSessionInformation(info, event.SessionStatistics)),
			)
		}
		model.input = nil
		model.cursor = 0
	case eventInformation:
		if preserveRejectedResume {
			model.resumePending = false
			model.resumeStatus, _ = event.Text.Get()
		}
	case eventUnspecified, eventInitialization,
		eventUserSubmitted, eventAvailability,
		eventTurnStarted, eventModelDelta,
		eventModelEnd, eventToolCallPreview,
		eventToolCallFinal, eventToolStarted,
		eventToolProgress, eventToolOutput,
		eventToolEnded, eventToolResult,
		eventTurnEnded, eventAgentSettled,
		eventAuthorization, eventError,
		eventModelSelectionChanged,
		eventSessionTree, eventSessionTreeNavigationProgress,
		eventSessionTreeNavigation,
		eventTreeOperationFailed, eventSessionForked,
		eventSessionCloned, eventEntryLabelSet,
		eventSessionEntryAdded:
	}
	return model
}

// applyEmissionResult clears accepted input or renders a delivery failure.
func (model interaction) applyEmissionResult(message emissionResultMsg) (interaction, bool) {
	model.emitting = false
	if message.err != nil {
		if message.command.TreeCommand.IsSome() {
			model.treeAwaiting = CommandUnspecified
			model.treeRequest = mo.None[TreePurpose]()
			model.treeStatus = commandSendFailurePrefix + message.err.Error()
			return model, false
		}
		model.applyProjection(event{
			RestoredTranscript:   nil,
			Kind:                 eventError,
			Startup:              nil,
			Availability:         mo.None[Availability](),
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[OutputStream](),
			Text:                 mo.Some(commandSendFailurePrefix + message.err.Error()),
			Contents:             mo.None[[]Content](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[ModelSelection](),
			SessionInfo:          mo.None[SessionInfo](),
			Sessions:             nil,
			SessionStatistics:    mo.None[SessionStatistics](),
			treeEvent:            mo.None[treeEvent](),
		})
		return model, false
	}

	switch message.command.Kind {
	case CommandSubmit:
		if message.command.Text.IsNone() {
			return model, false
		}
		model.applyProjection(event{
			RestoredTranscript:   nil,
			Kind:                 eventUserSubmitted,
			Startup:              nil,
			Availability:         mo.None[Availability](),
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[OutputStream](),
			Text:                 message.command.Text,
			Contents:             mo.None[[]Content](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[ModelSelection](),
			SessionInfo:          mo.None[SessionInfo](),
			Sessions:             nil,
			SessionStatistics:    mo.None[SessionStatistics](),
			treeEvent:            mo.None[treeEvent](),
		})
		model.input = nil
		model.cursor = 0
	case CommandQuit:
		return model, true
	case CommandUnspecified,
		CommandStop,
		CommandRetryAuthentication,
		CommandSelectModel,
		CommandSelectReasoningChoice,
		CommandCreateSession,
		CommandListSessions,
		CommandResumeSession,
		CommandSetSessionName,
		CommandGetSessionInfo,
		CommandGetSessionTree,
		CommandNavigateSessionTree,
		CommandForkSession,
		CommandCloneSession,
		CommandSetEntryLabel:
	}

	return model, false
}

//nolint:gocyclo // The explicit flat switch mirrors the supported editor and command keys.
func (model interaction) updateKey(key inputcontroller.Key) (interaction, *commandIntent) {
	if updated, command, handled := model.updateFocusedTreeKey(key); handled {
		return updated, command
	}
	if updated, handled := model.updateTranscriptDisplayKey(key); handled {
		return updated, nil
	}
	if isSelectionShortcut(key) {
		availability, ok := model.state.Availability.Get()
		if !ok || !availability.SelectionAllowed() {
			return model, nil
		}
	}
	if model.selectorOpen {
		return model.updateSelector(key)
	}
	if model.emitting {
		return model, nil
	}
	if key.Mod == inputcontroller.ModCtrl|inputcontroller.ModShift && key.Code == 'p' {
		return model.cycleModel(-1)
	}
	if key.Mod == inputcontroller.ModCtrl {
		return model.updateControlKey(key.Code)
	}
	if key.Mod == inputcontroller.ModShift && key.Code == inputcontroller.KeyTab {
		return model.cycleReasoning()
	}

	availability, ok := model.state.Availability.Get()
	if !ok || (availability != AvailabilityIdle &&
		availability != AvailabilityRunning) {
		return model, nil
	}
	if availability == AvailabilityRunning && len(model.input) == 0 &&
		key.Code != inputcontroller.KeyEnter && key.Text != slashCommandPrefix {
		return model, nil
	}

	switch key.Code {
	case inputcontroller.KeyEnter:
		return model.updateEnter(availability)
	case inputcontroller.KeyLeft:
		if model.cursor > 0 {
			model.cursor--
		}
	case inputcontroller.KeyRight:
		if model.cursor < len(model.input) {
			model.cursor++
		}
	case inputcontroller.KeyHome:
		model.cursor = 0
	case inputcontroller.KeyEnd:
		model.cursor = len(model.input)
	case inputcontroller.KeyBackspace:
		if model.cursor > 0 {
			model.input = append(model.input[:model.cursor-1], model.input[model.cursor:]...)
			model.cursor--
		}
	case inputcontroller.KeyDelete:
		if model.cursor < len(model.input) {
			model.input = append(model.input[:model.cursor], model.input[model.cursor+1:]...)
		}
	default:
		if key.Mod&(inputcontroller.ModCtrl|inputcontroller.ModAlt|inputcontroller.ModMeta) == 0 {
			model.insertText(key.Text)
		}
	}

	return model, nil
}

// updateTranscriptDisplayKey applies local transcript display shortcuts.
func (model interaction) updateTranscriptDisplayKey(key inputcontroller.Key) (interaction, bool) {
	if key.Mod != inputcontroller.ModCtrl {
		return model, false
	}
	switch key.Code {
	case 't':
		model.reasoningExpanded = !model.reasoningExpanded
		return model, true
	case 'o':
		model.branchSummariesExpanded = !model.branchSummariesExpanded
		return model, true
	default:
		return model, false
	}
}

// updateEnter interprets session commands before treating input as an agent request.
func (model interaction) updateEnter(availability Availability) (interaction, *commandIntent) {
	text := strings.TrimSpace(string(model.input))
	if text == "" {
		return model, nil
	}
	if updated, command, handled := model.updateTreeSlashCommand(text, availability); handled {
		return updated, command
	}
	switch text {
	case slashCommandModel:
		if availability != AvailabilityIdle {
			return model, nil
		}
		model.input = nil
		model.cursor = 0
		return model.openSelector()
	case slashCommandNew:
		return model.emitSessionCommand(CommandCreateSession, "", "")
	case slashCommandResume:
		return model.emitSessionCommand(CommandListSessions, "", "")
	case slashCommandSession:
		return model.emitSessionCommand(CommandGetSessionInfo, "", "")
	case slashCommandName:
		message := slashCommandNameUsage
		if info, present := model.state.SessionInfo.Get(); present && info.NamePresent {
			message = info.Name
		}
		model.applyProjection(sessionInformationEvent(message))
		model.input = nil
		model.cursor = 0
		return model, nil
	}
	if after, ok := strings.CutPrefix(text, slashCommandName+slashCommandArgumentSeparator); ok {
		name := after
		return model.emitSessionCommand(CommandSetSessionName, "", name)
	}
	if availability != AvailabilityIdle {
		return model, nil
	}
	return model.emitCommand(Command{
		Kind:            CommandSubmit,
		Text:            mo.Some(text),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TreeCommand:     mo.None[TreeCommand](),
	})
}

// isSelectionShortcut matches only the approved selection bindings.
func isSelectionShortcut(key inputcontroller.Key) bool {
	return key.Mod == inputcontroller.ModCtrl && (key.Code == 'l' || key.Code == 'p') ||
		key.Mod == inputcontroller.ModCtrl|inputcontroller.ModShift && key.Code == 'p' ||
		key.Mod == inputcontroller.ModShift && key.Code == inputcontroller.KeyTab
}

// emptyCommand creates a command without an operation-specific payload.
func emptyCommand(kind CommandKind) Command {
	return Command{
		Kind:            kind,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TreeCommand:     mo.None[TreeCommand](),
	}
}

// updateControlKey handles the exact control-key bindings.
func (model interaction) updateControlKey(code rune) (interaction, *commandIntent) {
	switch code {
	case 'q':
		return model.emitCommand(emptyCommand(CommandQuit))
	case 'c':
		if availability, ok := model.state.Availability.Get(); ok &&
			availability == AvailabilityRunning {
			return model.emitCommand(emptyCommand(CommandStop))
		}
	case 'r':
		availability, ok := model.state.Availability.Get()
		if ok && availability == AvailabilityAuthenticationFailed {
			return model.emitCommand(emptyCommand(CommandRetryAuthentication))
		}
	case 'l':
		return model.openSelector()
	case 'p':
		return model.cycleModel(1)
	}
	return model, nil
}
