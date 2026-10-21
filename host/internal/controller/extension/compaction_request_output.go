package extension

import (
	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// encodePolicyRequestResult projects a supplied replacement result.
func encodePolicyRequestResult(result session.CompactionEntry) *extensionpb.CompactionResult {
	wire := extensionpb.CompactionResult_builder{
		Summary:          new(result.Summary),
		FirstKeptEntryId: new(result.FirstKeptEntryID),
		Source:           MapCompactionSource(result.Source),
		Details:          nil,
	}.Build()
	if details, present := result.Details.Get(); present {
		wire.SetDetails(append([]byte(nil), details...))
	}
	return wire
}
