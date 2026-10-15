package extension

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// DecodeCompactionPrevious supplies a detached preceding compaction entry.
func DecodeCompactionPrevious(wire *extensionpb.SessionTreeCompaction) mo.Option[session.CompactionEntry] {
	if wire == nil {
		return mo.None[session.CompactionEntry]()
	}
	source := DecodeCompactionSource(wire.GetSource())
	for index, record := range wire.GetSource().GetModelRequests().GetRequests() {
		if cost := record.GetEstimatedCost(); cost != nil {
			source.Requests[index].EstimatedCost = mo.Some(session.EstimatedCost{
				Input: cost.GetInput(), Output: cost.GetOutput(), CacheRead: cost.GetCacheRead(),
				CacheWrite: cost.GetCacheWrite(), Total: cost.GetTotal(),
			})
		}
	}
	return mo.Some(session.CompactionEntry{
		Summary: wire.GetSummary(), FirstKeptEntryID: wire.GetFirstKeptEntryId(),
		Source:  source,
		Details: mo.TupleToOption(append([]byte(nil), wire.GetDetails()...), wire.HasDetails()),
	})
}
