package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// HandleCompactionRequest invokes one public compaction request handler.
//
//nolint:dupl // Each closed handler variant validates a different response union member.
func (r *Runtime) HandleCompactionRequest(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation contextcompaction.RequestInvocation,
) (contextcompaction.RequestAction, error) {
	payload, err := mapCompactionRequestInvocation(invocation)
	if err != nil {
		return contextcompaction.RequestAction{}, err
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active compaction payload.
	request := extensionpb.HandleRequest_builder{
		HandlerId: new(handlerID), Context: mapContext(binding), CompactionRequest: payload,
	}.Build()
	response, err := r.runCompactionHandler(ctx, handlerID, request)
	if err != nil {
		return contextcompaction.RequestAction{}, err
	}
	action := response.GetCompactionRequest()
	if action == nil {
		return contextcompaction.RequestAction{}, r.protocolViolation(
			errors.New("compaction request handler returned another action kind"),
		)
	}
	return mapCompactionRequestAction(action)
}

// GenerateCompaction invokes one public compaction generation capability.
func (r *Runtime) GenerateCompaction(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation contextcompaction.RequestInvocation,
) (contextcompaction.Result, error) {
	payload, err := mapCompactionRequestInvocation(invocation)
	if err != nil {
		return contextcompaction.Result{}, err
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active compaction payload.
	request := extensionpb.HandleRequest_builder{
		HandlerId: new(handlerID), Context: mapContext(binding), CompactionGenerate: payload,
	}.Build()
	response, err := r.runCompactionHandler(ctx, handlerID, request)
	if err != nil {
		return contextcompaction.Result{}, err
	}
	result := response.GetCompactionGenerate()
	if result == nil {
		return contextcompaction.Result{}, r.protocolViolation(
			errors.New("compaction generator returned another action kind"),
		)
	}
	return mapCompactionResultFromProto(result), nil
}

// HandleCompactionResult invokes one public compaction result handler.
//
//nolint:dupl // Each closed handler variant validates a different response union member.
func (r *Runtime) HandleCompactionResult(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation contextcompaction.ResultInvocation,
) (contextcompaction.ResultAction, error) {
	payload, err := mapCompactionResultInvocation(invocation)
	if err != nil {
		return contextcompaction.ResultAction{}, err
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active compaction payload.
	request := extensionpb.HandleRequest_builder{
		HandlerId: new(handlerID), Context: mapContext(binding), CompactionResult: payload,
	}.Build()
	response, err := r.runCompactionHandler(ctx, handlerID, request)
	if err != nil {
		return contextcompaction.ResultAction{}, err
	}
	action := response.GetCompactionResult()
	if action == nil {
		return contextcompaction.ResultAction{}, r.protocolViolation(
			errors.New("compaction result handler returned another action kind"),
		)
	}
	return mapCompactionResultAction(action)
}

// ObserveCompactionSuccess invokes one public committed-compaction observer.
func (r *Runtime) ObserveCompactionSuccess(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation contextcompaction.OutcomeInvocation,
) error {
	return r.observeCompaction(ctx, handlerID, binding, invocation, true)
}

// ObserveCompactionFailure invokes one public failed-compaction observer.
func (r *Runtime) ObserveCompactionFailure(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation contextcompaction.OutcomeInvocation,
) error {
	return r.observeCompaction(ctx, handlerID, binding, invocation, false)
}

// observeCompaction invokes one outcome observer and validates its acknowledgement kind.
func (r *Runtime) observeCompaction(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	invocation contextcompaction.OutcomeInvocation,
	success bool,
) error {
	payload, err := mapCompactionOutcome(invocation)
	if err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active compaction payload.
	builder := extensionpb.HandleRequest_builder{HandlerId: new(handlerID), Context: mapContext(binding)}
	if success {
		builder.CompactionSuccess = payload
	} else {
		builder.CompactionFailure = payload
	}
	response, err := r.runCompactionHandler(ctx, handlerID, builder.Build())
	if err != nil {
		return err
	}
	if success && response.GetCompactionSuccess() == nil || !success && response.GetCompactionFailure() == nil {
		return r.protocolViolation(errors.New("compaction observer returned another action kind"))
	}
	return nil
}

// runCompactionHandler executes one compaction payload through the existing handler operation path.
func (r *Runtime) runCompactionHandler(
	ctx context.Context,
	handlerID string,
	request *extensionpb.HandleRequest,
) (*extensionpb.HandleResponse, error) {
	hostRequest := new(extensionpb.HostRequest)
	hostRequest.SetHandle(request)
	operationID := r.operationID()
	started, err := r.connection.Start(ctx, operationID, hostRequest)
	if err != nil {
		return nil, r.handlerOperationError(ctx, handlerID, err)
	}
	completed, err := started.Wait(ctx, nil)
	if err != nil {
		var cancellationErr error
		if ctx.Err() != nil {
			cancellationErr = r.cancelOperation(context.WithoutCancel(ctx), operationID)
		}
		if isConnectionFailure(err) || isConnectionFailure(cancellationErr) {
			_ = r.Close()
		}
		return nil, errors.Join(r.handlerOperationError(ctx, handlerID, err), cancellationErr)
	}
	response := completed.GetHandle()
	if response == nil {
		return nil, r.protocolViolation(errors.New("compaction handler response is missing"))
	}
	if handlerErr := response.GetError(); handlerErr != nil {
		return nil, newOrdinaryHandlerError(handlerErr.GetMessage())
	}
	return response, nil
}

// mapCompactionRequestInvocation projects immutable and current request state.
func mapCompactionRequestInvocation(
	invocation contextcompaction.RequestInvocation,
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
	invocation contextcompaction.ResultInvocation,
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
func mapCompactionRequest(request contextcompaction.Request) (*extensionpb.CompactionRequest, error) {
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
		previous = mapCompactionMarker(value)
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
func mapCompactionEntries(entries []contextcompaction.InputEntry) ([]*extensionpb.CompactionInputEntry, error) {
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
func mapCompactionResult(result contextcompaction.Result) *extensionpb.CompactionResult {
	builder := extensionpb.CompactionResult_builder{
		Summary: new(result.Summary), FirstKeptEntryId: new(result.FirstKeptEntryID),
		Source: mapSummarySource(result.Source), Details: nil,
	}
	if details, present := result.Details.Get(); present {
		builder.Details = append([]byte(nil), details...)
	}
	return builder.Build()
}

// mapCompactionResultFromProto retains malformed values for Host validation.
func mapCompactionResultFromProto(value *extensionpb.CompactionResult) contextcompaction.Result {
	result := contextcompaction.Result{
		Summary: value.GetSummary(), FirstKeptEntryID: value.GetFirstKeptEntryId(),
		Source: mapSummarySourceFromProto(value.GetSource()), Details: mo.None[[]byte](),
	}
	if value.HasDetails() {
		result.Details = mo.Some(append([]byte(nil), value.GetDetails()...))
	}
	return result
}

// mapCompactionRequestAction maps explicit public action variants without repairing malformed shapes.
func mapCompactionRequestAction(
	action *extensionpb.CompactionRequestAction,
) (contextcompaction.RequestAction, error) {
	result := contextcompaction.RequestAction{
		Cancel:        action.GetCancel(),
		RequestAction: mapCompactionRequestDisposition(action.GetRequestAction()),
		Request:       mo.None[contextcompaction.Request](),
		ResultAction:  mapCompactionResultDisposition(action.GetResultAction()),
		Result:        mo.None[contextcompaction.Result](),
	}
	if action.GetRequest() != nil {
		replacement, err := mapCompactionRequestFromProto(action.GetRequest())
		if err != nil {
			return contextcompaction.RequestAction{}, err
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
) (contextcompaction.ResultAction, error) {
	disposition := action.GetResultAction()
	if action.GetCancel() {
		if disposition != extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED ||
			action.GetResult() != nil {
			return contextcompaction.ResultAction{}, errors.New("compaction cancellation must be exclusive")
		}
		return contextcompaction.ResultAction{
			Cancel: true, Preserve: false, Result: mo.None[contextcompaction.Result](),
		}, nil
	}
	if disposition != extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE &&
		disposition != extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE {
		return contextcompaction.ResultAction{}, errors.New("compaction result disposition is invalid")
	}
	result := contextcompaction.ResultAction{
		Cancel:   false,
		Preserve: disposition == extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE,
		Result:   mo.None[contextcompaction.Result](),
	}
	if action.GetResult() != nil {
		result.Result = mo.Some(mapCompactionResultFromProto(action.GetResult()))
	}
	return result, nil
}

// mapCompactionOutcome projects terminal state and optional durable commit.
func mapCompactionOutcome(invocation contextcompaction.OutcomeInvocation) (*extensionpb.CompactionOutcome, error) {
	original, err := mapCompactionRequest(invocation.OriginalRequest)
	if err != nil {
		return nil, err
	}
	current, err := mapCompactionRequest(invocation.CurrentRequest)
	if err != nil {
		return nil, err
	}
	builder := extensionpb.CompactionOutcome_builder{
		OriginalRequest: original, CurrentRequest: current, Result: nil, Committed: nil,
		Canceled: new(invocation.Canceled), Error: nil,
	}
	if result, present := invocation.Result.Get(); present {
		builder.Result = mapCompactionResult(result)
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

// mapCompactionRequestFromProto maps replacement state and rejects malformed public entry values.
func mapCompactionRequestFromProto(value *extensionpb.CompactionRequest) (contextcompaction.Request, error) {
	if !value.HasContextTokensEstimated() || !value.GetContextTokensEstimated() {
		return contextcompaction.Request{}, errors.New(
			"compaction context token estimate marker must be present and true",
		)
	}
	prefix, err := mapCompactionEntriesFromProto(value.GetPrefix())
	if err != nil {
		return contextcompaction.Request{}, err
	}
	suffix, err := mapCompactionEntriesFromProto(value.GetSuffix())
	if err != nil {
		return contextcompaction.Request{}, err
	}
	return contextcompaction.Request{
		Trigger:      mapCompactionTriggerFromProto(value.GetTrigger()),
		RetryIntent:  value.GetRetryIntent(),
		Instructions: optionalString(value.HasInstructions(), value.GetInstructions()),
		Model: mapCompactionModelFromProto(
			value.GetModel(),
		),
		ReasoningChoice:        model.ReasoningChoice(value.GetReasoningChoice()),
		Prefix:                 prefix,
		Suffix:                 suffix,
		Previous:               mapOptionalCompactionMarker(value.GetPrevious()),
		ContextTokens:          value.GetContextTokens(),
		ContextTokensEstimated: value.GetContextTokensEstimated(),
		ContextWindow:          value.GetContextWindow(),
		ResponseBudget:         value.GetResponseBudget(),
		RetainedBudget:         value.GetRetainedBudget(),
	}, nil
}

// mapCompactionEntriesFromProto maps replacement entries through the existing validated public mapper.
func mapCompactionEntriesFromProto(values []*extensionpb.CompactionInputEntry) ([]contextcompaction.InputEntry, error) {
	entries := make([]contextcompaction.InputEntry, len(values))
	for index := range values {
		if values[index] == nil || values[index].GetEntry() == nil {
			return nil, fmt.Errorf("map replacement compaction entry %d: payload is missing", index)
		}
		entry, err := mapSessionEntryFromProto(values[index].GetEntry())
		if err != nil {
			return nil, fmt.Errorf("map replacement compaction entry %d: %w", index, err)
		}
		entries[index] = contextcompaction.InputEntry{
			Entry: entry, EstimatedTokens: values[index].GetEstimatedTokens(),
		}
	}
	return entries, nil
}

// mapCompactionRequestDisposition maps the closed public request action.
func mapCompactionRequestDisposition(
	value extensionpb.CompactionRequestDisposition,
) contextcompaction.RequestActionKind {
	switch value {
	case extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE:
		return contextcompaction.RequestActionPreserve
	case extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE:
		return contextcompaction.RequestActionReplace
	case extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// mapCompactionResultDisposition maps the closed public result action.
func mapCompactionResultDisposition(
	value extensionpb.CompactionResultDisposition,
) contextcompaction.ResultActionKind {
	switch value {
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE:
		return contextcompaction.ResultActionPreserve
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE:
		return contextcompaction.ResultActionReplace
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR:
		return contextcompaction.ResultActionClear
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// mapCompactionTriggerFromProto maps the closed public trigger.
func mapCompactionTriggerFromProto(value extensionpb.CompactionTrigger) contextcompaction.Trigger {
	switch value {
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL:
		return contextcompaction.TriggerManual
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_THRESHOLD:
		return contextcompaction.TriggerThreshold
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW:
		return contextcompaction.TriggerOverflow
	case extensionpb.CompactionTrigger_COMPACTION_TRIGGER_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// mapCompactionTrigger maps the closed internal trigger.
func mapCompactionTrigger(trigger contextcompaction.Trigger) extensionpb.CompactionTrigger {
	return extensionpb.CompactionTrigger(trigger)
}

// optionalString preserves public scalar presence.
func optionalString(present bool, value string) mo.Option[string] {
	if !present {
		return mo.None[string]()
	}
	return mo.Some(value)
}
