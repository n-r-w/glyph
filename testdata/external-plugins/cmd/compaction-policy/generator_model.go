package main

import (
	"context"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// requestGeneratorModel obtains summary-model accounting through ordinary session-bound access.
func requestGeneratorModel(
	ctx context.Context,
	binding *extensionsdk.ExtensionContext,
	reasoningChoice string,
) (*extensionpb.CompactionModelRequest, error) {
	const (
		providerID            = "openai-codex"
		modelID               = "gpt-test"
		firstUserText         = "first user"
		afterImageText        = "after image"
		assistantText         = "assistant reply"
		assistantContinuation = "assistant continuation"
		secondUserText        = "second user"
		imageMediaType        = "image/png"
	)
	selection := extensionpb.ModelSelection_builder{
		ProviderId: new(providerID), ModelId: new(modelID), ReasoningChoice: new(reasoningChoice),
	}.Build()
	request := extensionpb.ConfiguredModelRequest_builder{
		Context: nil, Selection: selection, Instructions: new(""),
		Messages: []*extensionpb.ConfiguredModelMessage{
			extensionpb.ConfiguredModelMessage_builder{
				Role: new(extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER),
				Content: []*extensionpb.ConfiguredModelInputContent{
					extensionpb.ConfiguredModelInputContent_builder{Text: new(firstUserText), Image: nil}.Build(),
					extensionpb.ConfiguredModelInputContent_builder{
						Text: nil,
						Image: extensionpb.ConfiguredModelImage_builder{
							MediaType: new(imageMediaType), Data: []byte{0, 1, 2, 255},
						}.Build(),
					}.Build(),
					extensionpb.ConfiguredModelInputContent_builder{Text: new(afterImageText), Image: nil}.Build(),
				},
			}.Build(),
			extensionpb.ConfiguredModelMessage_builder{
				Role: new(extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_ASSISTANT),
				Content: []*extensionpb.ConfiguredModelInputContent{
					extensionpb.ConfiguredModelInputContent_builder{Text: new(assistantText), Image: nil}.Build(),
					extensionpb.ConfiguredModelInputContent_builder{
						Text:  new(assistantContinuation),
						Image: nil,
					}.Build(),
				},
			}.Build(),
			extensionpb.ConfiguredModelMessage_builder{
				Role: new(extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER),
				Content: []*extensionpb.ConfiguredModelInputContent{
					extensionpb.ConfiguredModelInputContent_builder{Text: new(secondUserText), Image: nil}.Build(),
				},
			}.Build(),
		},
	}.Build()
	operation, err := binding.StartConfiguredModel(ctx, request)
	if err != nil {
		return nil, err
	}
	result, err := operation.WaitWithProgress(ctx, writeGeneratorProgress)
	if err != nil {
		return nil, err
	}
	var usage *extensionpb.TokenUsage
	if reported := result.GetUsage(); reported != nil {
		usage = extensionpb.TokenUsage_builder{
			InputTokens:  new(reported.GetInputTokens()),
			OutputTokens: new(reported.GetOutputTokens()),
			CacheReadTokens: new(
				reported.GetCachedInputTokens(),
			),
			CacheWriteTokens: new(reported.GetCacheWriteTokens()),
			ReasoningTokens:  new(reported.GetReasoningTokens()),
			TotalTokens:      new(reported.GetTotalTokens()),
		}.Build()
	}
	return extensionpb.CompactionModelRequest_builder{
		Selection:     selection,
		Usage:         usage,
		EstimatedCost: nil,
	}.Build(), nil
}

// writeGeneratorProgress records the configured operation's public retry outcome.
func writeGeneratorProgress(progress *extensionpb.ConfiguredModelRetryProgress) error {
	const progressEnvironment = "GLYPH_REFERENCE_GENERATOR_PROGRESS"
	return writePolicyArtifact(os.Getenv(progressEnvironment), progress)
}
