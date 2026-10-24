// Package main provides the reference compaction policy fixture.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// runRequestPolicy supplies the policy outcome for request-handler composition.
func (o *policyOperation) runRequestPolicy(
	ctx context.Context,
	input *extensionpb.CompactionPolicyInvocation,
) (*extensionpb.HandleResponse, error) {
	boundary := findPolicyBoundary(input)
	if boundary == "" {
		return o.attachAcquiredState(
			newPolicyFailure(errors.New("reference policy requires an original user entry")),
		), nil
	}
	canceled, err := o.composeRequests(ctx, input, boundary)
	if err != nil {
		if os.Getenv(consumedInvalidOutputEnvironment) != "" {
			return newPolicyDecisionResponse(
				extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_CANCELED,
			), nil
		}
		return o.attachAcquiredState(newPolicyFailure(err)), nil
	}
	if canceled {
		response := newPolicyDecisionResponse(
			extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_CANCELED,
		)
		return o.attachAcquiredState(response), nil
	}
	return o.replaceHistory(ctx, input.GetUserInstructions())
}

// replaceHistory requests the durable replacement selected by the policy.
func (o *policyOperation) replaceHistory(
	ctx context.Context,
	guidance string,
) (*extensionpb.HandleResponse, error) {
	canceled, composeErr := o.composeResults(ctx)
	if composeErr != nil {
		if os.Getenv(consumedInvalidOutputEnvironment) != "" {
			return newPolicyDecisionResponse(
				extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_CANCELED,
			), nil
		}
		return o.attachAcquiredState(newPolicyFailure(composeErr)), nil
	}
	if canceled {
		response := newPolicyDecisionResponse(
			extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_CANCELED,
		)
		return o.attachAcquiredState(response), nil
	}
	replacement, err := o.policy.StartReplaceHistory(ctx, o.state.GetResult())
	if err != nil {
		return o.attachAcquiredState(newPolicyFailure(fmt.Errorf("start reference replacement: %w", err))), nil
	}
	receipt, err := replacement.Wait(ctx)
	if err != nil {
		return o.attachAcquiredState(newPolicyFailure(fmt.Errorf("await reference replacement: %w", err))), nil
	}
	if receipt.GetError() != "" {
		return o.attachAcquiredState(newPolicyFailure(errors.New(receipt.GetError()))), nil
	}
	return o.completeReplacement(guidance), nil
}
