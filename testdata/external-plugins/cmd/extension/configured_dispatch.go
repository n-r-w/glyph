package main

import (
	"context"
	"errors"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// runConfiguredRequest executes the selected configured-model fixture scenario.
func (operation *executeOperation) runConfiguredRequest(ctx context.Context) (*extensionpb.ToolResult, error) {
	switch operation.mode {
	case configuredRequestMode:
		return requestConfiguredModel(ctx, configuredInstructions, configuredNestedReasoningChoice)
	case configuredInputMode:
		return requestConfiguredInput(ctx, configuredInstructions)
	case configuredEmptyRequestMode:
		return requestConfiguredModel(ctx, "", configuredNestedReasoningChoice)
	case configuredEmptyInputMode:
		return requestConfiguredInput(ctx, "")
	case configuredRequestProgressMode:
		return requestConfiguredModelWithProgress(ctx)
	case configuredRequestCancellationMode:
		return requestConfiguredModelCancellation(ctx)
	case configuredRequestFailuresMode:
		return requestConfiguredModelFailures(ctx)
	case consumedConfiguredMode:
		return requestConsumedConfiguredFailure(ctx, operation.signals)
	default:
		return nil, errors.New("configured fixture mode is invalid")
	}
}
