package main

import (
	"context"
	"errors"

	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// composeResults applies accepted participant actions to policy-owned state.
func (o *policyOperation) composeResults(ctx context.Context) (bool, error) {
	original := proto.CloneOf(o.state.GetResult())
	for _, participant := range o.policy.Input().GetResultParticipants() {
		invocation := extensionpb.CompactionResultInvocation_builder{
			OriginalRequest: o.state.GetOriginalRequest(),
			CurrentRequest:  o.state.GetCurrentRequest(),
			OriginalResult:  original,
			CurrentResult:   o.state.GetResult(),
		}.Build()
		operation, err := o.policy.StartResultHandler(ctx, participant, invocation)
		if err != nil {
			return false, err
		}
		action, err := operation.Wait(ctx)
		if err != nil {
			if _, canceled := errors.AsType[*extensionsdk.CanceledError](err); canceled && ctx.Err() == nil {
				return true, nil
			}
			return false, err
		}
		if actionErr := validateReferenceResultAction(action); actionErr != nil {
			return false, actionErr
		}
		if action.GetCancel() {
			return true, nil
		}
		if action.GetResultAction() == extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE {
			if projectionErr := projectPolicyReplacement(ctx, o.policy, action.GetResult()); projectionErr != nil {
				return false, projectionErr
			}
			o.state.SetResult(proto.CloneOf(action.GetResult()))
		}
	}
	return false, nil
}

// validateReferenceResultAction validates a participant's proposed result action.
func validateReferenceResultAction(action *extensionpb.CompactionResultAction) error {
	if action == nil {
		return errors.New("result action is required")
	}
	if action.GetCancel() {
		if action.GetResultAction() != extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED ||
			action.GetResult() != nil {
			return errors.New("compaction cancellation must be exclusive")
		}
		return nil
	}
	switch action.GetResultAction() {
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE:
		if action.GetResult() != nil {
			return errors.New("preserving result cannot supply a replacement")
		}
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE:
		if action.GetResult() == nil {
			return errors.New("replacing result requires a candidate")
		}
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED,
		extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR:
		return errors.New("compaction result disposition is invalid")
	default:
		return errors.New("compaction result disposition is invalid")
	}
	return nil
}
