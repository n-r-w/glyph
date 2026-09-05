package main

import (
	"context"

	"google.golang.org/protobuf/encoding/protojson"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// requestConfiguredModel executes one explicit public request and returns its public result as tool text.
func requestConfiguredModel(ctx context.Context) (*extensionv1.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	request := extensionv1.ConfiguredModelRequest_builder{
		Context: nil,
		Selection: extensionv1.ModelSelection_builder{
			ProviderId: new("openai-codex"), ModelId: new("gpt-test"), ReasoningChoice: new("off"),
		}.Build(),
		Instructions: new("extension instructions"),
		Messages: []*extensionv1.ConfiguredModelMessage{
			extensionv1.ConfiguredModelMessage_builder{
				Role: new(extensionv1.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER), Text: new("first user"),
			}.Build(),
			extensionv1.ConfiguredModelMessage_builder{
				Role: new(
					extensionv1.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_ASSISTANT,
				),
				Text: new("assistant reply"),
			}.Build(),
			extensionv1.ConfiguredModelMessage_builder{
				Role: new(extensionv1.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER), Text: new("second user"),
			}.Build(),
		},
	}.Build()
	operation, err := binding.StartConfiguredModel(ctx, request)
	if err != nil {
		return nil, err
	}
	result, err := operation.Wait(ctx)
	if err != nil {
		return nil, err
	}
	encoded, err := protojson.Marshal(result)
	if err != nil {
		return nil, err
	}
	return catalogueTextResult(encoded), nil
}
