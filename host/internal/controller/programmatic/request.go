package programmatic

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

	command := Command{CorrelationID: correlationID, Kind: CommandUnspecified, UserText: ""}
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
	case programmaticv1.OpenRequest_Command_not_set_case:
	}
	return command, nil
}
