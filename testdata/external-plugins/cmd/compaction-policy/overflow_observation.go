package main

import (
	"context"
	"errors"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// conversationOverflowEnvironment is the environment variable name for the compaction-request replacement fixture flag.
const conversationOverflowEnvironment = "GLYPH_REFERENCE_CONVERSATION_OVERFLOW"

// overflowObservationSummary identifies the replacement fixture's model-visible text.
const overflowObservationSummary = "conversation overflow replacement summary"

// overflowObservationMessage is the message text for the compaction observer's committed append.
const overflowObservationMessage = "conversation overflow observer message"

// completeOverflowReplacement creates the complete result for the compaction request's retained suffix.
func (o *referenceRequestOperation) completeOverflowReplacement(
	binding *extensionsdk.ExtensionContext,
) (*extensionpb.HandleResponse, error) {
	suffix := o.invocation.GetCurrent().GetSuffix()
	if len(suffix) == 0 {
		return nil, errors.New("overflow fixture requires a retained suffix")
	}
	action := newReferencePreserveAction()
	action.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE)
	action.SetResult(extensionpb.CompactionResult_builder{
		Summary:          new(overflowObservationSummary),
		FirstKeptEntryId: new(suffix[0].GetSourceEntryId()),
		Source: extensionpb.CompactionSource_builder{
			ExtensionId:   new(binding.Identity().GetExtensionId()),
			ModelRequests: nil,
		}.Build(),
		Details: []byte{},
	}.Build())
	response := new(extensionpb.HandleResponse)
	response.SetCompactionRequest(action)
	return response, nil
}

// appendOverflowObservationMessage appends a model-visible message after durable replacement.
func appendOverflowObservationMessage(ctx context.Context) error {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return err
	}
	const entryType = "overflow-observation-message"
	operation, err := binding.StartAppendExtensionMessage(ctx, extensionpb.AppendExtensionMessageRequest_builder{
		Context: nil, EntryType: new(entryType), Text: new(overflowObservationMessage),
		Visibility: new(extensionpb.ClientVisibility_CLIENT_VISIBILITY_VISIBLE),
	}.Build())
	if err != nil {
		return err
	}
	_, err = operation.Wait(ctx)
	return err
}
