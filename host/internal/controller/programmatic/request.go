package programmatic

import (
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

var errNilOpenRequest = errors.New("received a nil Programmatic Control request")

// mapOpenRequest validates the operation envelope and maps one non-cancellation request.
func mapOpenRequest(request *programmaticv1.OpenRequest) (Command, error) {
	if request == nil {
		return Command{}, errNilOpenRequest
	}
	operationID := request.GetOperationId()
	if operationID == "" {
		return Command{}, Reject(
			RejectionCodeInvalidArgument,
			errors.New("programmatic operation identifier is required"),
		)
	}
	var payload *programmaticv1.ControllerRequest
	switch request.WhichContent() {
	case programmaticv1.OpenRequest_Request_case:
		payload = request.GetRequest()
	case programmaticv1.OpenRequest_Content_not_set_case:
		return Command{}, Reject(RejectionCodeInvalidArgument, errors.New("programmatic operation request is required"))
	}
	command := Command{
		OperationID:     operationID,
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
	if mapSessionRequest(payload, &command) {
		if !command.Valid() {
			return command, Reject(RejectionCodeInvalidArgument, errors.New("programmatic session request is invalid"))
		}
		return command, nil
	}
	return mapStandardRequest(payload, command)
}

// mapStandardRequest validates non-session protobuf variants and their required fields.
//
//nolint:gocyclo // The switch exhaustively maps the closed Programmatic request union.
func mapStandardRequest(request *programmaticv1.ControllerRequest, command Command) (Command, error) {
	switch request.WhichRequest() {
	case programmaticv1.ControllerRequest_UserRequest_case:
		userRequest := request.GetUserRequest()
		if !userRequest.HasText() || userRequest.GetText() == "" {
			return Command{}, Reject(
				RejectionCodeInvalidArgument,
				errors.New("programmatic user request text is required"),
			)
		}
		command.Kind = CommandUserRequest
		command.UserText = mo.Some(userRequest.GetText())
	case programmaticv1.ControllerRequest_GetRunState_case:
		command.Kind = CommandGetRunState
	case programmaticv1.ControllerRequest_GetMessages_case:
		command.Kind = CommandGetMessages
	case programmaticv1.ControllerRequest_GetModels_case:
		command.Kind = CommandGetModels
	case programmaticv1.ControllerRequest_SelectModel_case:
		selection := request.GetSelectModel()
		if !selection.HasProviderId() || selection.GetProviderId() == "" ||
			!selection.HasModelId() || selection.GetModelId() == "" {
			return Command{}, Reject(
				RejectionCodeInvalidArgument,
				errors.New("programmatic model selection is incomplete"),
			)
		}
		command.Kind = CommandSelectModel
		command.ProviderID = mo.Some(model.ProviderID(selection.GetProviderId()))
		command.ModelID = mo.Some(model.ID(selection.GetModelId()))
	case programmaticv1.ControllerRequest_SelectReasoningChoice_case:
		selection := request.GetSelectReasoningChoice()
		if !selection.HasChoice() ||
			selection.GetChoice() == programmaticv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED {
			return Command{}, Reject(
				RejectionCodeInvalidArgument,
				errors.New("programmatic reasoning choice is required"),
			)
		}
		command.Kind = CommandSelectReasoningChoice
		command.ReasoningChoice = mo.Some(mapRequestReasoningChoice(selection.GetChoice()))
	case programmaticv1.ControllerRequest_Cancel_case:
		return Command{}, errors.New("map Programmatic request: cancellation is controller-owned")
	case programmaticv1.ControllerRequest_CreateSession_case,
		programmaticv1.ControllerRequest_ListSessions_case,
		programmaticv1.ControllerRequest_ResumeSession_case,
		programmaticv1.ControllerRequest_SetSessionName_case,
		programmaticv1.ControllerRequest_GetSessionInfo_case,
		programmaticv1.ControllerRequest_GetSessionEntries_case,
		programmaticv1.ControllerRequest_GetSessionStats_case,
		programmaticv1.ControllerRequest_GetSessionTree_case,
		programmaticv1.ControllerRequest_NavigateSessionTree_case,
		programmaticv1.ControllerRequest_ForkSession_case,
		programmaticv1.ControllerRequest_CloneSession_case,
		programmaticv1.ControllerRequest_SetEntryLabel_case:
		return Command{}, errors.New("map Programmatic request: session request was not mapped")
	case programmaticv1.ControllerRequest_Request_not_set_case:
		return Command{}, Reject(RejectionCodeInvalidArgument, errors.New("programmatic request kind is required"))
	default:
		return Command{}, Reject(RejectionCodeInvalidArgument, errors.New("programmatic request kind is unknown"))
	}
	return command, nil
}

// mapSessionRequest preserves optional lifecycle arguments while mapping the protobuf oneof.
//
//nolint:gocyclo // The switch maps every closed session operation kind explicitly.
func mapSessionRequest(request *programmaticv1.ControllerRequest, command *Command) bool {
	switch request.WhichRequest() {
	case programmaticv1.ControllerRequest_CreateSession_case:
		command.Kind = CommandCreateSession
	case programmaticv1.ControllerRequest_ListSessions_case:
		command.Kind = CommandListSessions
	case programmaticv1.ControllerRequest_ResumeSession_case:
		resume := request.GetResumeSession()
		command.Kind = CommandResumeSession
		if resume.HasSessionId() {
			command.SessionID = mo.Some(session.ID(resume.GetSessionId()))
		}
	case programmaticv1.ControllerRequest_SetSessionName_case:
		name := request.GetSetSessionName()
		command.Kind = CommandSetSessionName
		if name.HasName() {
			command.SessionName = mo.Some(name.GetName())
		}
	case programmaticv1.ControllerRequest_GetSessionInfo_case:
		command.Kind = CommandGetSessionInfo
	case programmaticv1.ControllerRequest_GetSessionEntries_case:
		command.Kind = CommandGetSessionEntries
	case programmaticv1.ControllerRequest_GetSessionStats_case:
		command.Kind = CommandGetSessionStats
	case programmaticv1.ControllerRequest_GetSessionTree_case:
		command.Kind = CommandGetSessionTree
	case programmaticv1.ControllerRequest_NavigateSessionTree_case:
		navigate := request.GetNavigateSessionTree()
		command.Kind = CommandNavigateSessionTree
		if navigate.HasTargetEntryId() {
			command.TargetEntryID = mo.Some(navigate.GetTargetEntryId())
		}
		command.SummaryMode = mapRequestSummaryMode(navigate.GetSummaryMode())
		if navigate.HasCustomFocus() {
			command.CustomFocus = mo.Some(navigate.GetCustomFocus())
		}
	case programmaticv1.ControllerRequest_ForkSession_case:
		fork := request.GetForkSession()
		command.Kind = CommandForkSession
		if fork.HasTargetEntryId() {
			command.TargetEntryID = mo.Some(fork.GetTargetEntryId())
		}
	case programmaticv1.ControllerRequest_CloneSession_case:
		command.Kind = CommandCloneSession
	case programmaticv1.ControllerRequest_SetEntryLabel_case:
		label := request.GetSetEntryLabel()
		command.Kind = CommandSetEntryLabel
		if label.HasTargetEntryId() {
			command.TargetEntryID = mo.Some(label.GetTargetEntryId())
		}
		if label.HasLabel() {
			command.EntryLabel = mo.Some(label.GetLabel())
		}
	case programmaticv1.ControllerRequest_Request_not_set_case,
		programmaticv1.ControllerRequest_UserRequest_case, programmaticv1.ControllerRequest_Cancel_case,
		programmaticv1.ControllerRequest_GetRunState_case, programmaticv1.ControllerRequest_GetMessages_case,
		programmaticv1.ControllerRequest_GetModels_case, programmaticv1.ControllerRequest_SelectModel_case,
		programmaticv1.ControllerRequest_SelectReasoningChoice_case:
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

// mapRequestReasoningChoice maps one public reasoning choice.
func mapRequestReasoningChoice(choice programmaticv1.ReasoningChoice) model.ReasoningChoice {
	switch choice {
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
