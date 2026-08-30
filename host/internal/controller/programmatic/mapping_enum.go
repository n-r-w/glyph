package programmatic

import (
	"errors"
	"fmt"
	"math"

	"github.com/samber/mo"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

//nolint:gocyclo // The exhaustive switch maps every closed command kind explicitly.
func mapCommandType(kind CommandKind) (programmaticv1.CommandType, error) {
	switch kind {
	case CommandUnspecified:
		return programmaticv1.CommandType_COMMAND_TYPE_UNSPECIFIED, nil
	case CommandUserRequest:
		return programmaticv1.CommandType_COMMAND_TYPE_USER_REQUEST, nil
	case CommandAbort:
		return programmaticv1.CommandType_COMMAND_TYPE_ABORT, nil
	case CommandGetRunState:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_RUN_STATE, nil
	case CommandGetMessages:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_MESSAGES, nil
	case CommandGetModels:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_MODELS, nil
	case CommandSelectModel:
		return programmaticv1.CommandType_COMMAND_TYPE_SELECT_MODEL, nil
	case CommandSelectReasoningChoice:
		return programmaticv1.CommandType_COMMAND_TYPE_SELECT_REASONING_CHOICE, nil
	case CommandCreateSession:
		return programmaticv1.CommandType_COMMAND_TYPE_CREATE_SESSION, nil
	case CommandListSessions:
		return programmaticv1.CommandType_COMMAND_TYPE_LIST_SESSIONS, nil
	case CommandResumeSession:
		return programmaticv1.CommandType_COMMAND_TYPE_RESUME_SESSION, nil
	case CommandSetSessionName:
		return programmaticv1.CommandType_COMMAND_TYPE_SET_SESSION_NAME, nil
	case CommandGetSessionInfo:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_SESSION_INFO, nil
	case CommandGetSessionEntries:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_SESSION_ENTRIES, nil
	case CommandGetSessionStats:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_SESSION_STATS, nil
	case CommandGetSessionTree:
		return programmaticv1.CommandType_COMMAND_TYPE_GET_SESSION_TREE, nil
	case CommandNavigateSessionTree:
		return programmaticv1.CommandType_COMMAND_TYPE_NAVIGATE_SESSION_TREE, nil
	case CommandForkSession:
		return programmaticv1.CommandType_COMMAND_TYPE_FORK_SESSION, nil
	case CommandCloneSession:
		return programmaticv1.CommandType_COMMAND_TYPE_CLONE_SESSION, nil
	case CommandSetEntryLabel:
		return programmaticv1.CommandType_COMMAND_TYPE_SET_ENTRY_LABEL, nil
	default:
		return 0, fmt.Errorf("map command type: unknown value %d", kind)
	}
}

//nolint:gocyclo // The switch maps every closed public rejection code.
func mapRejectionCode(code RejectionCode) (programmaticv1.RejectionCode, error) {
	switch code {
	case RejectionInvalidArgument:
		return programmaticv1.RejectionCode_REJECTION_CODE_INVALID_ARGUMENT, nil
	case RejectionBusy:
		return programmaticv1.RejectionCode_REJECTION_CODE_BUSY, nil
	case RejectionNoActiveRun:
		return programmaticv1.RejectionCode_REJECTION_CODE_NO_ACTIVE_RUN, nil
	case RejectionCorrelationInUse:
		return programmaticv1.RejectionCode_REJECTION_CODE_CORRELATION_IN_USE, nil
	case RejectionInternal:
		return programmaticv1.RejectionCode_REJECTION_CODE_INTERNAL, nil
	case RejectionNotFound:
		return programmaticv1.RejectionCode_REJECTION_CODE_NOT_FOUND, nil
	case RejectionReasoningUnsupported:
		return programmaticv1.RejectionCode_REJECTION_CODE_REASONING_UNSUPPORTED, nil
	case RejectionCredentialUnavailable:
		return programmaticv1.RejectionCode_REJECTION_CODE_CREDENTIAL_UNAVAILABLE, nil
	case RejectionSessionUnavailable:
		return programmaticv1.RejectionCode_REJECTION_CODE_SESSION_UNAVAILABLE, nil
	case RejectionPersistenceUnavailable:
		return programmaticv1.RejectionCode_REJECTION_CODE_PERSISTENCE_UNAVAILABLE, nil
	case RejectionModelUnavailable:
		return programmaticv1.RejectionCode_REJECTION_CODE_MODEL_UNAVAILABLE, nil
	case RejectionModelFailed:
		return programmaticv1.RejectionCode_REJECTION_CODE_MODEL_FAILED, nil
	case RejectionExtensionInvalidResult:
		return programmaticv1.RejectionCode_REJECTION_CODE_EXTENSION_INVALID_RESULT, nil
	case RejectionExtensionUnavailable:
		return programmaticv1.RejectionCode_REJECTION_CODE_EXTENSION_UNAVAILABLE, nil
	case RejectionUnspecified:
		return 0, errors.New("map rejection code: unspecified value")
	default:
		return 0, fmt.Errorf("map rejection code: unknown value %d", code)
	}
}

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
	case AgentEventAgentSettled:
		return programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED, nil
	case AgentEventUnspecified:
		return 0, errors.New("map agent event type: unspecified value")
	default:
		return 0, fmt.Errorf("map agent event type: unknown value %d", eventType)
	}
}

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

func mapPosition(position int) (int32, error) {
	if position < math.MinInt32 || position > math.MaxInt32 {
		return 0, fmt.Errorf("map position: value %d exceeds int32", position)
	}
	return int32(position), nil
}
