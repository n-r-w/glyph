package ui

import (
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapCommand validates one generated UI command.
func mapCommand(response *uiv1.OpenResponse) (Command, error) {
	if response == nil || response.GetRequest() == nil {
		return Command{}, errors.New("receive UI command: operation request is required")
	}
	command, err := mapUIRequest(response.GetRequest())
	command.OperationID = response.GetOperationId()
	return command, err
}

// mapUIRequest validates one typed UI operation request.
func mapUIRequest(command *uiv1.UIRequest) (Command, error) {
	if mapped, handled, err := mapSessionCommand(command); handled {
		return mapped, err
	}
	if mapped, handled, err := mapSelectionCommand(command); handled {
		return mapped, err
	}
	switch {
	case command.GetSubmit() != nil:
		submit := command.GetSubmit()
		if !submit.HasText() {
			return Command{}, errors.New("receive UI command: submit text is required")
		}
		return newCommand(CommandSubmit, mo.Some(submit.GetText())), nil
	case command.GetRetryAuthentication() != nil:
		return newCommand(CommandRetryAuthentication, mo.None[string]()), nil
	case command.GetSelectModel() != nil, command.GetSelectReasoningChoice() != nil:
		return Command{}, errors.New("receive UI command: selection command was not mapped")
	case command.GetCreateSession() != nil, command.GetListSessions() != nil,
		command.GetGetSessionInfo() != nil, command.GetResumeSession() != nil,
		command.GetSetSessionName() != nil, command.GetGetSessionTree() != nil,
		command.GetNavigateSessionTree() != nil, command.GetForkSession() != nil,
		command.GetCloneSession() != nil, command.GetSetEntryLabel() != nil:
		return Command{}, errors.New("receive UI command: session command was not mapped")
	default:
		return Command{}, errors.New("receive UI command: payload is required")
	}
}

// mapSelectionCommand validates model and reasoning payloads before they reach the Host use case.
func mapSelectionCommand(command *uiv1.UIRequest) (Command, bool, error) {
	switch {
	case command.GetSelectModel() != nil:
		selected := command.GetSelectModel()
		if !selected.HasProviderId() || !selected.HasModelId() ||
			selected.GetProviderId() == "" || selected.GetModelId() == "" {
			return Command{}, true, errors.New("receive UI command: provider and model are required")
		}
		return Command{
			OperationID:     "",
			Kind:            CommandSelectModel,
			ProviderID:      mo.Some(selected.GetProviderId()),
			ModelID:         mo.Some(selected.GetModelId()),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
		}, true, nil
	case command.GetSelectReasoningChoice() != nil:
		selected := command.GetSelectReasoningChoice()
		if !selected.HasChoice() {
			return Command{}, true, errors.New("receive UI command: reasoning choice is required")
		}
		choice, err := mapReasoningChoiceFromProto(selected.GetChoice())
		if err != nil {
			return Command{}, true, err
		}
		return Command{
			OperationID:     "",
			Kind:            CommandSelectReasoningChoice,
			ReasoningChoice: mo.Some(choice),
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
		}, true, nil
	default:
		return Command{}, false, nil
	}
}

// mapSessionCommand validates lifecycle command arguments at the protobuf boundary.
//
//nolint:gocyclo // The switch maps every closed session command kind explicitly.
func mapSessionCommand(command *uiv1.UIRequest) (Command, bool, error) {
	switch {
	case command.GetCreateSession() != nil:
		return newCommand(CommandCreateSession, mo.None[string]()), true, nil
	case command.GetListSessions() != nil:
		return newCommand(CommandListSessions, mo.None[string]()), true, nil
	case command.GetGetSessionInfo() != nil:
		return newCommand(CommandGetSessionInfo, mo.None[string]()), true, nil
	case command.GetGetSessionTree() != nil:
		return newCommand(CommandGetSessionTree, mo.None[string]()), true, nil
	case command.GetNavigateSessionTree() != nil:
		navigate := command.GetNavigateSessionTree()
		mapped := newCommand(CommandNavigateSessionTree, mo.None[string]())
		if navigate.HasTargetEntryId() {
			mapped.TargetEntryID = mo.Some(navigate.GetTargetEntryId())
		}
		mapped.SummaryMode = mapSummaryModeFromProto(navigate.GetSummaryMode())
		if navigate.HasCustomFocus() {
			mapped.CustomFocus = mo.Some(navigate.GetCustomFocus())
		}
		return mapped, true, nil
	case command.GetForkSession() != nil:
		fork := command.GetForkSession()
		mapped := newCommand(CommandForkSession, mo.None[string]())
		if fork.HasTargetEntryId() {
			mapped.TargetEntryID = mo.Some(fork.GetTargetEntryId())
		}
		return mapped, true, nil
	case command.GetCloneSession() != nil:
		return newCommand(CommandCloneSession, mo.None[string]()), true, nil
	case command.GetSetEntryLabel() != nil:
		label := command.GetSetEntryLabel()
		mapped := newCommand(CommandSetEntryLabel, mo.None[string]())
		if label.HasTargetEntryId() {
			mapped.TargetEntryID = mo.Some(label.GetTargetEntryId())
		}
		if label.HasLabel() {
			mapped.EntryLabel = mo.Some(label.GetLabel())
		}
		return mapped, true, nil
	case command.GetResumeSession() != nil:
		resume := command.GetResumeSession()
		if !resume.HasSessionId() || resume.GetSessionId() == "" {
			return Command{}, true, errors.New("receive UI command: session ID is required")
		}
		mapped := newCommand(CommandResumeSession, mo.None[string]())
		mapped.SessionID = mo.Some(resume.GetSessionId())
		return mapped, true, nil
	case command.GetSetSessionName() != nil:
		name := command.GetSetSessionName()
		if !name.HasName() {
			return Command{}, true, errors.New("receive UI command: session name is required")
		}
		mapped := newCommand(CommandSetSessionName, mo.None[string]())
		mapped.SessionName = mo.Some(name.GetName())
		return mapped, true, nil
	default:
		return Command{}, false, nil
	}
}

// emptySessionCommand initializes absent arguments for lifecycle commands without payloads.
func newCommand(kind CommandKind, text mo.Option[string]) Command {
	return Command{
		OperationID:     "",
		Kind:            kind,
		Text:            text,
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TargetEntryID:   mo.None[string](),
		SummaryMode:     SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	}
}

// mapSummaryModeFromProto maps all public modes and preserves unknown values for typed rejection.
func mapSummaryModeFromProto(value uiv1.SummaryMode) SummaryMode {
	switch value {
	case uiv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY:
		return SummaryModeNoSummary
	case uiv1.SummaryMode_SUMMARY_MODE_SUMMARIZE:
		return SummaryModeSummarize
	case uiv1.SummaryMode_SUMMARY_MODE_SUMMARIZE_WITH_CUSTOM_PROMPT:
		return SummaryModeSummarizeWithCustomPrompt
	case uiv1.SummaryMode_SUMMARY_MODE_UNSPECIFIED:
		return SummaryMode(^uint8(0))
	default:
		return SummaryMode(^uint8(0))
	}
}

// mapReasoningChoiceFromProto rejects unspecified and unknown public values.
func mapReasoningChoiceFromProto(value uiv1.ReasoningChoice) (model.ReasoningChoice, error) {
	switch value {
	case uiv1.ReasoningChoice_REASONING_CHOICE_OFF:
		return model.ReasoningChoiceOff, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_ON:
		return model.ReasoningChoiceOn, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL:
		return model.ReasoningChoiceMinimal, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_LOW:
		return model.ReasoningChoiceLow, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM:
		return model.ReasoningChoiceMedium, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_HIGH:
		return model.ReasoningChoiceHigh, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH:
		return model.ReasoningChoiceXHigh, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MAX:
		return model.ReasoningChoiceMax, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED:
		return "", errors.New("receive UI command: reasoning choice is unspecified")
	default:
		return "", fmt.Errorf("receive UI command: unknown reasoning choice %d", value)
	}
}
