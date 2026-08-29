package plugin

import (
	"errors"
	"fmt"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// mapCommand validates and projects one presentation command onto the public stream.
func mapCommand(command presentationdomain.Command) (*uiv1.OpenResponse, error) {
	if response, handled, err := mapSessionCommand(command); handled {
		return response, err
	}
	switch command.Kind {
	case presentationdomain.CommandSubmit:
		text, ok := command.Text.Get()
		if !ok {
			return nil, errors.New("UI submit text is missing")
		}
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active Submit field.
		return uiv1.OpenResponse_builder{
			Submit: uiv1.SubmitCommand_builder{
				Text: new(text),
			}.Build(),
		}.Build(), nil
	case presentationdomain.CommandStop:
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active Stop field.
		return uiv1.OpenResponse_builder{
			Stop: &uiv1.StopCommand{},
		}.Build(), nil
	case presentationdomain.CommandRetryAuthentication:
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active RetryAuthentication field.
		return uiv1.OpenResponse_builder{
			RetryAuthentication: &uiv1.RetryAuthenticationCommand{},
		}.Build(), nil
	case presentationdomain.CommandQuit:
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active Quit field.
		return uiv1.OpenResponse_builder{
			Quit: &uiv1.QuitCommand{},
		}.Build(), nil
	case presentationdomain.CommandSelectModel:
		providerID, providerOK := command.ProviderID.Get()
		modelID, modelOK := command.ModelID.Get()
		if !providerOK || !modelOK {
			return nil, errors.New("UI model selection is missing")
		}
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active SelectModel field.
		return uiv1.OpenResponse_builder{
			SelectModel: uiv1.SelectModelCommand_builder{
				ProviderId: new(providerID),
				ModelId:    new(modelID),
			}.Build(),
		}.Build(), nil
	case presentationdomain.CommandSelectReasoningChoice:
		reasoningChoice, ok := command.ReasoningChoice.Get()
		if !ok {
			return nil, errors.New("UI reasoning choice is missing")
		}
		level := mapReasoningChoiceToProto(reasoningChoice)
		if level == uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED {
			return nil, errors.New("UI reasoning choice is unspecified")
		}
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active SelectReasoningChoice field.
		return uiv1.OpenResponse_builder{
			SelectReasoningChoice: uiv1.SelectReasoningChoiceCommand_builder{
				Choice: new(level),
			}.Build(),
		}.Build(), nil
	case presentationdomain.CommandCreateSession, presentationdomain.CommandListSessions,
		presentationdomain.CommandResumeSession, presentationdomain.CommandSetSessionName,
		presentationdomain.CommandGetSessionInfo:
		return nil, errors.New("UI session command was not mapped")
	case presentationdomain.CommandUnspecified:
		return nil, errors.New("UI command is unspecified")
	default:
		return nil, fmt.Errorf("unknown UI command %d", command.Kind)
	}
}

// mapSessionCommand preserves lifecycle argument presence in the protobuf oneof.
func mapSessionCommand(command presentationdomain.Command) (*uiv1.OpenResponse, bool, error) {
	switch command.Kind {
	case presentationdomain.CommandCreateSession:
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active CreateSession field.
		return uiv1.OpenResponse_builder{CreateSession: &uiv1.CreateSessionCommand{}}.Build(), true, nil
	case presentationdomain.CommandListSessions:
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active ListSessions field.
		return uiv1.OpenResponse_builder{ListSessions: &uiv1.ListSessionsCommand{}}.Build(), true, nil
	case presentationdomain.CommandGetSessionInfo:
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active GetSessionInfo field.
		return uiv1.OpenResponse_builder{GetSessionInfo: &uiv1.GetSessionInfoCommand{}}.Build(), true, nil
	case presentationdomain.CommandResumeSession:
		id, present := command.SessionID.Get()
		if !present || id == "" {
			return nil, true, errors.New("UI session ID is missing")
		}
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active ResumeSession field.
		response := uiv1.OpenResponse_builder{
			ResumeSession: uiv1.ResumeSessionCommand_builder{SessionId: new(id)}.Build(),
		}.Build()
		return response, true, nil
	case presentationdomain.CommandSetSessionName:
		name, present := command.SessionName.Get()
		if !present {
			return nil, true, errors.New("UI session name is missing")
		}
		//nolint:exhaustruct_v5 // uiv1.OpenResponse_builder sets only the active SetSessionName field.
		response := uiv1.OpenResponse_builder{
			SetSessionName: uiv1.SetSessionNameCommand_builder{Name: new(name)}.Build(),
		}.Build()
		return response, true, nil
	case presentationdomain.CommandUnspecified, presentationdomain.CommandSubmit,
		presentationdomain.CommandStop, presentationdomain.CommandRetryAuthentication,
		presentationdomain.CommandQuit, presentationdomain.CommandSelectModel,
		presentationdomain.CommandSelectReasoningChoice:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// mapReasoningChoiceToProto converts one validated presentation reasoning choice.
func mapReasoningChoiceToProto(level presentationdomain.ReasoningChoice) uiv1.ReasoningChoice {
	switch level {
	case presentationdomain.ReasoningChoiceOff:
		return uiv1.ReasoningChoice_REASONING_CHOICE_OFF
	case presentationdomain.ReasoningChoiceOn:
		return uiv1.ReasoningChoice_REASONING_CHOICE_ON
	case presentationdomain.ReasoningChoiceMinimal:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL
	case presentationdomain.ReasoningChoiceLow:
		return uiv1.ReasoningChoice_REASONING_CHOICE_LOW
	case presentationdomain.ReasoningChoiceMedium:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM
	case presentationdomain.ReasoningChoiceHigh:
		return uiv1.ReasoningChoice_REASONING_CHOICE_HIGH
	case presentationdomain.ReasoningChoiceXHigh:
		return uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH
	case presentationdomain.ReasoningChoiceMax:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MAX
	case presentationdomain.ReasoningChoiceUnspecified:
		return uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED
	default:
		return uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED
	}
}
