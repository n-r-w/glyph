package main

import (
	"context"
	"encoding/json/v2"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// configuredInputReport contains the image result and public input rejection text.
type configuredInputReport struct {
	// ResultJSON contains the successful public model response.
	ResultJSON string `json:"result_json"`
	// Errors contains the complete public error for each invalid input.
	Errors map[string]string `json:"errors"`
}

// requestConfiguredInput exercises image content and invalid input through the public SDK.
func requestConfiguredInput(ctx context.Context, instructions string) (*extensionv1.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	request := newConfiguredModelRequest(instructions, configuredNestedReasoningChoice)
	messages := request.GetMessages()
	messages[0].SetContent([]*extensionv1.ConfiguredModelInputContent{
		extensionv1.ConfiguredModelInputContent_builder{Text: new("first user"), Image: nil}.Build(),
		extensionv1.ConfiguredModelInputContent_builder{Text: nil, Image: extensionv1.ConfiguredModelImage_builder{
			MediaType: new("image/png"), Data: []byte{0, 1, 2, 255},
		}.Build()}.Build(),
		extensionv1.ConfiguredModelInputContent_builder{Text: new("after image"), Image: nil}.Build(),
	})
	operation, err := binding.StartConfiguredModel(ctx, request)
	if err != nil {
		return nil, err
	}
	result, err := operation.Wait(ctx)
	if err != nil {
		return nil, err
	}
	resultJSON, err := protojson.Marshal(result)
	if err != nil {
		return nil, err
	}
	failures := collectConfiguredInputErrors(ctx, binding, request)
	encoded, err := json.Marshal(configuredInputReport{ResultJSON: string(resultJSON), Errors: failures})
	if err != nil {
		return nil, err
	}
	return catalogueTextResult(encoded), nil
}

// collectConfiguredInputErrors records each public invalid-input operation error.
func collectConfiguredInputErrors(
	ctx context.Context,
	binding *extensionsdk.ExtensionContext,
	request *extensionv1.ConfiguredModelRequest,
) map[string]string {
	const (
		// emptyConfiguredContent identifies the empty content rejection case.
		emptyConfiguredContent = "empty content"
		// missingConfiguredPayload identifies the missing payload rejection case.
		missingConfiguredPayload = "missing payload"
		// emptyConfiguredText identifies the empty text rejection case.
		emptyConfiguredText = "empty text"
		// missingConfiguredMediaType identifies the missing media type rejection case.
		missingConfiguredMediaType = "missing media type"
		// emptyConfiguredImageData identifies the empty image data rejection case.
		emptyConfiguredImageData = "empty image data"
		// assistantConfiguredImage identifies the assistant image rejection case.
		assistantConfiguredImage = "assistant image"
		// unknownConfiguredRole identifies the unknown role rejection case.
		unknownConfiguredRole = "unknown role"
		// unsupportedConfiguredImage identifies the unsupported image rejection case.
		unsupportedConfiguredImage = "unsupported image"
		// unknownConfiguredRoleValue is the invalid author role used in rejection checks.
		unknownConfiguredRoleValue = 99
	)
	failures := make(map[string]string)
	for _, name := range []string{
		emptyConfiguredContent, missingConfiguredPayload, emptyConfiguredText,
		missingConfiguredMediaType, emptyConfiguredImageData,
		assistantConfiguredImage, unknownConfiguredRole, unsupportedConfiguredImage,
	} {
		invalid := proto.CloneOf(request)
		message := invalid.GetMessages()[0]
		switch name {
		case emptyConfiguredContent:
			message.SetContent(nil)
		case missingConfiguredPayload:
			message.SetContent([]*extensionv1.ConfiguredModelInputContent{new(extensionv1.ConfiguredModelInputContent)})
		case emptyConfiguredText:
			message.GetContent()[0].SetText("")
		case missingConfiguredMediaType:
			message.GetContent()[1].GetImage().SetMediaType("")
		case emptyConfiguredImageData:
			message.GetContent()[1].GetImage().SetData(nil)
		case assistantConfiguredImage:
			message.SetRole(extensionv1.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_ASSISTANT)
		case unknownConfiguredRole:
			message.SetRole(extensionv1.ConfiguredModelRole(unknownConfiguredRoleValue))
		case unsupportedConfiguredImage:
			invalid.GetSelection().SetModelId("gpt-active")
			invalid.GetSelection().SetReasoningChoice(configuredDisabledReasoningChoice)
		}
		invalidOperation, failure := binding.StartConfiguredModel(ctx, invalid)
		if failure == nil {
			_, failure = invalidOperation.Wait(ctx)
		}
		if failure == nil {
			failures[name] = "unexpected success"
		} else {
			failures[name] = failure.Error()
		}
	}
	return failures
}
