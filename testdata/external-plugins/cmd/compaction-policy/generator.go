package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// referenceGeneratorReasoningChoice is the reasoning choice for configured-model fixtures.
	referenceGeneratorReasoningChoice = "high"
	// referenceGeneratorExecutable identifies the generator-only fixture process.
	referenceGeneratorExecutable = "reference-generator"
	// referenceSeparateGeneratorEnvironment selects a distinct generator process.
	referenceSeparateGeneratorEnvironment = "GLYPH_REFERENCE_SEPARATE_GENERATOR"
	// referenceGeneratorsEnvironment selects fixture generator registration.
	referenceGeneratorsEnvironment = "GLYPH_REFERENCE_GENERATORS"
	// referenceGeneratorModeEnvironment selects a fixture terminal outcome.
	referenceGeneratorModeEnvironment = "GLYPH_REFERENCE_GENERATOR_MODE"
	// referenceGeneratorFailureCode identifies classified extension failures.
	referenceGeneratorFailureCode = "INTERNAL"
	// referenceGeneratorMultipleRegistrations selects an ambiguous generator catalog.
	referenceGeneratorMultipleRegistrations = "2"
	// referenceGeneratorFalseProducer identifies the deliberately incorrect result source.
	referenceGeneratorFalseProducer = "forged-generator-extension"
	// referenceGeneratorSummary is the fixture's replacement summary text.
	referenceGeneratorSummary = "Reference generator replacement summary."
	// referenceGeneratorID identifies the captured fixture capability.
	referenceGeneratorID = "reference-generator"
	// referenceGeneratorSecondID identifies an ambiguous fixture registration.
	referenceGeneratorSecondID = "reference-generator-second"
	// referenceGeneratorCancel selects explicit cancellation.
	referenceGeneratorCancel = "cancel"
	// referenceGeneratorMixedCancel selects cancellation with an independent cause.
	referenceGeneratorMixedCancel = "mixed-cancel"
	// referenceGeneratorClassifiedCancel selects a classified cancellation cause.
	referenceGeneratorClassifiedCancel = "classified-cancel"
	// referenceGeneratorBlockedModel selects participant-owned blocked provider work.
	referenceGeneratorBlockedModel = "blocked-model"
	// referenceGeneratorImages selects configured-model image input.
	referenceGeneratorImages = "images"
	// referenceGeneratorInvalidBoundary selects an out-of-branch candidate.
	referenceGeneratorInvalidBoundary = "invalid-boundary"
	// referenceGeneratorOutsideBranch identifies a boundary absent from the captured branch.
	referenceGeneratorOutsideBranch = "reference-generator-outside-branch"
	// referenceGeneratorForgedProducer selects a false producer identity.
	referenceGeneratorForgedProducer = "forged-producer"
)

// newReferenceGeneratorDescriptors supplies fixture generator capabilities.
func newReferenceGeneratorDescriptors() []*extensionpb.HandlerDescriptor {
	count := os.Getenv(referenceGeneratorsEnvironment)
	if count != fixtureEnabledValue && count != referenceGeneratorMultipleRegistrations {
		return nil
	}
	handlers := []*extensionpb.HandlerDescriptor{extensionpb.HandlerDescriptor_builder{
		Id: new(referenceGeneratorID), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_GENERATE),
	}.Build()}
	if count == referenceGeneratorMultipleRegistrations {
		handlers = append(handlers, extensionpb.HandlerDescriptor_builder{
			Id: new(referenceGeneratorSecondID), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_GENERATE),
		}.Build())
	}
	return handlers
}

// generateResult applies the reference policy's no-result generation rule.
func (o *policyOperation) generateResult(
	ctx context.Context,
	input *extensionpb.CompactionPolicyInvocation,
) (*extensionpb.CompactionResult, bool, error) {
	generators := input.GetGeneratorParticipants()
	if len(generators) != 1 {
		return nil, false, fmt.Errorf(
			"reference policy requires exactly one captured generator; received %d",
			len(generators),
		)
	}
	operation, err := o.policy.StartGenerator(ctx, generators[0], extensionpb.CompactionRequestInvocation_builder{
		Original: o.state.GetOriginalRequest(), Current: o.state.GetCurrentRequest(), CurrentResult: nil,
	}.Build())
	if err != nil {
		return nil, false, err
	}
	result, err := operation.Wait(ctx)
	if err != nil {
		if _, canceled := errors.AsType[*extensionsdk.CanceledError](err); canceled && ctx.Err() == nil {
			return nil, true, nil
		}
		return nil, false, err
	}
	return result, false, nil
}

// referenceGeneratorOperation owns one fixture generation invocation.
type referenceGeneratorOperation struct {
	// invocation contains the policy's original and final request.
	invocation *extensionpb.CompactionRequestInvocation
}

// Run supplies an extension-produced result with ordinary session-bound access.
func (o *referenceGeneratorOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	if _, authorized := extensionsdk.FindCompactionPolicy(ctx); authorized {
		return nil, errors.New("generator received policy authority")
	}
	if o.invocation.GetOriginal().GetInstructions() != referenceRequestClear ||
		o.invocation.GetCurrent().GetInstructions() != referenceRequestSecondInstruction ||
		o.invocation.GetCurrentResult() != nil {
		return nil, errors.New("generator did not receive immutable original and final request state")
	}
	mode := os.Getenv(referenceGeneratorModeEnvironment)
	if mode == referenceGeneratorBlockedModel {
		if selectionErr := handleMissingModelSelection(
			ctx,
			binding,
			o.invocation.GetOriginal().GetModel().GetProviderId(),
		); selectionErr != nil {
			return nil, selectionErr
		}
		if modelErr := waitPolicyConfiguredModel(
			ctx,
			binding,
			o.invocation.GetOriginal().GetModel(),
			o.invocation.GetOriginal().GetReasoningChoice(),
		); modelErr != nil {
			return nil, modelErr
		}
	}
	switch mode {
	case referenceGeneratorCancel:
		return nil, context.Canceled
	case referenceGeneratorMixedCancel:
		return nil, errors.Join(context.Canceled, errors.New("reference generator independent failure"))
	case referenceGeneratorClassifiedCancel:
		return nil, extensionsdk.Fail(referenceGeneratorFailureCode, context.Canceled)
	}
	producer := binding.Identity().GetExtensionId()
	if mode == referenceGeneratorForgedProducer {
		producer = referenceGeneratorFalseProducer
	}
	source := new(extensionpb.CompactionSource)
	source.SetExtensionId(producer)
	if mode == referenceGeneratorImages {
		modelSource, modelErr := requestGeneratorModel(ctx, binding, referenceGeneratorReasoningChoice)
		if modelErr != nil {
			return nil, modelErr
		}
		source.SetModelRequests(
			extensionpb.CompactionModelRequests_builder{
				Requests: []*extensionpb.CompactionModelRequest{modelSource},
			}.Build(),
		)
	}
	response := new(extensionpb.HandleResponse)
	response.SetCompactionGenerate(o.newResult(source, mode))
	return response, nil
}

// newResult builds a generator candidate with its retained boundary and source.
func (o *referenceGeneratorOperation) newResult(
	source *extensionpb.CompactionSource,
	mode string,
) *extensionpb.CompactionResult {
	boundary := o.invocation.GetCurrent().GetSuffix()[0].GetEntry().GetId()
	if mode == referenceGeneratorInvalidBoundary {
		boundary = referenceGeneratorOutsideBranch
	}
	return extensionpb.CompactionResult_builder{
		Summary: new(
			referenceGeneratorSummary,
		),
		FirstKeptEntryId: new(boundary),
		Source:           source,
		Details:          []byte{},
	}.Build()
}

// Release completes the generator's runtime admission.
func (o *referenceGeneratorOperation) Release() {}
