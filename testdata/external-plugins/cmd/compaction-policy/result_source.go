package main

import extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"

const (
	// referenceHistoricalProvider identifies a historical result producer.
	referenceHistoricalProvider = "result-historical-provider"
	// referenceHistoricalModel identifies a historical model selection.
	referenceHistoricalModel = "result-historical-model"
	// referenceHistoricalReasoning identifies the historical reasoning choice.
	referenceHistoricalReasoning = "high"
)

// newReferenceHistoricalResultSource supplies a valid historical selection without usage.
func newReferenceHistoricalResultSource() *extensionpb.CompactionSource {
	return extensionpb.CompactionSource_builder{
		ExtensionId: nil,
		ModelRequests: extensionpb.CompactionModelRequests_builder{
			Requests: []*extensionpb.CompactionModelRequest{extensionpb.CompactionModelRequest_builder{
				Selection: extensionpb.ModelSelection_builder{
					ProviderId:      new(referenceHistoricalProvider),
					ModelId:         new(referenceHistoricalModel),
					ReasoningChoice: new(referenceHistoricalReasoning),
				}.Build(),
				Usage:         nil,
				EstimatedCost: nil,
			}.Build()},
		}.Build(),
	}.Build()
}
