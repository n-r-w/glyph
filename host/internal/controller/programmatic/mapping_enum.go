package programmatic

import (
	"errors"
	"fmt"

	"github.com/samber/mo"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// mapRunState maps one internal run state to its public enum.
func mapRunState(state RunState) (programmaticv1.RunState, error) {
	switch state {
	case RunStateIdle:
		return programmaticv1.RunState_RUN_STATE_IDLE, nil
	case RunStateRunning:
		return programmaticv1.RunState_RUN_STATE_RUNNING, nil
	case RunStateUnspecified:
		return 0, errors.New("map run state: unspecified value")
	default:
		return 0, fmt.Errorf("map run state: unknown value %d", state)
	}
}

// mapAgentEventType maps one internal agent event type to its public enum.
//
//nolint:gocyclo // The closed event enum is mapped exhaustively.
func mapAgentEventType(eventType AgentEventType) (programmaticv1.AgentEventType, error) {
	switch eventType {
	case AgentEventAgentStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_START, nil
	case AgentEventTurnStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TURN_START, nil
	case AgentEventMessageStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MESSAGE_START, nil
	case AgentEventModelContentStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MODEL_CONTENT_START, nil
	case AgentEventModelTextDelta:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MODEL_TEXT_DELTA, nil
	case AgentEventModelContentEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MODEL_CONTENT_END, nil
	case AgentEventToolCallStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_CALL_START, nil
	case AgentEventToolCallDelta:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_CALL_DELTA, nil
	case AgentEventToolCallEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_CALL_END, nil
	case AgentEventMessageEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MESSAGE_END, nil
	case AgentEventToolExecutionStart:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_START, nil
	case AgentEventToolExecutionUpdate:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_UPDATE, nil
	case AgentEventToolExecutionEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END, nil
	case AgentEventToolResult:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT, nil
	case AgentEventTurnEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TURN_END, nil
	case AgentEventAgentEnd:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_END, nil
	case AgentEventUnspecified:
		return 0, errors.New("map agent event type: unspecified value")
	default:
		return 0, fmt.Errorf("map agent event type: unknown value %d", eventType)
	}
}

// mapModelContentKind maps one internal model content kind to its public enum.
func mapModelContentKind(kind ModelContentKind) (programmaticv1.ModelContentKind, error) {
	switch kind {
	case ModelContentText:
		return programmaticv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT, nil
	case ModelContentReasoning:
		return programmaticv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING, nil
	case ModelContentRefusal:
		return programmaticv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL, nil
	case ModelContentUnspecified:
		return 0, errors.New("map model content kind: unspecified value")
	default:
		return 0, fmt.Errorf("map model content kind: unknown value %d", kind)
	}
}

// mapProgressChannel maps one internal progress channel to its public enum.
func mapProgressChannel(channel ProgressChannel) (programmaticv1.ProgressChannel, error) {
	switch channel {
	case ProgressChannelStatus:
		return programmaticv1.ProgressChannel_PROGRESS_CHANNEL_STATUS, nil
	case ProgressChannelStdout:
		return programmaticv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT, nil
	case ProgressChannelStderr:
		return programmaticv1.ProgressChannel_PROGRESS_CHANNEL_STDERR, nil
	case ProgressChannelUnspecified:
		return 0, errors.New("map progress channel: unspecified value")
	default:
		return 0, fmt.Errorf("map progress channel: unknown value %d", channel)
	}
}

// mapRequiredModelOutcome maps the required outcome at the Protobuf boundary.
func mapRequiredModelOutcome(outcome mo.Option[ModelOutcome]) (programmaticv1.ModelOutcome, error) {
	outcomeValue, ok := outcome.Get()
	if !ok {
		return 0, errors.New("map model response: missing outcome")
	}
	return mapModelOutcome(outcomeValue)
}

// mapModelOutcome maps one internal model outcome to its public enum.
func mapModelOutcome(outcome ModelOutcome) (programmaticv1.ModelOutcome, error) {
	switch outcome {
	case ModelOutcomeStop:
		return programmaticv1.ModelOutcome_MODEL_OUTCOME_STOP, nil
	case ModelOutcomeToolUse:
		return programmaticv1.ModelOutcome_MODEL_OUTCOME_TOOL_USE, nil
	case ModelOutcomeLength:
		return programmaticv1.ModelOutcome_MODEL_OUTCOME_LENGTH, nil
	case ModelOutcomeAborted:
		return programmaticv1.ModelOutcome_MODEL_OUTCOME_ABORTED, nil
	case ModelOutcomeFailed:
		return programmaticv1.ModelOutcome_MODEL_OUTCOME_FAILED, nil
	case ModelOutcomeUnspecified:
		return 0, errors.New("map model outcome: unspecified value")
	default:
		return 0, fmt.Errorf("map model outcome: unknown value %d", outcome)
	}
}

// mapRunOutcome maps one internal run outcome to its public enum.
func mapRunOutcome(outcome RunOutcome) (programmaticv1.RunOutcome, error) {
	switch outcome {
	case RunOutcomeCompleted:
		return programmaticv1.RunOutcome_RUN_OUTCOME_COMPLETED, nil
	case RunOutcomeAborted:
		return programmaticv1.RunOutcome_RUN_OUTCOME_ABORTED, nil
	case RunOutcomeFailed:
		return programmaticv1.RunOutcome_RUN_OUTCOME_FAILED, nil
	case RunOutcomeUnspecified:
		return 0, errors.New("map run outcome: unspecified value")
	default:
		return 0, fmt.Errorf("map run outcome: unknown value %d", outcome)
	}
}
