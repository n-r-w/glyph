package runtime

import (
	"errors"
	"fmt"

	"github.com/samber/mo"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapCommand validates one generated UI command.
func mapCommand(command *uipb.OpenResponse) (domainui.Command, error) {
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
		return domainui.Command{
			Kind:            domainui.CommandSubmit,
			Text:            mo.Some(submit.GetText()),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	case command.GetStop() != nil:
		return domainui.Command{
			Kind:            domainui.CommandStop,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	case command.GetRetryAuthentication() != nil:
		return domainui.Command{
			Kind:            domainui.CommandRetryAuthentication,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	case command.GetQuit() != nil:
		return domainui.Command{
			Kind:            domainui.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	case command.GetSelectModel() != nil, command.GetSelectReasoningChoice() != nil:
		return domainui.Command{}, errors.New("receive UI command: selection command was not mapped")
	case command.GetCreateSession() != nil, command.GetListSessions() != nil,
		command.GetGetSessionInfo() != nil, command.GetResumeSession() != nil,
		command.GetSetSessionName() != nil:
		return domainui.Command{}, errors.New("receive UI command: session command was not mapped")
	default:
		return domainui.Command{}, errors.New("receive UI command: payload is required")
	}
}

// mapSelectionCommand validates model and reasoning payloads before they reach the Host use case.
func mapSelectionCommand(command *uipb.OpenResponse) (domainui.Command, bool, error) {
	switch {
	case command.GetSelectModel() != nil:
		selected := command.GetSelectModel()
		if !selected.HasProviderId() || !selected.HasModelId() ||
			selected.GetProviderId() == "" || selected.GetModelId() == "" {
			return domainui.Command{}, true, errors.New("receive UI command: provider and model are required")
		}
		return domainui.Command{
			Kind:            domainui.CommandSelectModel,
			ProviderID:      mo.Some(selected.GetProviderId()),
			ModelID:         mo.Some(selected.GetModelId()),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, true, nil
	case command.GetSelectReasoningChoice() != nil:
		selected := command.GetSelectReasoningChoice()
		if !selected.HasChoice() {
			return domainui.Command{}, true, errors.New("receive UI command: reasoning choice is required")
		}
		level, err := mapReasoningChoiceFromProto(selected.GetChoice())
		if err != nil {
			return domainui.Command{}, true, err
		}
		return domainui.Command{
			Kind:            domainui.CommandSelectReasoningChoice,
			ReasoningChoice: mo.Some(level),
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, true, nil
	default:
		return domainui.Command{}, false, nil
	}
}

// mapSessionCommand validates lifecycle command arguments at the protobuf boundary.
func mapSessionCommand(command *uipb.OpenResponse) (domainui.Command, bool, error) {
	switch {
	case command.GetCreateSession() != nil:
		return emptySessionCommand(domainui.CommandCreateSession), true, nil
	case command.GetListSessions() != nil:
		return emptySessionCommand(domainui.CommandListSessions), true, nil
	case command.GetGetSessionInfo() != nil:
		return emptySessionCommand(domainui.CommandGetSessionInfo), true, nil
	case command.GetResumeSession() != nil:
		resume := command.GetResumeSession()
		if !resume.HasSessionId() || resume.GetSessionId() == "" {
			return domainui.Command{}, true, errors.New("receive UI command: session ID is required")
		}
		mapped := emptySessionCommand(domainui.CommandResumeSession)
		mapped.SessionID = mo.Some(resume.GetSessionId())
		return mapped, true, nil
	case command.GetSetSessionName() != nil:
		name := command.GetSetSessionName()
		if !name.HasName() {
			return domainui.Command{}, true, errors.New("receive UI command: session name is required")
		}
		mapped := emptySessionCommand(domainui.CommandSetSessionName)
		mapped.SessionName = mo.Some(name.GetName())
		return mapped, true, nil
	default:
		return domainui.Command{}, false, nil
	}
}

// emptySessionCommand initializes absent arguments for lifecycle commands without payloads.
func emptySessionCommand(kind domainui.CommandKind) domainui.Command {
	return domainui.Command{
		Kind:            kind,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	}
}

// mapReasoningChoice converts a Host reasoning choice to the public contract.
func mapReasoningChoice(value domainui.ReasoningChoice) uipb.ReasoningChoice {
	switch value {
	case domainui.ReasoningChoiceOff:
		return uipb.ReasoningChoice_REASONING_CHOICE_OFF
	case domainui.ReasoningChoiceOn:
		return uipb.ReasoningChoice_REASONING_CHOICE_ON
	case domainui.ReasoningChoiceMinimal:
		return uipb.ReasoningChoice_REASONING_CHOICE_MINIMAL
	case domainui.ReasoningChoiceLow:
		return uipb.ReasoningChoice_REASONING_CHOICE_LOW
	case domainui.ReasoningChoiceMedium:
		return uipb.ReasoningChoice_REASONING_CHOICE_MEDIUM
	case domainui.ReasoningChoiceHigh:
		return uipb.ReasoningChoice_REASONING_CHOICE_HIGH
	case domainui.ReasoningChoiceXHigh:
		return uipb.ReasoningChoice_REASONING_CHOICE_XHIGH
	case domainui.ReasoningChoiceMax:
		return uipb.ReasoningChoice_REASONING_CHOICE_MAX
	default:
		return uipb.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED
	}
}

// mapReasoningChoiceFromProto rejects unspecified and unknown public values.
func mapReasoningChoiceFromProto(value uipb.ReasoningChoice) (domainui.ReasoningChoice, error) {
	switch value {
	case uipb.ReasoningChoice_REASONING_CHOICE_OFF:
		return domainui.ReasoningChoiceOff, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_ON:
		return domainui.ReasoningChoiceOn, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_MINIMAL:
		return domainui.ReasoningChoiceMinimal, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_LOW:
		return domainui.ReasoningChoiceLow, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_MEDIUM:
		return domainui.ReasoningChoiceMedium, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_HIGH:
		return domainui.ReasoningChoiceHigh, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_XHIGH:
		return domainui.ReasoningChoiceXHigh, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_MAX:
		return domainui.ReasoningChoiceMax, nil
	case uipb.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED:
		return 0, errors.New("receive UI command: reasoning choice is unspecified")
	default:
		return 0, fmt.Errorf("receive UI command: unknown reasoning choice %d", value)
	}
}

// mapSeverity converts startup severity to the public contract.
func mapSeverity(value domainui.ContentSeverity) uipb.ContentSeverity {
	switch value {
	case domainui.ContentSeverityInformation:
		return uipb.ContentSeverity_CONTENT_SEVERITY_INFORMATION
	case domainui.ContentSeverityError:
		return uipb.ContentSeverity_CONTENT_SEVERITY_ERROR
	case domainui.ContentSeverityWarning:
		return uipb.ContentSeverity_CONTENT_SEVERITY_WARNING
	default:
		return uipb.ContentSeverity_CONTENT_SEVERITY_INFORMATION
	}
}

// mapAvailability converts Host availability to the public contract.
func mapAvailability(value domainui.Availability) uipb.Availability {
	return uipb.Availability(value)
}
