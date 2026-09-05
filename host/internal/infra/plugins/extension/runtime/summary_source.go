package runtime

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// mapSummarySource projects the producer with usage inside the model alternative.
func mapSummarySource(source session.BranchSummarySource) *extensionpb.BranchSummarySource {
	// A missing alternative remains missing so final validation can reject it.
	wire := new(extensionpb.BranchSummarySource)
	if extensionID, present := source.ExtensionID.Get(); present {
		wire.SetExtensionId(extensionID)
	} else if value, modelPresent := source.Model.Get(); modelPresent {
		// Model usage travels with the selection that produced it.
		mapped := extensionpb.BranchSummaryModelSource_builder{
			Selection: mapModelSelection(value.Selection), Usage: nil,
		}
		if usage, reported := value.Usage.Get(); reported {
			mapped.Usage = mapTokenUsage(usage)
		}
		wire.SetModel(mapped.Build())
	}
	return wire
}

// mapSummarySourceFromProto retains missing or malformed source values for final Host validation.
func mapSummarySourceFromProto(wire *extensionpb.BranchSummarySource) session.BranchSummarySource {
	// Do not repair omitted source metadata with the configured summary model.
	source := session.BranchSummarySource{
		ExtensionID: mo.None[string](),
		Model:       mo.None[session.BranchSummaryModelSource](),
	}
	if wire == nil {
		return source
	}
	if wire.HasExtensionId() {
		source.ExtensionID = mo.Some(wire.GetExtensionId())
	} else if value := wire.GetModel(); value != nil {
		// Missing selection fields remain invalid values for Host final validation.
		selection := value.GetSelection()
		// Preserve absent usage rather than manufacturing zero-token execution.
		usage := mo.None[session.TokenUsage]()
		if value.GetUsage() != nil {
			usage = mo.Some(mapTokenUsageFromProto(value.GetUsage()))
		}
		source.Model = mo.Some(session.BranchSummaryModelSource{
			Selection: model.Selection{
				Provider: model.ProviderID(selection.GetProviderId()), Model: model.ID(selection.GetModelId()),
				ReasoningChoice: model.ReasoningChoice(selection.GetReasoningChoice()),
			}, Usage: usage,
		})
	}
	return source
}
