package plugin

import (
	"errors"
	"fmt"

	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// DecodeLifecycle validates an SDK lifecycle envelope and returns only its active model or tool input.
func DecodeLifecycle(lifecycle *uiv1.AgentEvent) (AgentUpdate, error) {
	if lifecycle == nil {
		return AgentUpdate{}, errors.New("lifecycle event is nil")
	}
	if err := validateLifecycleEnvelope(lifecycle); err != nil {
		return AgentUpdate{}, err
	}
	event := AgentUpdate{
		Kind:                 AgentUnspecified,
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[ToolCallState](),
	}

	var err error
	switch lifecycle.GetType() {
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START:
		event.Kind = AgentTurnStarted
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START:
		event.Kind = AgentModelDelta
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
		err = mapModelLifecycle(&event, lifecycle)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END:
		err = mapToolCallLifecycle(&event, lifecycle)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
		err = mapToolLifecycle(&event, lifecycle)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END:
		err = mapTerminalLifecycle(&event, lifecycle)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED:
		return AgentUpdate{}, errors.New("lifecycle type is unspecified")
	default:
		return AgentUpdate{}, fmt.Errorf("unknown lifecycle type %d", lifecycle.GetType())
	}
	if err != nil {
		return AgentUpdate{}, err
	}
	return event, nil
}

// lifecycleFields is a presence mask for optional AgentEvent payload fields.
type lifecycleFields uint16

const (
	// lifecycleFieldType records presence of the lifecycle discriminator.
	lifecycleFieldType lifecycleFields = 1 << iota
	// lifecycleFieldRunID records presence of the run identifier.
	lifecycleFieldRunID
	// lifecycleFieldText records presence of plain lifecycle text.
	lifecycleFieldText
	// lifecycleFieldToolCallID records presence of the tool call identifier.
	lifecycleFieldToolCallID
	// lifecycleFieldToolName records presence of the tool name.
	lifecycleFieldToolName
	// lifecycleFieldProgressChannel records presence of the active tool output channel.
	lifecycleFieldProgressChannel
	// lifecycleFieldIsError records presence of the tool or run failure flag.
	lifecycleFieldIsError
	// lifecycleFieldOutcome records presence of the terminal outcome.
	lifecycleFieldOutcome
	// lifecycleFieldErrorMessage records presence of the original diagnostic.
	lifecycleFieldErrorMessage
	// lifecycleFieldAvailability records presence of the Host admission state.
	lifecycleFieldAvailability
	// lifecycleFieldModelContent records presence of the streamed model block.
	lifecycleFieldModelContent
	// lifecycleFieldModelResponse records presence of the terminal model response.
	lifecycleFieldModelResponse
	// lifecycleFieldToolCallPreview records presence of the provisional tool arguments.
	lifecycleFieldToolCallPreview
	// lifecycleFieldFinalToolCall records presence of the completed tool arguments.
	lifecycleFieldFinalToolCall
	// lifecycleFieldContents records presence of the complete tool result content.
	lifecycleFieldContents
)

// validateLifecycleEnvelope validates shared fields and rejects fields owned by inactive variants.
func validateLifecycleEnvelope(lifecycle *uiv1.AgentEvent) error {
	if !lifecycle.HasType() {
		return errors.New("lifecycle type is missing")
	}
	if !lifecycle.HasRunId() {
		return errors.New("lifecycle run ID is missing")
	}
	allowed, err := allowedLifecycleFields(lifecycle.GetType())
	if err != nil {
		return err
	}
	if inactive := presentLifecycleFields(lifecycle) &^ allowed; inactive != 0 {
		return fmt.Errorf("lifecycle type %d has inactive fields 0x%x", lifecycle.GetType(), inactive)
	}
	return nil
}

// allowedLifecycleFields returns the complete field set for one lifecycle variant.
func allowedLifecycleFields(lifecycleType uiv1.LifecycleType) (lifecycleFields, error) {
	base := lifecycleFieldType | lifecycleFieldRunID
	switch lifecycleType {
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START:
		return base, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
		return base | lifecycleFieldModelResponse, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START:
		return base | lifecycleFieldToolCallID | lifecycleFieldToolName |
			lifecycleFieldText | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE:
		return base | lifecycleFieldToolCallID | lifecycleFieldToolName | lifecycleFieldText |
			lifecycleFieldProgressChannel | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END:
		return base | lifecycleFieldToolCallID | lifecycleFieldToolName | lifecycleFieldText |
			lifecycleFieldIsError | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
		return base | lifecycleFieldToolCallID | lifecycleFieldToolName | lifecycleFieldText |
			lifecycleFieldIsError | lifecycleFieldErrorMessage | lifecycleFieldContents, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END:
		return base | lifecycleFieldText | lifecycleFieldIsError |
			lifecycleFieldOutcome | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END:
		return base | lifecycleFieldIsError | lifecycleFieldOutcome | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END:
		return base | lifecycleFieldModelContent, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA:
		return base | lifecycleFieldToolCallPreview, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END:
		return base | lifecycleFieldFinalToolCall, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED:
		return 0, errors.New("lifecycle type is unspecified")
	default:
		return 0, fmt.Errorf("unknown lifecycle type %d", lifecycleType)
	}
}

// presentLifecycleFields records Protobuf presence without collapsing valid scalar zero values.
func presentLifecycleFields(lifecycle *uiv1.AgentEvent) lifecycleFields {
	fields := lifecycleFieldType
	if lifecycle.HasRunId() {
		fields |= lifecycleFieldRunID
	}
	if lifecycle.HasText() {
		fields |= lifecycleFieldText
	}
	if lifecycle.HasToolCallId() {
		fields |= lifecycleFieldToolCallID
	}
	if lifecycle.HasToolName() {
		fields |= lifecycleFieldToolName
	}
	if lifecycle.HasProgressChannel() {
		fields |= lifecycleFieldProgressChannel
	}
	if lifecycle.HasIsError() {
		fields |= lifecycleFieldIsError
	}
	if lifecycle.HasOutcome() {
		fields |= lifecycleFieldOutcome
	}
	if lifecycle.HasErrorMessage() {
		fields |= lifecycleFieldErrorMessage
	}
	if lifecycle.HasAvailability() {
		fields |= lifecycleFieldAvailability
	}
	if lifecycle.HasModelContent() {
		fields |= lifecycleFieldModelContent
	}
	if lifecycle.HasModelResponse() {
		fields |= lifecycleFieldModelResponse
	}
	if lifecycle.HasToolCallPreview() {
		fields |= lifecycleFieldToolCallPreview
	}
	if lifecycle.HasFinalToolCall() {
		fields |= lifecycleFieldFinalToolCall
	}
	if len(lifecycle.GetToolResultContents()) != 0 {
		fields |= lifecycleFieldContents
	}
	return fields
}

// mapModelLifecycle preserves optional streaming and terminal model payloads.
func mapModelLifecycle(event *AgentUpdate, lifecycle *uiv1.AgentEvent) error {
	if lifecycle.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END {
		response := lifecycle.GetModelResponse()
		event.Kind = AgentModelEnd
		if response == nil {
			return errors.New("model response is missing")
		}
		content, err := mapModelResponseContent(response.GetContent())
		if err != nil {
			return err
		}
		event.ModelResponseContent = content
		if response.HasErrorMessage() {
			event.ErrorText = mo.Some(response.GetErrorMessage())
		}
		if response.HasOutcome() {
			event.Status = mo.Some(response.GetOutcome())
		}
		event.Failure = mo.Some(response.HasErrorMessage() && response.GetErrorMessage() != "")
		return nil
	}
	content := lifecycle.GetModelContent()
	if content == nil {
		return errors.New("model content is missing")
	}
	if !content.HasType() {
		return errors.New("model content type is missing")
	}
	if !content.HasPosition() {
		return errors.New("model content position is missing")
	}
	if !content.HasKind() {
		return errors.New("model content kind is missing")
	}
	if err := validateModelContentText(lifecycle.GetType(), content); err != nil {
		return err
	}
	kind, err := mapModelContentDiscriminators(lifecycle.GetType(), content.GetType(), content.GetKind())
	if err != nil {
		return err
	}
	event.Kind = AgentModelDelta
	event.Position = mo.Some(int(content.GetPosition()))
	event.ModelContentKind = mo.Some(kind)
	if content.HasText() {
		event.Text = mo.Some(content.GetText())
	}
	return nil
}

// validateModelContentText enforces the nested text field selected by the lifecycle type.
func validateModelContentText(lifecycleType uiv1.LifecycleType, content *uiv1.ModelContent) error {
	if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA {
		if !content.HasText() {
			return errors.New("model content text is missing")
		}
		return nil
	}
	if content.HasText() {
		return errors.New("model content text must be absent")
	}
	return nil
}

// mapToolCallLifecycle validates preview and final call payloads before projection.
func mapToolCallLifecycle(event *AgentUpdate, lifecycle *uiv1.AgentEvent) error {
	if lifecycle.GetType() != uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END {
		preview := lifecycle.GetToolCallPreview()
		if preview == nil {
			return errors.New("tool call preview is missing")
		}
		mapped, err := mapToolCallPreview(preview)
		if err != nil {
			return err
		}
		event.Kind = AgentToolCallPreview
		event.ToolCall = mo.Some(mapped)
		return nil
	}
	call := lifecycle.GetFinalToolCall()
	if call == nil || call.GetArguments() == nil {
		return errors.New("final tool call is missing")
	}
	if !call.HasCallId() || !call.HasName() || !call.HasPosition() {
		return errors.New("final tool call scalar is missing")
	}
	event.Kind = AgentToolCallFinal
	event.ToolCall = mo.Some(ToolCallState{
		CallID:      call.GetCallId(),
		Name:        call.GetName(),
		Position:    int(call.GetPosition()),
		Provisional: false,
		Fields:      nil,
		Arguments:   call.GetArguments().AsMap(),
	})
	return nil
}

// mapToolLifecycle projects execution updates and terminal result payloads.
func mapToolLifecycle(event *AgentUpdate, lifecycle *uiv1.AgentEvent) error {
	lifecycleType := lifecycle.GetType()
	var err error
	switch int(lifecycleType) {
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START):
		err = mapToolStarted(event, lifecycle)
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE):
		err = mapToolProgress(event, lifecycle)
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END):
		err = mapToolEnded(event, lifecycle)
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT):
		err = mapToolResult(event, lifecycle)
	default:
		return fmt.Errorf("lifecycle type %d is not a tool event", lifecycleType)
	}
	if err != nil {
		return err
	}
	if lifecycle.HasToolCallId() {
		event.ToolCallID = mo.Some(lifecycle.GetToolCallId())
	}
	if lifecycle.HasToolName() {
		event.ToolName = mo.Some(lifecycle.GetToolName())
	}
	if lifecycle.HasText() {
		event.Text = mo.Some(lifecycle.GetText())
	}
	if lifecycle.HasErrorMessage() {
		event.ErrorText = mo.Some(lifecycle.GetErrorMessage())
	}
	return nil
}

// mapToolStarted requires tool identity and marks execution start.
func mapToolStarted(event *AgentUpdate, lifecycle *uiv1.AgentEvent) error {
	if !lifecycle.HasToolCallId() || !lifecycle.HasToolName() {
		return errors.New("started tool identity is missing")
	}
	event.Kind = AgentToolStarted
	event.ToolName = mo.Some(lifecycle.GetToolName())
	event.Status = mo.Some("started")
	return nil
}

// mapToolProgress validates the active progress channel before decoding its payload.
func mapToolProgress(event *AgentUpdate, lifecycle *uiv1.AgentEvent) error {
	if !lifecycle.HasText() || !lifecycle.HasProgressChannel() {
		return errors.New("tool progress is missing")
	}
	return mapProgress(event, lifecycle.GetProgressChannel())
}

// mapToolEnded retains execution success and complete failure diagnostics.
func mapToolEnded(event *AgentUpdate, lifecycle *uiv1.AgentEvent) error {
	if !lifecycle.HasToolCallId() || !lifecycle.HasToolName() || !lifecycle.HasIsError() {
		return errors.New("ended tool result is missing")
	}
	event.Kind = AgentToolEnded
	failure := lifecycle.GetIsError() || lifecycle.GetErrorMessage() != ""
	event.Failure = mo.Some(failure)
	if failure {
		event.Status = mo.Some("error")
	} else {
		event.Status = mo.Some("completed")
	}
	return nil
}

// mapToolResult requires complete result content and preserves tool failure status.
func mapToolResult(event *AgentUpdate, lifecycle *uiv1.AgentEvent) error {
	if !lifecycle.HasToolCallId() || !lifecycle.HasToolName() || !lifecycle.HasIsError() {
		return errors.New("tool result is missing")
	}
	contents, err := mapContents(lifecycle.GetToolResultContents(), false)
	if err != nil {
		return err
	}
	event.Kind = AgentToolResult
	event.Contents = mo.Some(contents)
	event.Failure = mo.Some(lifecycle.GetIsError() || lifecycle.GetErrorMessage() != "")
	return nil
}

// mapTerminalLifecycle preserves turn and agent outcome presence.
func mapTerminalLifecycle(event *AgentUpdate, lifecycle *uiv1.AgentEvent) error {
	if err := validateTerminalLifecyclePresence(lifecycle); err != nil {
		return err
	}
	event.Kind = AgentTurnEnded
	if lifecycle.HasErrorMessage() {
		event.ErrorText = mo.Some(lifecycle.GetErrorMessage())
	}
	event.Failure = mo.Some(lifecycle.GetIsError() || lifecycle.GetErrorMessage() != "")
	return nil
}

// validateTerminalLifecyclePresence checks fields required by terminal agent envelopes.
func validateTerminalLifecyclePresence(lifecycle *uiv1.AgentEvent) error {
	if lifecycle.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END && !lifecycle.HasText() {
		return errors.New("turn summary is missing")
	}
	if lifecycle.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END && !lifecycle.HasOutcome() {
		return errors.New("agent outcome is missing")
	}
	return nil
}
