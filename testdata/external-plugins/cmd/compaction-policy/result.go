package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// referenceResultsEnvironment is the environment variable name for enabling result participants.
	referenceResultsEnvironment = "GLYPH_REFERENCE_RESULTS"
	// referenceResultInputsEnvironment is the environment variable name for the location of captured participant inputs.
	referenceResultInputsEnvironment = "GLYPH_REFERENCE_RESULT_INPUTS"
	// referenceResultModel is the mode for ordinary configured-model access.
	referenceResultModel = "model"
	// referenceResultHistorical is the mode for a historical model result source.
	referenceResultHistorical = "historical"
	// referenceResultOriginal is the mode for a later transformation from the original result.
	referenceResultOriginal = "original"
	// referenceResultOriginalSummary identifies a result derived from the original.
	referenceResultOriginalSummary = "Reference original-derived result summary."
	// referenceResultFirst identifies the first registered transformation.
	referenceResultFirst = "result-z-first"
	// referenceResultSecond identifies the second registered transformation.
	referenceResultSecond = "result-a-second"
	// referenceResultLast identifies the sentinel after terminal handling.
	referenceResultLast = "result-m-last"
	// referenceResultModeEnvironment is the environment variable name for the terminal action fixture.
	referenceResultModeEnvironment = "GLYPH_REFERENCE_RESULT_MODE"
	// referenceSeparateResultsEnvironment is the environment variable name for a separate result runtime.
	referenceSeparateResultsEnvironment = "GLYPH_REFERENCE_SEPARATE_RESULTS"
	// referenceResultsExecutable identifies the participant-only runtime.
	referenceResultsExecutable = "reference-results"
	// referenceResultSummary is the transformed replacement content.
	referenceResultSummary = "Reference composed result summary."
)

// newReferenceResultDescriptors supplies result capabilities for the fixture.
func newReferenceResultDescriptors() []*extensionpb.HandlerDescriptor {
	if os.Getenv(referenceResultsEnvironment) == "" {
		return nil
	}
	return []*extensionpb.HandlerDescriptor{
		extensionpb.HandlerDescriptor_builder{
			Id:   new(referenceResultFirst),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_RESULT),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new(referenceResultSecond),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_RESULT),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new(referenceResultLast),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_RESULT),
		}.Build(),
	}
}

// referenceResultOperation supplies one result transformation.
type referenceResultOperation struct {
	// handlerID identifies the selected transformation.
	handlerID string
	// invocation contains original and composed inputs.
	invocation *extensionpb.CompactionResultInvocation
}

var _ extensionsdk.HandleOperation = (*referenceResultOperation)(nil)

// Run returns a replacement or preservation through the public handler contract.
func (o *referenceResultOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	if _, authorized := extensionsdk.FindCompactionPolicy(ctx); authorized {
		return nil, errors.New("result participant received policy authority")
	}
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	if directory := os.Getenv(referenceResultInputsEnvironment); directory != "" {
		if writeErr := writePolicyArtifact(
			filepath.Join(directory, o.handlerID+".json"),
			o.invocation,
		); writeErr != nil {
			return nil, writeErr
		}
	}
	action := extensionpb.CompactionResultAction_builder{
		Cancel:       new(false),
		ResultAction: new(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE),
		Result:       nil,
	}.Build()
	if o.handlerID == referenceResultFirst {
		candidate := proto.CloneOf(o.invocation.GetCurrentResult())
		candidate.SetSummary(referenceResultSummary)
		source := new(extensionpb.CompactionSource)
		source.SetExtensionId(binding.Identity().GetExtensionId())
		candidate.SetSource(source)
		if os.Getenv(referenceResultModeEnvironment) == referenceResultModel {
			modelSource, modelErr := requestGeneratorModel(ctx, binding, referenceGeneratorReasoningChoice)
			if modelErr != nil {
				return nil, modelErr
			}
			candidate.SetSource(
				extensionpb.CompactionSource_builder{
					ExtensionId: nil,
					ModelRequests: extensionpb.CompactionModelRequests_builder{
						Requests: []*extensionpb.CompactionModelRequest{modelSource},
					}.Build(),
				}.Build(),
			)
		}
		if os.Getenv(referenceResultModeEnvironment) == referenceResultHistorical {
			candidate.SetSource(newReferenceHistoricalResultSource())
		}
		candidate.SetDetails([]byte{})
		action.SetResultAction(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE)
		action.SetResult(candidate)
		// The participant must leave policy-owned originals unchanged during local changes.
		const localResultMutation = "participant-local result mutation"
		const localRequestMutation = "participant-local request mutation"
		o.invocation.GetOriginalResult().SetSummary(localResultMutation)
		o.invocation.GetOriginalRequest().SetInstructions(localRequestMutation)
	}
	if o.handlerID == referenceResultSecond {
		action, err = o.runTerminalAction(ctx, binding, action)
		if err != nil {
			return nil, err
		}
	}
	response := new(extensionpb.HandleResponse)
	response.SetCompactionResult(action)
	return response, nil
}

// Release ends participant-owned resources.
func (o *referenceResultOperation) Release() {}
