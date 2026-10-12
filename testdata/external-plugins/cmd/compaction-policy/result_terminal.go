package main

import (
	"context"
	"errors"
	"os"

	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// referenceResultOutsideBranch identifies a boundary outside the captured branch.
	referenceResultOutsideBranch = "outside-result-branch"
	// referenceResultForgedProducer identifies attribution to another extension.
	referenceResultForgedProducer = "result-forged-producer"
	// referenceResultUnknownDisposition is an unknown public action value.
	referenceResultUnknownDisposition extensionpb.CompactionResultDisposition = 99
	// referenceResultCancel is the mode for exclusive cancellation.
	referenceResultCancel = "cancel"
	// referenceResultMixed is the mode for cancellation with a PRESERVE disposition.
	referenceResultMixed = "mixed"
	// referenceResultClear is the mode for an invalid clearing action.
	referenceResultClear = "clear"
	// referenceResultMissing is the mode for replacement without a candidate.
	referenceResultMissing = "missing"
	// referenceResultUnexpected is the mode for preservation with a candidate.
	referenceResultUnexpected = "unexpected"
	// referenceResultInvalid is the mode for an unknown disposition.
	referenceResultInvalid = "invalid"
	// referenceResultBoundary is the mode for an out-of-branch replacement.
	referenceResultBoundary = "boundary"
	// referenceResultProducer is the mode for attribution to another extension.
	referenceResultProducer = "producer"
	// referenceResultFailure is the mode for a participant failure.
	referenceResultFailure = "failure"
	// referenceResultCanceled is the mode for an unclassified canceled invocation.
	referenceResultCanceled = "canceled"
	// referenceResultClassified is the mode for cancellation with an independent failure category.
	referenceResultClassified = "classified"
	// referenceResultFailureCause identifies an independent participant failure.
	referenceResultFailureCause = "result participant source failure"
)

// runTerminalAction supplies terminal participant behavior for public regression tests.
func (o *referenceResultOperation) runTerminalAction(
	ctx context.Context,
	binding *extensionsdk.ExtensionContext,
	action *extensionpb.CompactionResultAction,
) (*extensionpb.CompactionResultAction, error) {
	mode := os.Getenv(referenceResultModeEnvironment)
	switch mode {
	case referenceGeneratorBlockedModel:
		return action, o.runNestedResultWork(ctx, binding)
	case referenceResultCancel:
		action.SetCancel(true)
		action.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED)
	case referenceResultMixed:
		action.SetCancel(true)
	case referenceResultClear:
		action.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR)
	case referenceResultMissing:
		action.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE)
	case referenceResultUnexpected:
		action.SetResult(proto.CloneOf(o.invocation.GetCurrentResult()))
	case referenceResultInvalid:
		action.SetResultAction(referenceResultUnknownDisposition)
	case referenceResultBoundary, referenceResultProducer:
		candidate := proto.CloneOf(o.invocation.GetCurrentResult())
		if mode == referenceResultBoundary {
			candidate.SetFirstKeptEntryId(referenceResultOutsideBranch)
		} else {
			candidate.GetSource().SetExtensionId(referenceResultForgedProducer)
		}
		action.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE)
		action.SetResult(candidate)
	case referenceResultOriginal:
		candidate := proto.CloneOf(o.invocation.GetOriginalResult())
		candidate.SetSummary(referenceResultOriginalSummary)
		action.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE)
		action.SetResult(candidate)
	case referenceResultFailure:
		return nil, errors.New(referenceResultFailureCause)
	case referenceResultCanceled:
		return nil, context.Canceled
	case referenceResultClassified:
		return nil, extensionsdk.Fail(
			referenceGeneratorFailureCode,
			errors.Join(context.Canceled, errors.New(referenceResultFailureCause)),
		)
	}
	return action, nil
}
