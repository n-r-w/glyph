package runtime

import (
	"errors"
	"fmt"

	"github.com/samber/mo"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapCommand validates one generated UI command.
func mapCommand(response *uiv1.OpenResponse) (domainui.Command, error) {
	if response == nil || response.GetRequest() == nil {
		return domainui.Command{}, errors.New("receive UI command: operation request is required")
	}
	command, err := mapUIRequest(response.GetRequest())
	command.OperationID = response.GetOperationId()
	return command, err
}

// mapUIRequest validates one typed UI operation request.
func mapUIRequest(command *uiv1.UIRequest) (domainui.Command, error) {
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
			return domainui.Command{}, errors.New("receive UI command: submit text is required")
		}
		return newCommand(domainui.CommandSubmit, mo.Some(submit.GetText())), nil
	case command.GetRetryAuthentication() != nil:
		return newCommand(domainui.CommandRetryAuthentication, mo.None[string]()), nil
	case command.GetSelectModel() != nil, command.GetSelectReasoningChoice() != nil:
		return domainui.Command{}, errors.New("receive UI command: selection command was not mapped")
	case command.GetCreateSession() != nil, command.GetListSessions() != nil,
		command.GetGetSessionInfo() != nil, command.GetResumeSession() != nil,
		command.GetSetSessionName() != nil, command.GetGetSessionTree() != nil,
		command.GetNavigateSessionTree() != nil, command.GetForkSession() != nil,
		command.GetCloneSession() != nil, command.GetSetEntryLabel() != nil:
		return domainui.Command{}, errors.New("receive UI command: session command was not mapped")
	default:
		return domainui.Command{}, errors.New("receive UI command: payload is required")
	}
}

// mapSelectionCommand validates model and reasoning payloads before they reach the Host use case.
func mapSelectionCommand(command *uiv1.UIRequest) (domainui.Command, bool, error) {
	switch {
	case command.GetSelectModel() != nil:
		selected := command.GetSelectModel()
		if !selected.HasProviderId() || !selected.HasModelId() ||
			selected.GetProviderId() == "" || selected.GetModelId() == "" {
			return domainui.Command{}, true, errors.New("receive UI command: provider and model are required")
		}
		return domainui.Command{
			OperationID:     "",
			Kind:            domainui.CommandSelectModel,
			ProviderID:      mo.Some(selected.GetProviderId()),
			ModelID:         mo.Some(selected.GetModelId()),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     domainui.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
		}, true, nil
	case command.GetSelectReasoningChoice() != nil:
		selected := command.GetSelectReasoningChoice()
		if !selected.HasChoice() {
			return domainui.Command{}, true, errors.New("receive UI command: reasoning choice is required")
		}
		choice, err := mapReasoningChoiceFromProto(selected.GetChoice())
		if err != nil {
			return domainui.Command{}, true, err
		}
		return domainui.Command{
			OperationID:     "",
			Kind:            domainui.CommandSelectReasoningChoice,
			ReasoningChoice: mo.Some(choice),
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     domainui.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
		}, true, nil
	default:
		return domainui.Command{}, false, nil
	}
}

// mapSessionCommand validates lifecycle command arguments at the protobuf boundary.
//
//nolint:gocyclo // The switch maps every closed session command kind explicitly.
func mapSessionCommand(command *uiv1.UIRequest) (domainui.Command, bool, error) {
	switch {
	case command.GetCreateSession() != nil:
		return newCommand(domainui.CommandCreateSession, mo.None[string]()), true, nil
	case command.GetListSessions() != nil:
		return newCommand(domainui.CommandListSessions, mo.None[string]()), true, nil
	case command.GetGetSessionInfo() != nil:
		return newCommand(domainui.CommandGetSessionInfo, mo.None[string]()), true, nil
	case command.GetGetSessionTree() != nil:
		return newCommand(domainui.CommandGetSessionTree, mo.None[string]()), true, nil
	case command.GetNavigateSessionTree() != nil:
		navigate := command.GetNavigateSessionTree()
		mapped := newCommand(domainui.CommandNavigateSessionTree, mo.None[string]())
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
		mapped := newCommand(domainui.CommandForkSession, mo.None[string]())
		if fork.HasTargetEntryId() {
			mapped.TargetEntryID = mo.Some(fork.GetTargetEntryId())
		}
		return mapped, true, nil
	case command.GetCloneSession() != nil:
		return newCommand(domainui.CommandCloneSession, mo.None[string]()), true, nil
	case command.GetSetEntryLabel() != nil:
		label := command.GetSetEntryLabel()
		mapped := newCommand(domainui.CommandSetEntryLabel, mo.None[string]())
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
			return domainui.Command{}, true, errors.New("receive UI command: session ID is required")
		}
		mapped := newCommand(domainui.CommandResumeSession, mo.None[string]())
		mapped.SessionID = mo.Some(resume.GetSessionId())
		return mapped, true, nil
	case command.GetSetSessionName() != nil:
		name := command.GetSetSessionName()
		if !name.HasName() {
			return domainui.Command{}, true, errors.New("receive UI command: session name is required")
		}
		mapped := newCommand(domainui.CommandSetSessionName, mo.None[string]())
		mapped.SessionName = mo.Some(name.GetName())
		return mapped, true, nil
	default:
		return domainui.Command{}, false, nil
	}
}

// emptySessionCommand initializes absent arguments for lifecycle commands without payloads.
func newCommand(kind domainui.CommandKind, text mo.Option[string]) domainui.Command {
	return domainui.Command{
		OperationID:     "",
		Kind:            kind,
		Text:            text,
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TargetEntryID:   mo.None[string](),
		SummaryMode:     domainui.SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	}
}

// mapSummaryModeFromProto maps all public modes and preserves unknown values for typed rejection.
func mapSummaryModeFromProto(value uiv1.SummaryMode) domainui.SummaryMode {
	switch value {
	case uiv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY:
		return domainui.SummaryModeNoSummary
	case uiv1.SummaryMode_SUMMARY_MODE_SUMMARIZE:
		return domainui.SummaryModeSummarize
	case uiv1.SummaryMode_SUMMARY_MODE_SUMMARIZE_WITH_CUSTOM_PROMPT:
		return domainui.SummaryModeSummarizeWithCustomPrompt
	case uiv1.SummaryMode_SUMMARY_MODE_UNSPECIFIED:
		return domainui.SummaryMode(^uint8(0))
	default:
		return domainui.SummaryMode(^uint8(0))
	}
}

// mapReasoningChoice converts a Host reasoning choice to the public contract.
func mapReasoningChoice(value domainui.ReasoningChoice) uiv1.ReasoningChoice {
	switch value {
	case domainui.ReasoningChoiceOff:
		return uiv1.ReasoningChoice_REASONING_CHOICE_OFF
	case domainui.ReasoningChoiceOn:
		return uiv1.ReasoningChoice_REASONING_CHOICE_ON
	case domainui.ReasoningChoiceMinimal:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL
	case domainui.ReasoningChoiceLow:
		return uiv1.ReasoningChoice_REASONING_CHOICE_LOW
	case domainui.ReasoningChoiceMedium:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM
	case domainui.ReasoningChoiceHigh:
		return uiv1.ReasoningChoice_REASONING_CHOICE_HIGH
	case domainui.ReasoningChoiceXHigh:
		return uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH
	case domainui.ReasoningChoiceMax:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MAX
	default:
		return uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED
	}
}

// mapReasoningChoiceFromProto rejects unspecified and unknown public values.
func mapReasoningChoiceFromProto(value uiv1.ReasoningChoice) (domainui.ReasoningChoice, error) {
	switch value {
	case uiv1.ReasoningChoice_REASONING_CHOICE_OFF:
		return domainui.ReasoningChoiceOff, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_ON:
		return domainui.ReasoningChoiceOn, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL:
		return domainui.ReasoningChoiceMinimal, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_LOW:
		return domainui.ReasoningChoiceLow, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM:
		return domainui.ReasoningChoiceMedium, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_HIGH:
		return domainui.ReasoningChoiceHigh, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH:
		return domainui.ReasoningChoiceXHigh, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MAX:
		return domainui.ReasoningChoiceMax, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED:
		return 0, errors.New("receive UI command: reasoning choice is unspecified")
	default:
		return 0, fmt.Errorf("receive UI command: unknown reasoning choice %d", value)
	}
}

// mapSeverity converts startup severity to the public contract.
func mapSeverity(value domainui.ContentSeverity) uiv1.ContentSeverity {
	switch value {
	case domainui.ContentSeverityInformation:
		return uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION
	case domainui.ContentSeverityError:
		return uiv1.ContentSeverity_CONTENT_SEVERITY_ERROR
	case domainui.ContentSeverityWarning:
		return uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING
	default:
		return uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION
	}
}

// mapAvailability converts Host availability to the public contract.
func mapAvailability(value domainui.Availability) uiv1.Availability {
	return uiv1.Availability(value)
}
