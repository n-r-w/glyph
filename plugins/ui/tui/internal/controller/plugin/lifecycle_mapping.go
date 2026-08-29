package plugin

import (
	"errors"
	"fmt"

	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

func mapLifecycle(lifecycle *uiv1.LifecycleEvent) (presentationdomain.Event, error) {
	if lifecycle == nil {
		return presentationdomain.Event{}, errors.New("lifecycle event is nil")
	}
	if err := validateLifecycleEnvelope(lifecycle); err != nil {
		return presentationdomain.Event{}, err
	}
	event := presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventUnspecified,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	}

	var err error
	switch lifecycle.GetType() {
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START:
		event.Kind = presentationdomain.EventTurnStarted
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START:
		event.Kind = presentationdomain.EventModelDelta
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
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED:
		err = mapTerminalLifecycle(&event, lifecycle)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED:
		if !lifecycle.HasAvailability() {
			return presentationdomain.Event{}, errors.New("availability is missing")
		}
		availability, mapErr := mapAvailability(lifecycle.GetAvailability())
		if mapErr != nil {
			return presentationdomain.Event{}, mapErr
		}
		event.Kind = presentationdomain.EventAvailability
		event.Availability = mo.Some(availability)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED:
		return presentationdomain.Event{}, errors.New("lifecycle type is unspecified")
	default:
		return presentationdomain.Event{}, fmt.Errorf("unknown lifecycle type %d", lifecycle.GetType())
	}
	if err != nil {
		return presentationdomain.Event{}, err
	}
	return event, nil
}

// lifecycleFields is a presence mask for optional LifecycleEvent payload fields.
type lifecycleFields uint16

const (
	lifecycleFieldType lifecycleFields = 1 << iota
	lifecycleFieldRunID
	lifecycleFieldText
	lifecycleFieldToolCallID
	lifecycleFieldToolName
	lifecycleFieldProgressChannel
	lifecycleFieldIsError
	lifecycleFieldOutcome
	lifecycleFieldErrorMessage
	lifecycleFieldAvailability
	lifecycleFieldModelContent
	lifecycleFieldModelResponse
	lifecycleFieldToolCallPreview
	lifecycleFieldFinalToolCall
	lifecycleFieldContents
)

// validateLifecycleEnvelope validates shared fields and rejects fields owned by inactive variants.
func validateLifecycleEnvelope(lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasType() {
		return errors.New("lifecycle type is missing")
	}
	if lifecycle.GetType() != uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED && !lifecycle.HasRunId() {
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
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED:
		return base | lifecycleFieldIsError | lifecycleFieldOutcome | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED:
		return base | lifecycleFieldAvailability, nil
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
func presentLifecycleFields(lifecycle *uiv1.LifecycleEvent) lifecycleFields {
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
func mapModelLifecycle(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if lifecycle.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END {
		response := lifecycle.GetModelResponse()
		event.Kind = presentationdomain.EventModelEnd
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
	event.Kind = presentationdomain.EventModelDelta
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
func mapToolCallLifecycle(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if lifecycle.GetType() != uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END {
		preview := lifecycle.GetToolCallPreview()
		if preview == nil {
			return errors.New("tool call preview is missing")
		}
		mapped, err := mapToolCallPreview(preview)
		if err != nil {
			return err
		}
		event.Kind = presentationdomain.EventToolCallPreview
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
	event.Kind = presentationdomain.EventToolCallFinal
	event.ToolCall = mo.Some(presentationdomain.ToolCallState{
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
func mapToolLifecycle(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
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

func mapToolStarted(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasToolCallId() || !lifecycle.HasToolName() {
		return errors.New("started tool identity is missing")
	}
	event.Kind = presentationdomain.EventToolStarted
	event.ToolName = mo.Some(lifecycle.GetToolName())
	event.Status = mo.Some("started")
	return nil
}

func mapToolProgress(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasText() || !lifecycle.HasProgressChannel() {
		return errors.New("tool progress is missing")
	}
	return mapProgress(event, lifecycle.GetProgressChannel())
}

func mapToolEnded(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasToolCallId() || !lifecycle.HasToolName() || !lifecycle.HasIsError() {
		return errors.New("ended tool result is missing")
	}
	event.Kind = presentationdomain.EventToolEnded
	failure := lifecycle.GetIsError() || lifecycle.GetErrorMessage() != ""
	event.Failure = mo.Some(failure)
	if failure {
		event.Status = mo.Some("error")
	} else {
		event.Status = mo.Some("completed")
	}
	return nil
}

func mapToolResult(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasToolCallId() || !lifecycle.HasToolName() || !lifecycle.HasIsError() {
		return errors.New("tool result is missing")
	}
	contents, err := mapContents(lifecycle.GetToolResultContents(), false)
	if err != nil {
		return err
	}
	event.Kind = presentationdomain.EventToolResult
	event.Contents = mo.Some(contents)
	event.Failure = mo.Some(lifecycle.GetIsError() || lifecycle.GetErrorMessage() != "")
	return nil
}

// mapTerminalLifecycle preserves turn and settlement outcome presence.
func mapTerminalLifecycle(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if err := validateTerminalLifecyclePresence(lifecycle); err != nil {
		return err
	}
	if lifecycle.GetType() != uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED {
		event.Kind = presentationdomain.EventTurnEnded
		if lifecycle.HasErrorMessage() {
			event.ErrorText = mo.Some(lifecycle.GetErrorMessage())
		}
		event.Failure = mo.Some(lifecycle.GetIsError() || lifecycle.GetErrorMessage() != "")
		return nil
	}
	event.Kind = presentationdomain.EventAgentSettled
	if lifecycle.HasErrorMessage() {
		event.ErrorText = mo.Some(lifecycle.GetErrorMessage())
	}
	if lifecycle.HasOutcome() {
		event.Status = mo.Some(lifecycle.GetOutcome())
	}
	if lifecycle.HasIsError() || lifecycle.HasErrorMessage() {
		event.Failure = mo.Some(lifecycle.GetIsError() || lifecycle.GetErrorMessage() != "")
	}
	if lifecycle.HasErrorMessage() && lifecycle.GetErrorMessage() != "" {
		event.Text = mo.Some(lifecycle.GetErrorMessage())
	} else if lifecycle.HasOutcome() {
		event.Text = mo.Some(lifecycle.GetOutcome())
	}
	return nil
}

func validateTerminalLifecyclePresence(lifecycle *uiv1.LifecycleEvent) error {
	if lifecycle.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END && !lifecycle.HasText() {
		return errors.New("turn summary is missing")
	}
	if lifecycle.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END && !lifecycle.HasOutcome() {
		return errors.New("agent outcome is missing")
	}
	return nil
}
