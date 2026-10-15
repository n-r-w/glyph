// Package main provides the reference compaction policy fixture.
package main

import (
	"context"
	"errors"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// Run selects the reference action for the captured invocation.
func (o *policyOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	input := o.policy.Input()
	switch input.GetPurpose() {
	case extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC:
		return o.runAutomatic(ctx, input)
	case extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_MANUAL:
		return o.runReplacement(ctx, input)
	case extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW:
		return o.runOverflow(ctx, input)
	case extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_UNSPECIFIED:
		return newPolicyFailure(errors.New("reference policy purpose is unspecified or unknown")), nil
	default:
		return newPolicyFailure(errors.New("reference policy purpose is unspecified or unknown")), nil
	}
}

// runReplacement commits the reference candidate through scoped authority.
func (o *policyOperation) runReplacement(
	ctx context.Context,
	input *extensionpb.CompactionPolicyInvocation,
) (*extensionpb.HandleResponse, error) {
	const (
		referenceSummary = "Reference policy replacement summary."
		cancelGuidance   = "cancel-before-commit"
		staleGuidance    = "change-branch-before-commit"
		waitGuidance     = policyWaitBeforeCommit
	)
	const admissionProbeGuidance = "request-after-parent-cancellation"
	if input.GetUserInstructions() == admissionProbeGuidance {
		return requestCanceledParentAdmission(ctx)
	}
	const policyInputEnvironment = "GLYPH_REFERENCE_POLICY_INPUT"
	if err := writePolicyArtifact(os.Getenv(policyInputEnvironment), input); err != nil {
		return o.attachAcquiredState(newPolicyFailure(err)), nil
	}
	if isReferenceRequestGuidance(input.GetUserInstructions()) {
		return o.runRequestPolicy(ctx, input)
	}
	if input.GetUserInstructions() == cancelGuidance {
		response := newPolicyDecisionResponse(
			extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_CANCELED,
		)
		return o.attachAcquiredState(response), nil
	}
	boundary := findReplacementBoundary(input)
	if boundary == "" {
		return o.attachAcquiredState(
			newPolicyFailure(errors.New("reference policy requires an original user entry")),
		), nil
	}
	contextBinding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return o.attachAcquiredState(newPolicyFailure(err)), nil
	}
	if rejectionErr := handlePolicySelectionRejection(
		ctx, contextBinding, input.GetUserInstructions(), input.GetModel().GetProviderId(),
	); rejectionErr != nil {
		return o.attachAcquiredState(newPolicyFailure(rejectionErr)), nil
	}
	if input.GetUserInstructions() == waitGuidance {
		if modelErr := waitPolicyConfiguredModel(
			ctx,
			contextBinding,
			input.GetModel(),
			input.GetReasoningChoice(),
		); modelErr != nil {
			return o.attachAcquiredState(newPolicyFailure(modelErr)), nil
		}
	}
	source := new(extensionpb.BranchSummarySource)
	source.SetExtensionId(contextBinding.Identity().GetExtensionId())
	if input.GetUserInstructions() == staleGuidance {
		if appendErr := appendPolicyBranchChange(ctx, contextBinding); appendErr != nil {
			return o.attachAcquiredState(newPolicyFailure(appendErr)), nil
		}
	}
	candidate := extensionpb.CompactionResult_builder{
		Summary:          new(referenceSummary),
		FirstKeptEntryId: new(boundary),
		Source:           source,
		Details:          nil,
	}.Build()
	if os.Getenv(referenceCandidateRejectionEnvironment) == referenceCandidateDirect {
		candidate.SetFirstKeptEntryId(referenceGeneratorOutsideBranch)
	}
	o.state = newReferencePolicyState(input, boundary)
	if projectionErr := projectPolicyReplacement(ctx, o.policy, candidate); projectionErr != nil {
		return o.attachAcquiredState(newPolicyFailure(projectionErr)), nil
	}
	o.state.SetResult(candidate)
	return o.replaceHistory(ctx, input.GetUserInstructions())
}
