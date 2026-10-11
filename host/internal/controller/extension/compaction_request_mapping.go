package extension

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// DecodeCompactionPrevious supplies a detached preceding replacement.
func DecodeCompactionPrevious(wire *extensionpb.SessionTreeCompaction) mo.Option[session.CompactionEntry] {
	if wire == nil {
		return mo.None[session.CompactionEntry]()
	}
	source := DecodeCompactionSource(wire.GetSource())
	cost := mo.None[session.EstimatedCost]()
	if value := wire.GetEstimatedCost(); value != nil {
		cost = mo.Some(
			session.EstimatedCost{
				Input:      value.GetInput(),
				Output:     value.GetOutput(),
				CacheRead:  value.GetCacheRead(),
				CacheWrite: value.GetCacheWrite(),
				Total:      value.GetTotal(),
			},
		)
	}
	return mo.Some(
		session.CompactionEntry{
			Summary:          wire.GetSummary(),
			FirstKeptEntryID: wire.GetFirstKeptEntryId(),
			Source:           source,
			EstimatedCost:    cost,
			Details:          mo.TupleToOption(append([]byte(nil), wire.GetDetails()...), wire.HasDetails()),
		},
	)
}
