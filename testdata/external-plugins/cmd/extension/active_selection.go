package main

import (
	"context"

	"google.golang.org/protobuf/encoding/protojson"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// exerciseSelection selects one model and reasoning choice and returns the final typed result as tool text.
func exerciseSelection(ctx context.Context) (*extensionv1.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	modelOperation, err := binding.StartModelSelection(ctx, extensionv1.SelectModelRequest_builder{
		Context: nil, ProviderId: new(selectionProviderID), ModelId: new(selectionModelID),
	}.Build())
	if err != nil {
		return nil, err
	}
	if _, err = modelOperation.Wait(ctx); err != nil {
		return nil, err
	}
	reasoningOperation, err := binding.StartReasoningSelection(ctx, extensionv1.SelectReasoningRequest_builder{
		Context: nil, ReasoningChoice: new(selectionFinalReasoning),
	}.Build())
	if err != nil {
		return nil, err
	}
	result, err := reasoningOperation.Wait(ctx)
	if err != nil {
		return nil, err
	}
	encoded, err := protojson.Marshal(result)
	if err != nil {
		return nil, err
	}
	return catalogueTextResult(encoded), nil
}
