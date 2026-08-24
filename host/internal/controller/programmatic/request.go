package programmatic

import (
	"errors"

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
		CorrelationID: correlationID, Kind: CommandUnspecified, UserText: "",
		ProviderID: "", ModelID: "", ReasoningLevel: "",
	}
	switch request.WhichCommand() {
	case programmaticv1.OpenRequest_UserRequest_case:
		command.Kind = CommandUserRequest
		command.UserText = request.GetUserRequest().GetText()
	case programmaticv1.OpenRequest_Abort_case:
		command.Kind = CommandAbort
	case programmaticv1.OpenRequest_GetRunState_case:
		command.Kind = CommandGetRunState
	case programmaticv1.OpenRequest_GetMessages_case:
		command.Kind = CommandGetMessages
	case programmaticv1.OpenRequest_GetModels_case:
		command.Kind = CommandGetModels
	case programmaticv1.OpenRequest_SelectModel_case:
		command.Kind = CommandSelectModel
		command.ProviderID = model.ProviderID(request.GetSelectModel().GetProviderId())
		command.ModelID = model.ID(request.GetSelectModel().GetModelId())
	case programmaticv1.OpenRequest_SelectReasoningLevel_case:
		command.Kind = CommandSelectReasoningLevel
		command.ReasoningLevel = mapRequestReasoningLevel(request.GetSelectReasoningLevel().GetLevel())
	case programmaticv1.OpenRequest_Command_not_set_case:
	}
	return command, nil
}

func mapRequestReasoningLevel(level programmaticv1.ReasoningLevel) model.ReasoningLevel {
	switch level {
	case programmaticv1.ReasoningLevel_REASONING_LEVEL_NONE:
		return model.ReasoningLevelNone
	case programmaticv1.ReasoningLevel_REASONING_LEVEL_MINIMAL:
		return model.ReasoningLevelMinimal
	case programmaticv1.ReasoningLevel_REASONING_LEVEL_LOW:
		return model.ReasoningLevelLow
	case programmaticv1.ReasoningLevel_REASONING_LEVEL_MEDIUM:
		return model.ReasoningLevelMedium
	case programmaticv1.ReasoningLevel_REASONING_LEVEL_HIGH:
		return model.ReasoningLevelHigh
	case programmaticv1.ReasoningLevel_REASONING_LEVEL_XHIGH:
		return model.ReasoningLevelXHigh
	case programmaticv1.ReasoningLevel_REASONING_LEVEL_MAX:
		return model.ReasoningLevelMax
	case programmaticv1.ReasoningLevel_REASONING_LEVEL_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}
