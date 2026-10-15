package main

import (
	"context"
	"strings"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// requestReplacementSource supplies the reference scenario's producer accounting.
func requestReplacementSource(
	ctx context.Context,
	binding *extensionsdk.ExtensionContext,
	guidance string,
) (*extensionpb.CompactionSource, error) {
	const referenceAccountingGuidance = "accounting:"

	const referenceAccountingSecondReasoning = "off"

	source := new(extensionpb.CompactionSource)
	if !strings.HasPrefix(guidance, referenceAccountingGuidance) {
		source.SetExtensionId(binding.Identity().GetExtensionId())
		return source, nil
	}
	choices := []string{referenceGeneratorReasoningChoice, referenceAccountingSecondReasoning}
	requests := make([]*extensionpb.CompactionModelRequest, len(choices))
	for index, choice := range choices {
		request, err := requestGeneratorModel(ctx, binding, choice)
		if err != nil {
			return nil, err
		}
		requests[index] = request
	}
	source.SetModelRequests(extensionpb.CompactionModelRequests_builder{Requests: requests}.Build())
	return source, nil
}
