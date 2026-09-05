package main

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"

	"google.golang.org/protobuf/encoding/protojson"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// catalogueReport exposes exactly the identity and catalog data received through the public SDK.
type catalogueReport struct {
	// Identity contains the issued invocation binding.
	Identity jsontext.Value `json:"identity"`
	// Models contains complete provider-neutral descriptors and active selection.
	Models jsontext.Value `json:"models"`
	// Providers contains provider IDs and ordered model IDs.
	Providers jsontext.Value `json:"providers"`
}

// readCatalogues starts and awaits both catalog kinds without another connection or Host file access.
func readCatalogues(ctx context.Context) (*extensionv1.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	models, err := binding.StartGetModels(ctx)
	if err != nil {
		return nil, err
	}
	modelResult, err := models.Wait(ctx)
	if err != nil {
		return nil, err
	}
	providers, err := binding.StartGetProviders(ctx)
	if err != nil {
		return nil, err
	}
	providerResult, err := providers.Wait(ctx)
	if err != nil {
		return nil, err
	}
	identityJSON, err := protojson.Marshal(binding.Identity())
	if err != nil {
		return nil, err
	}
	modelJSON, err := protojson.Marshal(modelResult)
	if err != nil {
		return nil, err
	}
	providerJSON, err := protojson.Marshal(providerResult)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(catalogueReport{Identity: identityJSON, Models: modelJSON, Providers: providerJSON})
	if err != nil {
		return nil, err
	}
	return catalogueTextResult(encoded), nil
}

// catalogueTextResult returns an observed public result as model-visible fixture text.
func catalogueTextResult(encoded []byte) *extensionv1.ToolResult {
	return extensionv1.ToolResult_builder{Contents: []*extensionv1.ToolResultContent{
		//nolint:exhaustruct_v5 // Only the active text result is populated.
		extensionv1.ToolResultContent_builder{Text: new(string(encoded))}.Build(),
	}, IsError: new(false)}.Build()
}

// readRetainedCatalogues reports the actual public outcome of using the extension's first binding again.
func (o *executeOperation) readRetainedCatalogues(ctx context.Context) (*extensionv1.ToolResult, error) {
	if o.savedContext == nil {
		return nil, errors.New("no preceding invocation context was retained")
	}
	current, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	started, readErr := o.savedContext.StartGetModels(ctx)
	if readErr == nil {
		_, readErr = started.Wait(ctx)
	}
	code, text := "", ""
	if readErr != nil {
		text = readErr.Error()
		code = internalFailureCode
		if rejected, found := errors.AsType[*extensionsdk.RejectionError](readErr); found {
			code = rejected.Code()
		}
		if failed, found := errors.AsType[*extensionsdk.FailureError](readErr); found {
			code = failed.Code()
		}
	}
	encoded, err := json.Marshal(struct {
		// PreviousContextID identifies the saved invocation binding.
		PreviousContextID string `json:"previous_context_id"`
		// CurrentContextID identifies the newly issued invocation binding.
		CurrentContextID string `json:"current_context_id"`
		// ErrorCode records the category returned by the SDK.
		ErrorCode string `json:"error_code"`
		// ErrorText records the complete SDK failure text.
		ErrorText string `json:"error_text"`
	}{
		PreviousContextID: o.savedContext.Identity().GetContextId(),
		CurrentContextID:  current.Identity().GetContextId(),
		ErrorCode:         code,
		ErrorText:         text,
	})
	if err != nil {
		return nil, err
	}
	return catalogueTextResult(encoded), nil
}
