// Package main provides the reference compaction policy fixture.
package main

import (
	"context"
	"errors"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// Run executes one reference manual policy invocation.
func (o *policyOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	const (
		referenceSummary = "Reference policy replacement summary."
		cancelGuidance   = "cancel-before-commit"
		staleGuidance    = "change-branch-before-commit"
		waitGuidance     = "wait-for-caller-cancel"
	)

	input := o.policy.Input()
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
		response := new(extensionpb.HandleResponse)
		response.SetCompactionPolicy(
			extensionpb.CompactionPolicyResult_builder{State: nil, Error: nil, Canceled: new(true)}.Build(),
		)
		return o.attachAcquiredState(response), nil
	}
	boundary := findPolicyBoundary(input.GetEntries())
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
	o.state = newReferencePolicyState(input, boundary)
	o.state.SetResult(candidate)
	return o.replaceHistory(ctx, candidate, input.GetUserInstructions())
}
