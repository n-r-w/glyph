// Package main provides the reference compaction policy fixture.
package main

import (
	"context"
	"errors"
	"fmt"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// runRequestPolicy supplies the policy outcome for request-handler composition.
func (o *policyOperation) runRequestPolicy(
	ctx context.Context,
	input *extensionpb.CompactionPolicyInvocation,
) (*extensionpb.HandleResponse, error) {
	boundary := findPolicyBoundary(input.GetEntries())
	if boundary == "" {
		return o.attachAcquiredState(
			newPolicyFailure(errors.New("reference policy requires an original user entry")),
		), nil
	}
	result, canceled, err := o.composeRequests(ctx, input, boundary)
	if err != nil {
		return o.attachAcquiredState(newPolicyFailure(err)), nil
	}
	if canceled {
		response := new(extensionpb.HandleResponse)
		response.SetCompactionPolicy(
			extensionpb.CompactionPolicyResult_builder{Canceled: new(true), State: nil, Error: nil}.Build(),
		)
		return o.attachAcquiredState(response), nil
	}
	return o.replaceHistory(ctx, result, input.GetUserInstructions())
}

// replaceHistory requests the durable replacement selected by the policy.
func (o *policyOperation) replaceHistory(
	ctx context.Context,
	candidate *extensionpb.CompactionResult,
	guidance string,
) (*extensionpb.HandleResponse, error) {
	if projectionErr := projectPolicyReplacement(ctx, o.policy, candidate); projectionErr != nil {
		return o.attachAcquiredState(newPolicyFailure(projectionErr)), nil
	}
	replacement, err := o.policy.StartReplaceHistory(ctx, candidate)
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
