package programmatic

import (
	"errors"

	"github.com/samber/mo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

var errNilOpenRequest = errors.New("received a nil Programmatic Control request")

func mapOpenRequest(request *programmaticv1.OpenRequest) (Command, error) {
	if request == nil {
		return Command{}, errNilOpenRequest
	}
	correlationID := request.GetCorrelationId()
	if correlationID == "" {
		return Command{}, status.Error(codes.InvalidArgument, "correlation ID is required")
	}

	command := Command{
		CorrelationID:   correlationID,
		Kind:            CommandUnspecified,
		UserText:        mo.None[string](),
		ProviderID:      mo.None[model.ProviderID](),
		ModelID:         mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
	}
	switch request.WhichCommand() {
	case programmaticv1.OpenRequest_UserRequest_case:
		userRequest := request.GetUserRequest()
		if !userRequest.HasText() {
			return Command{}, status.Error(codes.InvalidArgument, "user request text is required")
		}
		command.Kind = CommandUserRequest
		command.UserText = mo.Some(userRequest.GetText())
	case programmaticv1.OpenRequest_Abort_case:
		command.Kind = CommandAbort
	case programmaticv1.OpenRequest_GetRunState_case:
		command.Kind = CommandGetRunState
	case programmaticv1.OpenRequest_GetMessages_case:
		command.Kind = CommandGetMessages
	case programmaticv1.OpenRequest_GetModels_case:
		command.Kind = CommandGetModels
	case programmaticv1.OpenRequest_SelectModel_case:
		selection := request.GetSelectModel()
		if !selection.HasProviderId() || !selection.HasModelId() {
			return Command{}, status.Error(codes.InvalidArgument, "provider ID and model ID are required")
		}
		command.Kind = CommandSelectModel
		command.ProviderID = mo.Some(model.ProviderID(selection.GetProviderId()))
		command.ModelID = mo.Some(model.ID(selection.GetModelId()))
	case programmaticv1.OpenRequest_SelectReasoningChoice_case:
		selection := request.GetSelectReasoningChoice()
		if !selection.HasChoice() {
			return Command{}, status.Error(codes.InvalidArgument, "reasoning choice is required")
		}
		command.Kind = CommandSelectReasoningChoice
		command.ReasoningChoice = mo.Some(mapRequestReasoningChoice(selection.GetChoice()))
	case programmaticv1.OpenRequest_Command_not_set_case:
	}
	return command, nil
}

func mapRequestReasoningChoice(level programmaticv1.ReasoningChoice) model.ReasoningChoice {
	switch level {
	case programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF:
		return model.ReasoningChoiceOff
	case programmaticv1.ReasoningChoice_REASONING_CHOICE_ON:
		return model.ReasoningChoiceOn
	case programmaticv1.ReasoningChoice_REASONING_CHOICE_MINIMAL:
		return model.ReasoningChoiceMinimal
	case programmaticv1.ReasoningChoice_REASONING_CHOICE_LOW:
		return model.ReasoningChoiceLow
	case programmaticv1.ReasoningChoice_REASONING_CHOICE_MEDIUM:
		return model.ReasoningChoiceMedium
	case programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH:
		return model.ReasoningChoiceHigh
	case programmaticv1.ReasoningChoice_REASONING_CHOICE_XHIGH:
		return model.ReasoningChoiceXHigh
	case programmaticv1.ReasoningChoice_REASONING_CHOICE_MAX:
		return model.ReasoningChoiceMax
	case programmaticv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}
