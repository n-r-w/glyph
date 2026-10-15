package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// HandleCompactionRequest invokes one public compaction request handler.
func (r *Runtime) HandleCompactionRequest(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation extensioncontext.RequestInvocation,
) (extensioncontext.RequestAction, error) {
	payload, err := mapCompactionRequestInvocation(invocation)
	if err != nil {
		return extensioncontext.RequestAction{}, err
	}
	request := newCompactionParticipantRequest(handlerID, binding, invocation.ParentCompactionID)
	request.SetCompactionRequest(payload)
	response, err := r.runCompactionParticipant(ctx, handlerID, request)
	if response == nil {
		return extensioncontext.RequestAction{}, err
	}
	action, decodeErr := r.decodeCompactionRequestResponse(response)
	return action, errors.Join(err, decodeErr)
}

// GenerateCompaction invokes one public compaction generation capability.
func (r *Runtime) GenerateCompaction(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation extensioncontext.RequestInvocation,
) (extensioncontext.Result, error) {
	payload, err := mapCompactionRequestInvocation(invocation)
	if err != nil {
		return extensioncontext.Result{}, err
	}
	request := newCompactionParticipantRequest(handlerID, binding, invocation.ParentCompactionID)
	request.SetCompactionGenerate(payload)
	response, err := r.runCompactionParticipant(ctx, handlerID, request)
	if response == nil {
		return extensioncontext.Result{}, err
	}
	result, decodeErr := r.decodeCompactionGeneratorResponse(response)
	return result, errors.Join(err, decodeErr)
}

// HandleCompactionResult invokes one public compaction result handler.
func (r *Runtime) HandleCompactionResult(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation extensioncontext.ResultInvocation,
) (extensioncontext.ResultAction, error) {
	payload, err := mapCompactionResultInvocation(invocation)
	if err != nil {
		return extensioncontext.ResultAction{}, err
	}
	request := newCompactionParticipantRequest(handlerID, binding, invocation.ParentCompactionID)
	request.SetCompactionResult(payload)
	response, err := r.runCompactionParticipant(ctx, handlerID, request)
	if response == nil {
		return extensioncontext.ResultAction{}, err
	}
	action, decodeErr := r.decodeCompactionResultResponse(response)
	return action, errors.Join(err, decodeErr)
}

// ObserveCompactionSuccess invokes one public committed-compaction observer.
func (r *Runtime) ObserveCompactionSuccess(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation extensioncontext.OutcomeInvocation,
) error {
	return r.observeCompaction(ctx, handlerID, binding, invocation, true)
}

// ObserveCompactionFailure invokes one public failed-compaction observer.
func (r *Runtime) ObserveCompactionFailure(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation extensioncontext.OutcomeInvocation,
) error {
	return r.observeCompaction(ctx, handlerID, binding, invocation, false)
}

// observeCompaction invokes one outcome observer and validates its acknowledgement kind.
func (r *Runtime) observeCompaction(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation extensioncontext.OutcomeInvocation,
	success bool,
) (terminalCause error) {
	category := modelexecution.FailureExtensionFailed
	defer func() {
		terminalCause = extensioncontext.NewPolicyOperationError(ctx, category, terminalCause)
	}()
	payload, err := mapCompactionOutcome(invocation)
	if err != nil {
		category = modelexecution.FailureInternal
		return err
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active compaction payload.
	builder := extensionpb.HandleRequest_builder{
		CompletedConversation: nil,
		HandlerId:             new(handlerID), Context: mapContext(binding),
		ParentCompactionId: new(invocation.ParentCompactionID.OrEmpty()),
		CompactionPolicy:   nil,
	}
	if success {
		builder.CompactionSuccess = payload
	} else {
		builder.CompactionFailure = payload
	}
	completion, err := r.runCompactionHandler(ctx, handlerID, builder.Build())
	response := completion.Response
	if response == nil || response.GetError() != nil {
		return err
	}
	if success && response.GetCompactionSuccess() == nil || !success && response.GetCompactionFailure() == nil {
		return errors.Join(err, r.protocolViolation(errors.New("compaction observer returned another action kind")))
	}
	return err
}

// runCompactionHandler executes one compaction payload through the existing handler operation path.
func (r *Runtime) runCompactionHandler(
	ctx context.Context,
	handlerID string,
	request *extensionpb.HandleRequest,
) (compactionHandlerCompletion, error) {
	hostRequest := new(extensionpb.HostRequest)
	hostRequest.SetHandle(request)
	operationID := r.operationID()
	started, err := r.connection.Start(ctx, operationID, hostRequest)
	if err != nil {
		err = extensioncontext.NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed,
			r.handlerOperationError(ctx, handlerID, err))
		return compactionHandlerCompletion{Response: nil, InvocationCause: err}, err
	}
	completed, waitErr := r.waitForOperation(ctx, operationID, started, nil)
	if waitErr != nil {
		waitErr = extensioncontext.NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed,
			r.handlerOperationError(ctx, handlerID, waitErr))
	}
	if completed == nil {
		return compactionHandlerCompletion{Response: nil, InvocationCause: waitErr}, waitErr
	}
	response := completed.GetHandle()
	if response == nil {
		cause := extensioncontext.NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed,
			errors.Join(waitErr, r.protocolViolation(errors.New("compaction handler response is missing"))))
		return compactionHandlerCompletion{Response: nil, InvocationCause: cause}, cause
	}
	completion := compactionHandlerCompletion{Response: response, InvocationCause: waitErr}
	if handlerErr := response.GetError(); handlerErr != nil {
		cause := error(newOrdinaryHandlerError(handlerErr.GetMessage()))
		if request.GetCompactionOutgoingInput() == nil {
			cause = extensioncontext.NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed, cause)
		}
		return completion, errors.Join(waitErr, cause)
	}
	return completion, waitErr
}

// compactionHandlerCompletion contains received handler data and a separate invocation failure.
type compactionHandlerCompletion struct {
	// Response contains the acquired handler completion.
	Response *extensionpb.HandleResponse
	// InvocationCause contains start, wait, and protocol failures separately from a received semantic rejection.
	InvocationCause error
}

// mapCompactionRequestInvocation projects immutable and current request state.
func mapCompactionRequestInvocation(
	invocation extensioncontext.RequestInvocation,
) (*extensionpb.CompactionRequestInvocation, error) {
	original, err := mapCompactionRequest(invocation.Original)
	if err != nil {
		return nil, err
	}
	current, err := mapCompactionRequest(invocation.Current)
	if err != nil {
		return nil, err
	}
	var currentResult *extensionpb.CompactionResult
	if result, present := invocation.CurrentResult.Get(); present {
		currentResult = mapCompactionResult(result)
	}
	return extensionpb.CompactionRequestInvocation_builder{
		Original: original, Current: current, CurrentResult: currentResult,
	}.Build(), nil
}

// mapCompactionResultInvocation projects immutable request and result state.
func mapCompactionResultInvocation(
	invocation extensioncontext.ResultInvocation,
) (*extensionpb.CompactionResultInvocation, error) {
	original, err := mapCompactionRequest(invocation.OriginalRequest)
	if err != nil {
		return nil, err
	}
	current, err := mapCompactionRequest(invocation.CurrentRequest)
	if err != nil {
		return nil, err
	}
	return extensionpb.CompactionResultInvocation_builder{
		OriginalRequest: original, CurrentRequest: current,
		OriginalResult: mapCompactionResult(invocation.OriginalResult),
		CurrentResult:  mapCompactionResult(invocation.CurrentResult),
	}.Build(), nil
}

// mapCompactionRequest projects one Host request without provider replay or hidden extension state.
func mapCompactionRequest(request extensioncontext.Request) (*extensionpb.CompactionRequest, error) {
	prefix, err := mapCompactionEntries(request.Prefix)
	if err != nil {
		return nil, err
	}
	suffix, err := mapCompactionEntries(request.Suffix)
	if err != nil {
		return nil, err
	}
	var instructions *string
	if value, present := request.Instructions.Get(); present {
		instructions = new(value)
	}
	var previous *extensionpb.SessionTreeCompaction
	if value, present := request.Previous.Get(); present {
		previous = extensioncontroller.MapCompactionMarker(value)
	}
	return extensionpb.CompactionRequest_builder{
		Trigger: new(mapCompactionTrigger(request.Trigger)), RetryIntent: new(request.RetryIntent),
		Instructions: instructions, Model: mapCompactionModel(request.Model),
		ReasoningChoice: new(string(request.ReasoningChoice)), Prefix: prefix, Suffix: suffix, Previous: previous,
		ContextTokens: new(request.ContextTokens), ContextTokensEstimated: new(request.ContextTokensEstimated),
		ContextWindow: new(request.ContextWindow), ResponseBudget: new(request.ResponseBudget),
		RetainedBudget: new(request.RetainedBudget),
	}.Build(), nil
}

// mapCompactionEntries reuses the public session-entry projection for every source record.
func mapCompactionEntries(entries []extensioncontext.InputEntry) ([]*extensionpb.CompactionInputEntry, error) {
	mapped := make([]*extensionpb.CompactionInputEntry, len(entries))
	for index := range entries {
		value, err := mapSessionEntry(extensionruntime.ProjectTreeEntry(entries[index].Entry))
		if err != nil {
			return nil, fmt.Errorf("map compaction entry %d: %w", index, err)
		}
		mapped[index] = extensionpb.CompactionInputEntry_builder{
			Entry: value, EstimatedTokens: new(entries[index].EstimatedTokens),
		}.Build()
	}
	return mapped, nil
}

// mapCompactionResult projects one extension result.
func mapCompactionResult(result extensioncontext.Result) *extensionpb.CompactionResult {
	builder := extensionpb.CompactionResult_builder{
		Summary: new(result.Summary), FirstKeptEntryId: new(result.FirstKeptEntryID),
		Source: mapSummarySource(result.Source), Details: nil,
	}
	if details, present := result.Details.Get(); present {
		builder.Details = append([]byte(nil), details...)
	}
	return builder.Build()
}

// mapCompactionResultFromProto decodes a supplied result and optional producer details.
func mapCompactionResultFromProto(value *extensionpb.CompactionResult) extensioncontext.Result {
	result := extensioncontext.Result{
		Summary: value.GetSummary(), FirstKeptEntryID: value.GetFirstKeptEntryId(),
		Source: extensioncontroller.DecodeCompactionSource(value.GetSource()), Details: mo.None[[]byte](),
	}
	if value.HasDetails() {
		result.Details = mo.Some(append([]byte(nil), value.GetDetails()...))
	}
	return result
}

// mapCompactionRequestAction maps explicit public action variants without repairing malformed shapes.
func mapCompactionRequestAction(
	action *extensionpb.CompactionRequestAction,
) (extensioncontext.RequestAction, error) {
	result := extensioncontext.RequestAction{
		Cancel:        action.GetCancel(),
		RequestAction: mapCompactionRequestDisposition(action.GetRequestAction()),
		Request:       mo.None[extensioncontext.Request](),
		ResultAction:  mapCompactionResultDisposition(action.GetResultAction()),
		Result:        mo.None[extensioncontext.Result](),
	}
	if action.GetRequest() != nil {
		replacement, err := mapCompactionRequestFromProto(action.GetRequest())
		if err != nil {
			return extensioncontext.RequestAction{}, err
		}
		result.Request = mo.Some(replacement)
	}
	if action.GetResult() != nil {
		result.Result = mo.Some(mapCompactionResultFromProto(action.GetResult()))
	}
	return result, nil
}

// mapCompactionResultAction maps explicit preservation, replacement, and cancellation variants.
func mapCompactionResultAction(
	action *extensionpb.CompactionResultAction,
) (extensioncontext.ResultAction, error) {
	disposition := action.GetResultAction()
	if action.GetCancel() {
		if disposition != extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED ||
			action.GetResult() != nil {
			return extensioncontext.ResultAction{}, errors.New("compaction cancellation must be exclusive")
		}
		return extensioncontext.ResultAction{
			Cancel: true, ResultAction: 0, Result: mo.None[extensioncontext.Result](),
		}, nil
	}
	if disposition != extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE &&
		disposition != extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE {
		return extensioncontext.ResultAction{}, errors.New("compaction result disposition is invalid")
	}
	result := extensioncontext.ResultAction{
		Cancel:       false,
		ResultAction: mapCompactionResultDisposition(disposition),
		Result:       mo.None[extensioncontext.Result](),
	}
	if action.GetResult() != nil {
		result.Result = mo.Some(mapCompactionResultFromProto(action.GetResult()))
	}
	return result, nil
}

// mapCompactionOutcome projects terminal state and optional durable commit.
func mapCompactionOutcome(invocation extensioncontext.OutcomeInvocation) (*extensionpb.CompactionOutcome, error) {
	builder := extensionpb.CompactionOutcome_builder{
		State:     nil,
		Committed: nil,
		Canceled:  new(invocation.Canceled),
		Error:     nil,
	}
	var err error
	if state, present := invocation.State.Get(); present {
		builder.State, err = mapPolicyState(state)
		if err != nil {
			return nil, err
		}
	}
	if committed, present := invocation.Committed.Get(); present {
		builder.Committed, err = mapSessionEntry(extensionruntime.ProjectTreeEntry(committed))
		if err != nil {
			return nil, err
		}
	}
	if message, present := invocation.Error.Get(); present {
		builder.Error = new(message)
	}
	return builder.Build(), nil
}

// mapCompactionRequestFromProto decodes a public replacement request.
func mapCompactionRequestFromProto(value *extensionpb.CompactionRequest) (extensioncontext.Request, error) {
	if !value.HasContextTokensEstimated() || !value.GetContextTokensEstimated() {
		return extensioncontext.Request{}, errors.New(
			"compaction context token estimate marker must be present and true",
		)
	}
	prefix, err := mapCompactionEntriesFromProto(value.GetPrefix())
	if err != nil {
		return extensioncontext.Request{}, err
	}
	suffix, err := mapCompactionEntriesFromProto(value.GetSuffix())
	if err != nil {
		return extensioncontext.Request{}, err
	}
	return extensioncontext.Request{
		Trigger:      mapCompactionTriggerFromProto(value.GetTrigger()),
		RetryIntent:  value.GetRetryIntent(),
		Instructions: optionalString(value.HasInstructions(), value.GetInstructions()),
		Model: extensioncontroller.DecodeCompactionRequestModel(
			value.GetModel(),
		),
		ReasoningChoice:        model.ReasoningChoice(value.GetReasoningChoice()),
		Prefix:                 prefix,
		Suffix:                 suffix,
		Previous:               extensioncontroller.DecodeCompactionPrevious(value.GetPrevious()),
		ContextTokens:          value.GetContextTokens(),
		ContextTokensEstimated: value.GetContextTokensEstimated(),
		ContextWindow:          value.GetContextWindow(),
		ResponseBudget:         value.GetResponseBudget(),
		RetainedBudget:         value.GetRetainedBudget(),
	}, nil
}

// mapCompactionEntriesFromProto decodes replacement entries and their token estimates.
func mapCompactionEntriesFromProto(values []*extensionpb.CompactionInputEntry) ([]extensioncontext.InputEntry, error) {
	entries := make([]extensioncontext.InputEntry, len(values))
	for index := range values {
		if values[index] == nil || values[index].GetEntry() == nil {
			return nil, fmt.Errorf("map replacement compaction entry %d: payload is missing", index)
		}
		entry, err := extensioncontroller.DecodeCompactionRequestEntry(values[index].GetEntry())
		if err != nil {
			return nil, fmt.Errorf("map replacement compaction entry %d: %w", index, err)
		}
		entries[index] = extensioncontext.InputEntry{
			Entry: entry, EstimatedTokens: values[index].GetEstimatedTokens(),
		}
	}
	return entries, nil
}

// mapCompactionRequestDisposition maps the closed public request action.
func mapCompactionRequestDisposition(
	value extensionpb.CompactionRequestDisposition,
) extensioncontext.RequestActionKind {
	switch value {
	case extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE:
		return extensioncontext.RequestActionPreserve
	case extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE:
		return extensioncontext.RequestActionReplace
	case extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// mapCompactionResultDisposition maps the closed public result action.
func mapCompactionResultDisposition(
	value extensionpb.CompactionResultDisposition,
) extensioncontext.ResultActionKind {
	switch value {
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE:
		return extensioncontext.ResultActionPreserve
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE:
		return extensioncontext.ResultActionReplace
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR:
		return extensioncontext.ResultActionClear
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// mapCompactionTriggerFromProto maps the closed public trigger.
func mapCompactionTriggerFromProto(value extensionpb.CompactionTrigger) extensioncontext.Trigger {
	switch value {
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL:
		return extensioncontext.TriggerManual
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_THRESHOLD:
		return extensioncontext.TriggerThreshold
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW:
		return extensioncontext.TriggerOverflow
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// mapCompactionTrigger maps the closed internal trigger.
func mapCompactionTrigger(trigger extensioncontext.Trigger) extensionpb.CompactionTrigger {
	return extensionpb.CompactionTrigger(trigger)
}

// optionalString preserves public scalar presence.
func optionalString(present bool, value string) mo.Option[string] {
	if !present {
		return mo.None[string]()
	}
	return mo.Some(value)
}

// mapPolicyState encodes acquired composition values for outcome observers.
func mapPolicyState(state extensioncontext.PolicyState) (*extensionpb.CompactionPolicyState, error) {
	original, err := mapCompactionRequest(state.OriginalRequest)
	if err != nil {
		return nil, err
	}
	current, err := mapCompactionRequest(state.CurrentRequest)
	if err != nil {
		return nil, err
	}
	result := (*extensionpb.CompactionResult)(nil)
	if value, present := state.Result.Get(); present {
		result = mapCompactionResult(value)
	}
	return extensionpb.CompactionPolicyState_builder{
		OriginalRequest: original,
		CurrentRequest:  current,
		Result:          result,
	}.Build(), nil
}

// mapPolicyStateFromProto decodes the diagnostic state actually supplied by the policy.
func mapPolicyStateFromProto(raw *extensionpb.CompactionPolicyState) (extensioncontext.PolicyState, error) {
	if raw.GetOriginalRequest() == nil || raw.GetCurrentRequest() == nil {
		return extensioncontext.PolicyState{}, errors.New(
			"policy diagnostic original and current requests are required",
		)
	}
	original, err := mapCompactionRequestFromProto(raw.GetOriginalRequest())
	if err != nil {
		return extensioncontext.PolicyState{}, err
	}
	current, err := mapCompactionRequestFromProto(raw.GetCurrentRequest())
	if err != nil {
		return extensioncontext.PolicyState{}, err
	}
	result := mo.None[extensioncontext.Result]()
	if value := raw.GetResult(); value != nil {
		result = mo.Some(mapCompactionResultFromProto(value))
	}
	return extensioncontext.PolicyState{OriginalRequest: original, CurrentRequest: current, Result: result}, nil
}
