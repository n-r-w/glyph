package uiv1

import (
	"errors"
	"fmt"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// validateHostProgressFields validates required fields inside one correlated progress payload.
func validateHostProgressFields(progress *uiv1.HostProgress) error {
	if progress == nil {
		return errors.New("Host progress payload is required")
	}
	if authorization := progress.GetAuthorization(); authorization != nil {
		if !authorization.HasUrl() || authorization.GetUrl() == "" {
			return errors.New("Host authorization URL is required")
		}
		return nil
	}
	event := progress.GetAgentEvent()
	if event == nil {
		return errors.New("Host agent event is required")
	}
	if !event.HasType() || event.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED {
		return errors.New("Host agent event type is required")
	}
	if !event.HasRunId() || event.GetRunId() == "" {
		return errors.New("Host agent event run ID is required")
	}
	if err := validateAgentEventFields(event); err != nil {
		return err
	}
	return validateAgentEventTypeFields(event)
}

// validateAgentEventTypeFields validates fields required by one lifecycle type.
func validateAgentEventTypeFields(event *uiv1.AgentEvent) error {
	switch event.GetType() {
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START:
		return nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
		return validateModelAgentEvent(event)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
		return validateToolAgentEvent(event)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END:
		return validateTerminalAgentEvent(event)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED:
		return errors.New("Host agent event type is required")
	default:
		return errors.New("Host agent event type is unknown")
	}
}

// validateModelAgentEvent validates model lifecycle nested payloads.
func validateModelAgentEvent(event *uiv1.AgentEvent) error {
	if event.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END {
		return validateModelResponse(event.GetModelResponse())
	}
	content := event.GetModelContent()
	if content == nil || !content.HasType() || !content.HasPosition() || !content.HasKind() {
		return errors.New("Host model content fields are required")
	}
	if content.GetType() == uiv1.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED {
		return errors.New("Host model content type is required")
	}
	expectedType, err := expectedModelContentType(event.GetType())
	if err != nil {
		return err
	}
	if content.GetType() != expectedType {
		return fmt.Errorf(
			"Host model content type %d does not match lifecycle type %d",
			content.GetType(),
			event.GetType(),
		)
	}
	if validationErr := validateModelContentKind(content.GetKind()); validationErr != nil {
		return validationErr
	}
	if event.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA && !content.HasText() {
		return errors.New("Host model text delta is required")
	}
	return nil
}

// validateToolAgentEvent validates tool lifecycle nested payloads.
func validateToolAgentEvent(event *uiv1.AgentEvent) error {
	switch event.GetType() {
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END:
		return validateToolCallAgentEvent(event)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
		return validateToolExecutionAgentEvent(event)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START, uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START, uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END, uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END:
		return errors.New("Host tool event type is required")
	default:
		return errors.New("Host tool event type is unknown")
	}
}

// validateToolCallAgentEvent validates tool-call preview and final payloads.
func validateToolCallAgentEvent(event *uiv1.AgentEvent) error {
	if event.GetType() != uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END {
		return validateToolCallPreview(event.GetToolCallPreview())
	}
	return validateFinalToolCall(event.GetFinalToolCall())
}

// validateToolExecutionAgentEvent validates tool execution identity and result payloads.
func validateToolExecutionAgentEvent(event *uiv1.AgentEvent) error {
	switch int(event.GetType()) {
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START):
		return validateToolExecutionStart(event)
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE):
		return validateToolExecutionUpdate(event)
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END):
		return validateToolExecutionResultFields(event)
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT):
		if err := validateToolExecutionResultFields(event); err != nil {
			return err
		}
		return validateToolResultContents(event.GetToolResultContents(), false)
	default:
		if event.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED {
			return errors.New("Host tool execution type is required")
		}
		return fmt.Errorf("Host lifecycle type %d is not a tool execution event", event.GetType())
	}
}

// validateToolExecutionStart validates the started tool identity.
func validateToolExecutionStart(event *uiv1.AgentEvent) error {
	if !event.HasToolCallId() || !event.HasToolName() {
		return errors.New("Host started tool identity is required")
	}
	return nil
}

// validateToolExecutionUpdate validates one streamed tool progress payload.
func validateToolExecutionUpdate(event *uiv1.AgentEvent) error {
	if !event.HasText() || !event.HasProgressChannel() {
		return errors.New("Host tool progress fields are required")
	}
	return validateProgressChannel(event.GetProgressChannel())
}

// validateToolExecutionResultFields validates terminal tool identity and status.
func validateToolExecutionResultFields(event *uiv1.AgentEvent) error {
	if !event.HasToolCallId() || !event.HasToolName() || !event.HasIsError() {
		return errors.New("Host tool result fields are required")
	}
	return nil
}

// validateTerminalAgentEvent validates terminal lifecycle nested payloads.
func validateTerminalAgentEvent(event *uiv1.AgentEvent) error {
	if event.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END && !event.HasText() {
		return errors.New("Host turn summary is required")
	}
	if event.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END && !event.HasOutcome() {
		return errors.New("Host agent outcome is required")
	}
	return nil
}

// validateHostCompletedFields validates required fields inside one correlated completion payload.
func validateHostCompletedFields(completed *uiv1.HostCompleted) error {
	if completed == nil {
		return errors.New("Host completed payload is required")
	}
	switch completed.WhichCompleted() {
	case uiv1.HostCompleted_Cancel_case:
		cancel := completed.GetCancel()
		if cancel == nil || !cancel.HasTargetState() {
			return errors.New("Host cancellation target state is required")
		}
		switch cancel.GetTargetState() {
		case operationv1.TerminalState_TERMINAL_STATE_COMPLETED,
			operationv1.TerminalState_TERMINAL_STATE_CANCELED,
			operationv1.TerminalState_TERMINAL_STATE_FAILED:
		case operationv1.TerminalState_TERMINAL_STATE_UNSPECIFIED:
			return errors.New("Host cancellation target state is required")
		default:
			return fmt.Errorf("Host cancellation target state %d is unknown", cancel.GetTargetState())
		}
	case uiv1.HostCompleted_Submit_case:
		if completed.GetSubmit() == nil {
			return errors.New("Host submit acknowledgement is required")
		}
	case uiv1.HostCompleted_Authentication_case:
		if completed.GetAuthentication() == nil {
			return errors.New("Host authentication acknowledgement is required")
		}
	case uiv1.HostCompleted_ModelSelection_case:
		changed := completed.GetModelSelection()
		if changed == nil {
			return errors.New("Host model selection result is required")
		}
		return validateModelSelection(changed.GetSelection())
	case uiv1.HostCompleted_SessionChanged_case, uiv1.HostCompleted_SessionList_case,
		uiv1.HostCompleted_SessionInformation_case, uiv1.HostCompleted_SessionTree_case,
		uiv1.HostCompleted_SessionTreeNavigation_case, uiv1.HostCompleted_SessionForked_case,
		uiv1.HostCompleted_SessionCloned_case, uiv1.HostCompleted_EntryLabelSet_case:
		return validateHostSessionCompletedFields(completed)
	case uiv1.HostCompleted_Completed_not_set_case:
		return errors.New("Host completed payload kind is required")
	default:
		return errors.New("Host completed payload kind is unknown")
	}
	return nil
}

// validateHostSessionCompletedFields validates session completion variants.
func validateHostSessionCompletedFields(completed *uiv1.HostCompleted) error {
	switch completed.WhichCompleted() {
	case uiv1.HostCompleted_SessionChanged_case:
		return validateSessionChanged(completed.GetSessionChanged())
	case uiv1.HostCompleted_SessionList_case:
		listed := completed.GetSessionList()
		if listed == nil {
			return errors.New("Host session list is required")
		}
		for index, summary := range listed.GetSessions() {
			if err := validateSessionSummary(summary); err != nil {
				return fmt.Errorf("Host session list item %d: %w", index, err)
			}
		}
	case uiv1.HostCompleted_SessionInformation_case:
		information := completed.GetSessionInformation()
		if information == nil || information.GetInfo() == nil || information.GetStatistics() == nil {
			return errors.New("Host session information and statistics are required")
		}
		if err := validateSessionInfo(information.GetInfo()); err != nil {
			return err
		}
		return validateSessionStatistics(information.GetStatistics())
	case uiv1.HostCompleted_SessionTree_case, uiv1.HostCompleted_SessionTreeNavigation_case,
		uiv1.HostCompleted_SessionForked_case, uiv1.HostCompleted_SessionCloned_case,
		uiv1.HostCompleted_EntryLabelSet_case:
		return validateHostTreeCompletedFields(completed)
	case uiv1.HostCompleted_Cancel_case, uiv1.HostCompleted_Submit_case,
		uiv1.HostCompleted_Authentication_case, uiv1.HostCompleted_ModelSelection_case,
		uiv1.HostCompleted_Completed_not_set_case:
		return errors.New("Host session completed payload kind is required")
	default:
		return errors.New("Host session completed payload kind is unknown")
	}
	return nil
}

// validateHostTreeCompletedFields validates tree and replacement completion variants.
func validateHostTreeCompletedFields(completed *uiv1.HostCompleted) error {
	switch completed.WhichCompleted() {
	case uiv1.HostCompleted_SessionTree_case:
		result := completed.GetSessionTree()
		if result == nil {
			return errors.New("Host session tree result is required")
		}
		return validateSessionTree(result.GetTree())
	case uiv1.HostCompleted_SessionTreeNavigation_case:
		return validateNavigation(completed.GetSessionTreeNavigation())
	case uiv1.HostCompleted_SessionForked_case:
		forked := completed.GetSessionForked()
		if forked == nil || forked.GetSession() == nil || !forked.HasNextInput() {
			return errors.New("Host forked session and next input are required")
		}
		return validateSessionChanged(forked.GetSession())
	case uiv1.HostCompleted_SessionCloned_case:
		cloned := completed.GetSessionCloned()
		if cloned == nil {
			return errors.New("Host cloned session result is required")
		}
		return validateSessionChanged(cloned.GetSession())
	case uiv1.HostCompleted_EntryLabelSet_case:
		result := completed.GetEntryLabelSet()
		if result == nil {
			return errors.New("Host entry label result is required")
		}
		return validateSessionTree(result.GetTree())
	case uiv1.HostCompleted_Cancel_case, uiv1.HostCompleted_Submit_case,
		uiv1.HostCompleted_Authentication_case, uiv1.HostCompleted_ModelSelection_case,
		uiv1.HostCompleted_SessionChanged_case, uiv1.HostCompleted_SessionList_case,
		uiv1.HostCompleted_SessionInformation_case, uiv1.HostCompleted_Completed_not_set_case:
		return errors.New("Host tree completed payload kind is required")
	default:
		return errors.New("Host tree completed payload kind is unknown")
	}
}

// validateModelSelection validates one committed model selection.
func validateModelSelection(selection *uiv1.ModelSelection) error {
	if selection == nil || !selection.HasProviderId() || selection.GetProviderId() == "" ||
		!selection.HasModelId() || selection.GetModelId() == "" || !selection.HasReasoningChoice() {
		return errors.New("Host model selection fields are required")
	}
	switch selection.GetReasoningChoice() {
	case uiv1.ReasoningChoice_REASONING_CHOICE_OFF,
		uiv1.ReasoningChoice_REASONING_CHOICE_ON,
		uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL,
		uiv1.ReasoningChoice_REASONING_CHOICE_LOW,
		uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM,
		uiv1.ReasoningChoice_REASONING_CHOICE_HIGH,
		uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH,
		uiv1.ReasoningChoice_REASONING_CHOICE_MAX:
		return nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED:
		return errors.New("Host model selection reasoning choice is required")
	default:
		return fmt.Errorf("Host model selection reasoning choice %d is unknown", selection.GetReasoningChoice())
	}
}

// validateSessionInfo validates one required session identity.
func validateSessionInfo(info *uiv1.SessionInfo) error {
	if info == nil || !info.HasId() || info.GetId() == "" ||
		!info.HasWorkingDirectory() || info.GetWorkingDirectory() == "" ||
		info.GetCreatedTime() == nil || info.GetUpdateTime() == nil {
		return errors.New("Host session identity fields are required")
	}
	if err := info.GetCreatedTime().CheckValid(); err != nil {
		return fmt.Errorf("Host session creation time: %w", err)
	}
	if err := info.GetUpdateTime().CheckValid(); err != nil {
		return fmt.Errorf("Host session update time: %w", err)
	}
	return nil
}

// validateSessionChanged validates one replacement session payload.
func validateSessionChanged(changed *uiv1.SessionChanged) error {
	if changed == nil || changed.GetInfo() == nil {
		return errors.New("Host changed session information is required")
	}
	if err := validateSessionInfo(changed.GetInfo()); err != nil {
		return err
	}
	for index, entry := range changed.GetEntries() {
		if err := validateSessionEntry(entry); err != nil {
			return fmt.Errorf("Host session entry %d: %w", index, err)
		}
	}
	return nil
}

// validateSessionTree validates one committed session tree.
func validateSessionTree(tree *uiv1.SessionTree) error {
	if tree == nil {
		return errors.New("Host session tree is required")
	}
	for index, entry := range tree.GetEntries() {
		if err := validateSessionTreeEntry(entry); err != nil {
			return fmt.Errorf("Host session tree entry %d: %w", index, err)
		}
	}
	return nil
}

// validateNavigation validates committed and canceled navigation result invariants.
func validateNavigation(result *uiv1.SessionTreeNavigationResult) error {
	if result == nil || !result.HasStatus() ||
		result.GetStatus() == uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_UNSPECIFIED {
		return errors.New("Host navigation status is required")
	}
	switch result.GetStatus() {
	case uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED:
		if err := validateSessionTree(result.GetTree()); err != nil {
			return err
		}
		for index, entry := range result.GetActiveBranch() {
			if err := validateSessionEntry(entry); err != nil {
				return fmt.Errorf("Host navigation active branch entry %d: %w", index, err)
			}
		}
	case uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED:
	case uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_UNSPECIFIED:
		return errors.New("Host navigation status is required")
	default:
		return fmt.Errorf("Host navigation status %d is unknown", result.GetStatus())
	}
	for index, issue := range result.GetIssues() {
		if issue == nil || !issue.HasCode() ||
			issue.GetCode() == uiv1.OperationIssueCode_OPERATION_ISSUE_CODE_UNSPECIFIED ||
			!issue.HasMessage() {
			return fmt.Errorf("Host navigation issue %d fields are required", index)
		}
	}
	return nil
}
