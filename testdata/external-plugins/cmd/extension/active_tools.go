package main

import (
	"context"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// activeToolsHandlerID identifies the fixture input handler that updates tool selection.
	activeToolsHandlerID = "select-active-tools"
	// activeToolsInput selects only the fixture tool through a steer input.
	activeToolsInput = "select only external tool"
)

// activeToolsInputOperation updates selection from an input handler while another model request runs.
type activeToolsInputOperation struct {
	// binding owns the nested context operation.
	binding *extensionsdk.ExtensionContext
	// request contains the input being processed.
	request *extensionpb.HandleRequest
}

// prepareActiveToolsInput captures invocation ownership for the selection handler.
func prepareActiveToolsInput(
	ctx context.Context,
	request *extensionpb.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	return &activeToolsInputOperation{binding: binding, request: request}, nil
}

// setAndReadTools updates selection from inside the fixture's own tool before reading the catalog.
func setAndReadTools(ctx context.Context) (*extensionpb.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	update, err := binding.StartSetActiveTools(ctx, []string{toolName})
	if err != nil {
		return nil, err
	}
	if waitErr := update.Wait(ctx); waitErr != nil {
		return nil, waitErr
	}
	return readTools(ctx)
}

// Run preserves input after completing a requested active-set update.
func (o *activeToolsInputOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	for _, content := range o.request.GetUserInput().GetCurrent().GetContent() {
		if content.GetText() != activeToolsInput {
			continue
		}
		update, err := o.binding.StartSetActiveTools(ctx, []string{toolName})
		if err != nil {
			return nil, err
		}
		if waitErr := update.Wait(ctx); waitErr != nil {
			return nil, waitErr
		}
	}
	response := new(extensionpb.HandleResponse)
	response.SetUserInput(extensionpb.UserInputAction_builder{Preserve: new(true), Replace: nil, Handle: nil}.Build())
	return response, nil
}

// Release frees the input operation, which owns no reservation.
func (*activeToolsInputOperation) Release() {}
