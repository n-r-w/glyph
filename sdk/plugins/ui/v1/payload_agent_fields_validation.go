package uiv1

import (
	"errors"
	"fmt"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// agentEventFields identifies scalar and nested fields present in one lifecycle payload.
type agentEventFields uint16

const (
	// agentEventFieldType identifies the lifecycle type field.
	agentEventFieldType agentEventFields = 1 << iota
	// agentEventFieldRunID identifies the run ID field.
	agentEventFieldRunID
	// agentEventFieldText identifies the text field.
	agentEventFieldText
	// agentEventFieldToolCallID identifies the tool call ID field.
	agentEventFieldToolCallID
	// agentEventFieldToolName identifies the tool name field.
	agentEventFieldToolName
	// agentEventFieldProgressChannel identifies the progress channel field.
	agentEventFieldProgressChannel
	// agentEventFieldIsError identifies the error-state field.
	agentEventFieldIsError
	// agentEventFieldOutcome identifies the terminal outcome field.
	agentEventFieldOutcome
	// agentEventFieldErrorMessage identifies the error message field.
	agentEventFieldErrorMessage
	// agentEventFieldAvailability identifies the availability field.
	agentEventFieldAvailability
	// agentEventFieldModelContent identifies the incremental model content field.
	agentEventFieldModelContent
	// agentEventFieldModelResponse identifies the final model response field.
	agentEventFieldModelResponse
	// agentEventFieldToolCallPreview identifies the tool call preview field.
	agentEventFieldToolCallPreview
	// agentEventFieldFinalToolCall identifies the final tool call field.
	agentEventFieldFinalToolCall
	// agentEventFieldToolResultContents identifies the tool result content field.
	agentEventFieldToolResultContents
)

// validateAgentEventFields rejects fields that are inactive for the lifecycle type.
func validateAgentEventFields(event *uiv1.AgentEvent) error {
	allowed, err := allowedAgentEventFields(event.GetType())
	if err != nil {
		return err
	}
	if inactive := presentAgentEventFields(event) &^ allowed; inactive != 0 {
		return fmt.Errorf("Host agent event type %d has inactive fields 0x%x", event.GetType(), inactive)
	}
	return nil
}

// allowedAgentEventFields returns the active fields for one lifecycle type.
func allowedAgentEventFields(lifecycleType uiv1.LifecycleType) (agentEventFields, error) {
	base := agentEventFieldType | agentEventFieldRunID
	switch lifecycleType {
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START:
		return base, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END:
		return base | agentEventFieldModelContent, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
		return base | agentEventFieldModelResponse, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA:
		return base | agentEventFieldToolCallPreview, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END:
		return base | agentEventFieldFinalToolCall, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START:
		return base | agentEventFieldToolCallID | agentEventFieldToolName |
			agentEventFieldText | agentEventFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE:
		return base | agentEventFieldToolCallID | agentEventFieldToolName | agentEventFieldText |
			agentEventFieldProgressChannel | agentEventFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END:
		return base | agentEventFieldToolCallID | agentEventFieldToolName | agentEventFieldText |
			agentEventFieldIsError | agentEventFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
		return base | agentEventFieldToolCallID | agentEventFieldToolName | agentEventFieldText |
			agentEventFieldIsError | agentEventFieldErrorMessage | agentEventFieldToolResultContents, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END:
		return base | agentEventFieldText | agentEventFieldIsError |
			agentEventFieldOutcome | agentEventFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END:
		return base | agentEventFieldIsError | agentEventFieldOutcome | agentEventFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED:
		return 0, errors.New("Host agent event type is required")
	default:
		return 0, fmt.Errorf("Host agent event type %d is unknown", lifecycleType)
	}
}

// presentAgentEventFields returns all populated lifecycle fields.
func presentAgentEventFields(event *uiv1.AgentEvent) agentEventFields {
	return presentAgentEventScalarFields(event) | presentAgentEventPayloadFields(event)
}

// presentAgentEventScalarFields returns populated scalar lifecycle fields.
func presentAgentEventScalarFields(event *uiv1.AgentEvent) agentEventFields {
	var fields agentEventFields
	if event.HasType() {
		fields |= agentEventFieldType
	}
	if event.HasRunId() {
		fields |= agentEventFieldRunID
	}
	if event.HasText() {
		fields |= agentEventFieldText
	}
	if event.HasToolCallId() {
		fields |= agentEventFieldToolCallID
	}
	if event.HasToolName() {
		fields |= agentEventFieldToolName
	}
	if event.HasProgressChannel() {
		fields |= agentEventFieldProgressChannel
	}
	if event.HasIsError() {
		fields |= agentEventFieldIsError
	}
	if event.HasOutcome() {
		fields |= agentEventFieldOutcome
	}
	if event.HasErrorMessage() {
		fields |= agentEventFieldErrorMessage
	}
	if event.HasAvailability() {
		fields |= agentEventFieldAvailability
	}
	return fields
}

// presentAgentEventPayloadFields returns populated nested lifecycle fields.
func presentAgentEventPayloadFields(event *uiv1.AgentEvent) agentEventFields {
	var fields agentEventFields
	if event.HasModelContent() {
		fields |= agentEventFieldModelContent
	}
	if event.HasModelResponse() {
		fields |= agentEventFieldModelResponse
	}
	if event.HasToolCallPreview() {
		fields |= agentEventFieldToolCallPreview
	}
	if event.HasFinalToolCall() {
		fields |= agentEventFieldFinalToolCall
	}
	if len(event.GetToolResultContents()) > 0 {
		fields |= agentEventFieldToolResultContents
	}
	return fields
}
