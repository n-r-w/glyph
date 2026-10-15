package main

import (
	"context"
	"errors"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// runOverflow demonstrates policy-owned recovery through captured composition participants.
func (o *policyOperation) runOverflow(
	ctx context.Context,
	input *extensionpb.CompactionPolicyInvocation,
) (*extensionpb.HandleResponse, error) {
	const policyInputEnvironment = "GLYPH_REFERENCE_POLICY_INPUT"
	if !input.HasOverflowError() {
		return newPolicyFailure(errors.New("reference overflow policy requires the rejected provider error")), nil
	}
	if err := writePolicyArtifact(os.Getenv(policyInputEnvironment), input); err != nil {
		return newPolicyFailure(err), nil
	}
	return o.runRequestPolicy(ctx, input)
}
