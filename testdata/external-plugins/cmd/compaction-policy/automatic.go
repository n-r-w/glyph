package main

import (
	"context"
	"fmt"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// automaticModeEnvironment names the environment variable for the automatic scenario.
	automaticModeEnvironment = "GLYPH_REFERENCE_AUTOMATIC_MODE"
	// policyWaitBeforeCommit names the scenario where the policy pauses preparation in a configured-model request.
	policyWaitBeforeCommit = "wait-for-caller-cancel"
	// policyLoseAfterCommit names the scenario where the policy process exits before acknowledging its receipt.
	policyLoseAfterCommit = "lose-state-after-commit"
	// automaticRejectFinal names the scenario where the outgoing-input handler rejects history
	// captured after outcome notifications.
	automaticRejectFinal = "reject-final"
)

// automaticReplacementSelected reports whether the fixture should replace context.
func automaticReplacementSelected(input *extensionpb.CompactionPolicyInvocation) bool {
	const automaticTargetEnvironment = "GLYPH_REFERENCE_AUTOMATIC_TARGET"
	const preserveMode = "preserve"
	if mode := os.Getenv(automaticModeEnvironment); mode == "" || mode == preserveMode {
		return false
	}
	target := os.Getenv(automaticTargetEnvironment)
	if target == "" {
		return true
	}
	history := input.GetHistory()
	if len(history) == 0 {
		return false
	}
	for _, content := range history[len(history)-1].GetUser().GetContent() {
		if content.GetText() == target {
			return true
		}
	}
	return false
}

// runAutomatic selects the fixture's preparation decision and composition.
func (o *policyOperation) runAutomatic(
	ctx context.Context,
	input *extensionpb.CompactionPolicyInvocation,
) (*extensionpb.HandleResponse, error) {
	const (
		// automaticUnspecified names the scenario where the policy omits a completed decision.
		automaticUnspecified = "unspecified"
		// automaticReplacedWithoutReceipt names the scenario where the policy acknowledges
		// a replacement it never requested.
		automaticReplacedWithoutReceipt = "replaced-without-receipt"
		// automaticCanceledBefore names the scenario where the policy ends preparation before mutation.
		automaticCanceledBefore = "canceled-before"
		// automaticCanceledDiagnostic selects acquired cancellation beside an independent failure.
		automaticCanceledDiagnostic    = "canceled-diagnostic"
		diagnosticCause                = "independent policy diagnostic"
		automaticInvocationEnvironment = "GLYPH_REFERENCE_AUTOMATIC_INPUT"
	)
	if err := writePolicyArtifact(os.Getenv(automaticInvocationEnvironment), input); err != nil {
		return newPolicyFailure(err), nil
	}
	if !automaticReplacementSelected(input) {
		return newPolicyDecisionResponse(
			extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_PRESERVE,
		), nil
	}
	mode := os.Getenv(automaticModeEnvironment)
	switch mode {
	case automaticUnspecified:
		return newPolicyDecisionResponse(
			extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_UNSPECIFIED,
		), nil
	case automaticReplacedWithoutReceipt:
		return newPolicyDecisionResponse(
			extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_REPLACED,
		), nil
	case automaticCanceledBefore:
		return newPolicyDecisionResponse(
			extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_CANCELED,
		), nil
	case automaticCanceledDiagnostic:
		response := newPolicyDecisionResponse(
			extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_CANCELED,
		)
		response.GetCompactionPolicy().SetError(diagnosticCause)
		return response, nil
	case policyWaitBeforeCommit:
		return o.runWaitingAutomatic(ctx, input)
	case policyLoseAfterCommit:
		input.SetUserInstructions(mode)
		return o.runReplacement(ctx, input)
	}
	if isReferenceRequestGuidance(mode) {
		input.SetUserInstructions(mode)
		return o.runRequestPolicy(ctx, input)
	}
	return o.runReplacement(ctx, input)
}

// runWaitingAutomatic holds preparation in its lifecycle-owned configured request.
func (o *policyOperation) runWaitingAutomatic(
	ctx context.Context,
	input *extensionpb.CompactionPolicyInvocation,
) (*extensionpb.HandleResponse, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	if modelErr := waitPolicyConfiguredModel(
		ctx, binding, input.GetModel(), input.GetReasoningChoice(),
	); modelErr != nil {
		return nil, modelErr
	}
	return o.runReplacement(ctx, input)
}

// findReplacementBoundary selects the fixture's retained branch position.
func findReplacementBoundary(input *extensionpb.CompactionPolicyInvocation) string {
	// automaticSplitBoundary names the scenario where the policy proposes a structurally invalid replacement.
	const automaticSplitBoundary = "split-boundary"
	if input.GetPurpose() == extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC &&
		os.Getenv(automaticModeEnvironment) == automaticSplitBoundary {
		for _, entry := range input.GetEntries() {
			if entry.GetEntry().GetToolResult() != nil {
				return entry.GetEntry().GetId()
			}
		}
	}
	return findPolicyBoundary(input.GetEntries())
}

// newPolicyDecisionResponse supplies a typed completion without acquired composition state.
func newPolicyDecisionResponse(disposition extensionpb.CompactionPolicyDisposition) *extensionpb.HandleResponse {
	response := new(extensionpb.HandleResponse)
	response.SetCompactionPolicy(extensionpb.CompactionPolicyResult_builder{
		State: nil, Error: nil, Disposition: disposition.Enum(),
	}.Build())
	return response
}

// outgoingInputOperation reports the actual frozen provider input.
type outgoingInputOperation struct {
	// input contains final captured history and conversation binding.
	input *extensionpb.CompactionOutgoingInput
}

// outgoingInputOperation supplies the SDK callback contract for final outgoing-input validation.
var _ extensionsdk.HandleOperation = (*outgoingInputOperation)(nil)

// Run acknowledges or rejects the final input supplied by Host.
func (o *outgoingInputOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	const (
		// automaticHoldFinal names the scenario where the outgoing-input handler pauses validation
		// in a configured-model request.
		automaticHoldFinal = "hold-final"
		// automaticAppendFinal names the scenario where the outgoing-input handler adds visible context
		// during final validation.
		automaticAppendFinal = "append-final"
		// automaticFinalAppend is context added after the outgoing capture.
		automaticFinalAppend         = "later final validation append"
		automaticOutgoingEnvironment = "GLYPH_REFERENCE_AUTOMATIC_OUTGOING"
		// automaticFinalCause is the error text for rejection of outgoing input.
		automaticFinalCause = "reference policy rejected actual outgoing input: " +
			"observer growth exceeds captured conversation budget"
	)
	if err := writePolicyArtifact(os.Getenv(automaticOutgoingEnvironment), o.input); err != nil {
		return nil, err
	}
	if os.Getenv(automaticModeEnvironment) == automaticHoldFinal {
		binding, err := extensionsdk.ContextFrom(ctx)
		if err != nil {
			return nil, err
		}
		if modelErr := waitPolicyConfiguredModel(
			ctx, binding, o.input.GetModel(), o.input.GetReasoningChoice(),
		); modelErr != nil {
			return nil, modelErr
		}
	}
	if os.Getenv(automaticModeEnvironment) == automaticAppendFinal {
		if err := appendAutomaticMessage(ctx, automaticFinalAppend); err != nil {
			return nil, err
		}
	}
	if os.Getenv(automaticModeEnvironment) == automaticRejectFinal {
		const textBytesPerToken int64 = 4
		const budgetFailureFormat = "%s: input=%d, budget=%d"
		bytes := int64(len(o.input.GetInstructions()))
		for _, record := range o.input.GetHistory() {
			for _, content := range record.GetUser().GetContent() {
				bytes += int64(len(content.GetText()))
			}
		}
		estimate := (bytes + textBytesPerToken - 1) / textBytesPerToken
		budget := o.input.GetModel().GetContextWindow() - o.input.GetModel().GetMaxTokens()
		if estimate > budget {
			response := new(extensionpb.HandleResponse)
			response.SetError(extensionpb.HandlerError_builder{
				Message: new(fmt.Sprintf(budgetFailureFormat, automaticFinalCause, estimate, budget)),
			}.Build())
			return response, nil
		}
	}
	response := new(extensionpb.HandleResponse)
	response.SetCompactionOutgoingInput(new(extensionpb.CompactionObserverAction))
	return response, nil
}

// Release is the SDK lifecycle callback for the outgoing-input handler.
func (*outgoingInputOperation) Release() {}

// appendAutomaticMessage supplies visible session growth during preparation.
func appendAutomaticMessage(ctx context.Context, message string) error {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return err
	}
	const entryType = "automatic-observer"
	operation, err := binding.StartAppendExtensionMessage(ctx, extensionpb.AppendExtensionMessageRequest_builder{
		Context: nil, EntryType: new(entryType), Text: new(message),
		Visibility: extensionpb.ClientVisibility_CLIENT_VISIBILITY_VISIBLE.Enum(),
	}.Build())
	if err != nil {
		return err
	}
	_, err = operation.Wait(ctx)
	return err
}
