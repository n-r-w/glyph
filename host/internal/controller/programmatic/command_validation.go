package programmatic

import (
	"strings"
)

// Valid reports whether the command payload matches its discriminator.
func (command Command) Valid() bool {
	if invalid, handled := command.invalidSessionCommand(); handled {
		return !invalid
	}
	switch command.Kind {
	case CommandUserRequest:
		return !command.invalidUserRequest()
	case CommandAbort, CommandGetRunState, CommandGetMessages, CommandGetModels:
		return command.UserText.IsNone() && !command.hasModelArguments() && !command.hasSessionArguments()
	case CommandSelectModel:
		return !command.invalidModelSelection()
	case CommandSelectReasoningChoice:
		return !command.invalidReasoningSelection()
	case CommandCreateSession, CommandListSessions, CommandResumeSession, CommandSetSessionName,
		CommandGetSessionInfo, CommandGetSessionEntries, CommandGetSessionStats, CommandGetSessionTree,
		CommandNavigateSessionTree, CommandUnspecified:
		return false
	default:
		return false
	}
}

// invalidSessionCommand validates session command payloads and reports whether the kind was handled.
func (command Command) invalidSessionCommand() (invalid, handled bool) {
	switch command.Kind {
	case CommandCreateSession, CommandListSessions, CommandGetSessionInfo, CommandGetSessionEntries,
		CommandGetSessionStats, CommandGetSessionTree:
		return command.UserText.IsSome() || command.hasModelArguments() || command.hasSessionArguments(), true
	case CommandResumeSession:
		return command.invalidResumeSession(), true
	case CommandSetSessionName:
		return command.invalidSessionName(), true
	case CommandNavigateSessionTree:
		return command.invalidTreeNavigation(), true
	case CommandUnspecified, CommandUserRequest, CommandAbort, CommandGetRunState, CommandGetMessages,
		CommandGetModels, CommandSelectModel, CommandSelectReasoningChoice:
		return false, false
	default:
		return false, false
	}
}

// invalidResumeSession reports a malformed session-resume payload.
func (command Command) invalidResumeSession() bool {
	id, present := command.SessionID.Get()
	return !present || id == "" || command.SessionName.IsSome() || command.UserText.IsSome() ||
		command.hasModelArguments()
}

// invalidSessionName reports a malformed session-name payload.
func (command Command) invalidSessionName() bool {
	return command.SessionName.IsNone() || command.SessionID.IsSome() || command.UserText.IsSome() ||
		command.hasModelArguments() || command.hasTreeArguments()
}

// invalidTreeNavigation reports a malformed tree-navigation payload.
func (command Command) invalidTreeNavigation() bool {
	targetID, present := command.TargetEntryID.Get()
	customFocus := command.CustomFocus.OrEmpty()
	invalidFocus := command.SummaryMode == SummaryModeSummarizeWithCustomPrompt && customFocus == "" ||
		command.SummaryMode != SummaryModeSummarizeWithCustomPrompt && customFocus != ""
	return !present || targetID == "" || invalidFocus || command.UserText.IsSome() ||
		command.hasModelArguments() || command.SessionID.IsSome() || command.SessionName.IsSome()
}

// invalidUserRequest reports a malformed user request payload.
func (command Command) invalidUserRequest() bool {
	userText, present := command.UserText.Get()
	return !present || strings.TrimSpace(userText) == "" || command.hasModelArguments() ||
		command.hasSessionArguments()
}

// invalidModelSelection reports a malformed model selection payload.
func (command Command) invalidModelSelection() bool {
	providerID, providerPresent := command.ProviderID.Get()
	modelID, modelPresent := command.ModelID.Get()
	return command.UserText.IsSome() || !providerPresent || providerID == "" || !modelPresent || modelID == "" ||
		command.ReasoningChoice.IsSome() || command.hasSessionArguments()
}

// invalidReasoningSelection reports a malformed reasoning selection payload.
func (command Command) invalidReasoningSelection() bool {
	reasoningChoice, reasoningPresent := command.ReasoningChoice.Get()
	return command.UserText.IsSome() || command.ProviderID.IsSome() || command.ModelID.IsSome() ||
		!reasoningPresent || !reasoningChoice.Valid() || command.hasSessionArguments()
}

// hasModelArguments reports model-selection arguments.
func (command Command) hasModelArguments() bool {
	return command.ProviderID.IsSome() || command.ModelID.IsSome() || command.ReasoningChoice.IsSome()
}

// hasTreeArguments reports tree-navigation arguments.
func (command Command) hasTreeArguments() bool {
	return command.TargetEntryID.IsSome() || command.CustomFocus.IsSome() || command.SummaryMode != SummaryModeNoSummary
}

// hasSessionArguments reports session arguments.
func (command Command) hasSessionArguments() bool {
	return command.SessionID.IsSome() || command.SessionName.IsSome() || command.hasTreeArguments()
}
