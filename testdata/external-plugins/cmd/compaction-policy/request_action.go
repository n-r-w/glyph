package main

import (
	"context"
	"errors"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// validateReferenceRequestAction checks the shape of a participant action.
func validateReferenceRequestAction(action *extensionpb.CompactionRequestAction) error {
	if action.GetCancel() {
		if action.GetRequestAction() != extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_UNSPECIFIED ||
			action.GetResultAction() != extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED ||
			action.GetRequest() != nil ||
			action.GetResult() != nil {
			return errors.New("invalid reference request action: cancellation is exclusive")
		}
		return nil
	}
	if err := validateReferenceRequestChange(action); err != nil {
		return err
	}
	return validateReferenceResultChange(action)
}

// validateReferenceRequestChange checks the independent current-request action.
func validateReferenceRequestChange(action *extensionpb.CompactionRequestAction) error {
	switch action.GetRequestAction() {
	case extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE:
		if action.GetRequest() != nil {
			return errors.New("invalid reference request action: preserve has a replacement")
		}
	case extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE:
		if action.GetRequest() == nil {
			return errors.New("invalid reference request action: replacement is missing")
		}
	case extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_UNSPECIFIED:
		return errors.New("invalid reference request action: request disposition is missing")
	default:
		return errors.New("invalid reference request action: unknown request disposition")
	}
	return nil
}

// validateReferenceResultChange checks the independent optional-result action.
func validateReferenceResultChange(action *extensionpb.CompactionRequestAction) error {
	switch action.GetResultAction() {
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE,
		extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR:
		if action.GetResult() != nil {
			return errors.New("invalid reference request action: result disposition has a replacement")
		}
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE:
		if action.GetResult() == nil || action.GetResult().GetSummary() == "" || action.GetResult().GetSource() == nil {
			return errors.New("invalid reference request action: complete result is required")
		}
	case extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED:
		return errors.New("invalid reference request action: result disposition is missing")
	default:
		return errors.New("invalid reference request action: unknown result disposition")
	}
	return nil
}

// acceptRequestAction installs a structurally accepted combined update.
func (o *policyOperation) acceptRequestAction(ctx context.Context, action *extensionpb.CompactionRequestAction) error {
	// Accept a combined update before installing either proposed value.
	if action.GetResultAction() == extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE {
		if err := projectPolicyReplacement(ctx, o.policy, action.GetResult()); err != nil {
			return err
		}
	}
	if action.GetRequestAction() == extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE {
		o.state.SetCurrentRequest(action.GetRequest())
	}
	if action.GetResultAction() == extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE {
		o.state.SetResult(action.GetResult())
	}
	if action.GetResultAction() == extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR {
		o.state.ClearResult()
	}
	return nil
}
