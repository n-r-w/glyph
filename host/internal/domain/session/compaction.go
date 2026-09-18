package session

import (
	"bytes"
	"errors"

	"github.com/samber/mo"
)

// CompactionSource identifies the producer of a context-compaction summary.
type CompactionSource = BranchSummarySource

// CompactionEntry stores one active-context summary and its retained suffix boundary.
type CompactionEntry struct {
	// Summary contains the replacement context supplied by its source.
	Summary string
	// FirstKeptEntryID identifies the first original entry retained after the summary.
	FirstKeptEntryID string
	// Source identifies the actual producer and its optional model usage.
	Source CompactionSource
	// EstimatedCost contains persisted summary cost when calculable.
	EstimatedCost mo.Option[EstimatedCost]
	// Details contains optional opaque extension-owned result data.
	Details mo.Option[[]byte]
}

// ValidateAccounting checks source identity, reported usage, and persisted cost.
func (compaction CompactionEntry) ValidateAccounting() error {
	if err := compaction.Source.Validate(); err != nil {
		return err
	}
	if cost, present := compaction.EstimatedCost.Get(); present {
		modelSource, modelPresent := compaction.Source.Model.Get()
		if !modelPresent || modelSource.Usage.IsNone() {
			return errors.New("compaction cost requires reported model usage")
		}
		if !cost.Valid() {
			return errors.New("compaction estimated cost is invalid")
		}
	}
	return nil
}

// Clone returns a compaction payload with independent extension details.
func (compaction CompactionEntry) Clone() CompactionEntry {
	compaction.Details = compaction.Details.MapValue(bytes.Clone)
	return compaction
}
