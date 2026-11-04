package main

import (
	"context"

	"google.golang.org/protobuf/encoding/protojson"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// readTools reads the registered tools through the invocation context and returns them as fixture text.
func readTools(ctx context.Context) (*extensionv1.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	read, err := binding.StartGetTools(ctx)
	if err != nil {
		return nil, err
	}
	result, err := read.Wait(ctx)
	if err != nil {
		return nil, err
	}
	encoded, err := protojson.Marshal(result)
	if err != nil {
		return nil, err
	}
	return catalogueTextResult(encoded), nil
}
