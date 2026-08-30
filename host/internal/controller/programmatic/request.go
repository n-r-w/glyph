package programmatic

import (
	"errors"

	"github.com/samber/mo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
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
		SessionID:       mo.None[session.ID](),
		SessionName:     mo.None[string](),
		TargetEntryID:   mo.None[string](),
		SummaryMode:     SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	}
	if mapSessionRequest(request, &command) {
		return command, nil
	}
	return mapStandardRequest(request, command)
}

// mapStandardRequest validates non-session protobuf variants and their required fields.
func mapStandardRequest(request *programmaticv1.OpenRequest, command Command) (Command, error) {
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
	case programmaticv1.OpenRequest_CreateSession_case,
		programmaticv1.OpenRequest_ListSessions_case,
		programmaticv1.OpenRequest_ResumeSession_case,
		programmaticv1.OpenRequest_SetSessionName_case,
		programmaticv1.OpenRequest_GetSessionInfo_case,
		programmaticv1.OpenRequest_GetSessionEntries_case,
		programmaticv1.OpenRequest_GetSessionStats_case,
		programmaticv1.OpenRequest_GetSessionTree_case,
		programmaticv1.OpenRequest_NavigateSessionTree_case,
		programmaticv1.OpenRequest_ForkSession_case,
		programmaticv1.OpenRequest_CloneSession_case,
		programmaticv1.OpenRequest_SetEntryLabel_case:
		return Command{}, status.Error(codes.Internal, "session command was not mapped")
	case programmaticv1.OpenRequest_Command_not_set_case:
	}
	return command, nil
}

// mapSessionRequest preserves optional lifecycle arguments while mapping the protobuf oneof.
//
//nolint:gocyclo // The switch maps every closed session command kind explicitly.
func mapSessionRequest(request *programmaticv1.OpenRequest, command *Command) bool {
	switch request.WhichCommand() {
	case programmaticv1.OpenRequest_CreateSession_case:
		command.Kind = CommandCreateSession
	case programmaticv1.OpenRequest_ListSessions_case:
		command.Kind = CommandListSessions
	case programmaticv1.OpenRequest_ResumeSession_case:
		resume := request.GetResumeSession()
		command.Kind = CommandResumeSession
		if resume.HasSessionId() {
			command.SessionID = mo.Some(session.ID(resume.GetSessionId()))
		}
	case programmaticv1.OpenRequest_SetSessionName_case:
		name := request.GetSetSessionName()
		command.Kind = CommandSetSessionName
		if name.HasName() {
			command.SessionName = mo.Some(name.GetName())
		}
	case programmaticv1.OpenRequest_GetSessionInfo_case:
		command.Kind = CommandGetSessionInfo
	case programmaticv1.OpenRequest_GetSessionEntries_case:
		command.Kind = CommandGetSessionEntries
	case programmaticv1.OpenRequest_GetSessionStats_case:
		command.Kind = CommandGetSessionStats
	case programmaticv1.OpenRequest_GetSessionTree_case:
		command.Kind = CommandGetSessionTree
	case programmaticv1.OpenRequest_NavigateSessionTree_case:
		navigate := request.GetNavigateSessionTree()
		command.Kind = CommandNavigateSessionTree
		if navigate.HasTargetEntryId() {
			command.TargetEntryID = mo.Some(navigate.GetTargetEntryId())
		}
		command.SummaryMode = mapRequestSummaryMode(navigate.GetSummaryMode())
		if navigate.HasCustomFocus() {
			command.CustomFocus = mo.Some(navigate.GetCustomFocus())
		}
	case programmaticv1.OpenRequest_ForkSession_case:
		fork := request.GetForkSession()
		command.Kind = CommandForkSession
		if fork.HasTargetEntryId() {
			command.TargetEntryID = mo.Some(fork.GetTargetEntryId())
		}
	case programmaticv1.OpenRequest_CloneSession_case:
		command.Kind = CommandCloneSession
	case programmaticv1.OpenRequest_SetEntryLabel_case:
		label := request.GetSetEntryLabel()
		command.Kind = CommandSetEntryLabel
		if label.HasTargetEntryId() {
			command.TargetEntryID = mo.Some(label.GetTargetEntryId())
		}
		if label.HasLabel() {
			command.EntryLabel = mo.Some(label.GetLabel())
		}
	case programmaticv1.OpenRequest_Command_not_set_case,
		programmaticv1.OpenRequest_UserRequest_case, programmaticv1.OpenRequest_Abort_case,
		programmaticv1.OpenRequest_GetRunState_case, programmaticv1.OpenRequest_GetMessages_case,
		programmaticv1.OpenRequest_GetModels_case, programmaticv1.OpenRequest_SelectModel_case,
		programmaticv1.OpenRequest_SelectReasoningChoice_case:
		return false
	default:
		return false
	}
	return true
}

// mapRequestSummaryMode maps the closed public navigation mode.
func mapRequestSummaryMode(mode programmaticv1.SummaryMode) SummaryMode {
	switch mode {
	case programmaticv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY:
		return SummaryModeNoSummary
	case programmaticv1.SummaryMode_SUMMARY_MODE_SUMMARIZE:
		return SummaryModeSummarize
	case programmaticv1.SummaryMode_SUMMARY_MODE_SUMMARIZE_WITH_CUSTOM_PROMPT:
		return SummaryModeSummarizeWithCustomPrompt
	case programmaticv1.SummaryMode_SUMMARY_MODE_UNSPECIFIED:
		return SummaryMode(^uint8(0))
	default:
		return SummaryMode(^uint8(0))
	}
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
