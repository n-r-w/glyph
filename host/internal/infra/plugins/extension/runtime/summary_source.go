package runtime

import (
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
