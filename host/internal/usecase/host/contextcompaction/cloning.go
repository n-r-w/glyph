package contextcompaction

import (
	"slices"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// cloneSnapshot detaches mutable branch and history values.
func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Entries = cloneEntries(snapshot.Entries)
	if snapshot.Context != nil {
		contextHistory := make([]agent.HistoryEntry, len(snapshot.Context))
		for index := range snapshot.Context {
			contextHistory[index] = snapshot.Context[index].Clone()
		}
		snapshot.Context = contextHistory
	}
	snapshot.Previous = snapshot.Previous.MapValue(session.CompactionEntry.Clone)
	return snapshot
}

// cloneRequest returns one request with independently owned slices and payloads.
func cloneRequest(request Request) Request {
	request.Model = request.Model.Clone()
	request.Prefix = cloneInputEntries(request.Prefix)
	request.Suffix = cloneInputEntries(request.Suffix)
	request.Previous = request.Previous.MapValue(session.CompactionEntry.Clone)
	return request
}

// cloneResult detaches optional extension details.
func cloneResult(result Result) Result {
	result.Details = result.Details.MapValue(appendBytes)
	return result
}

// cloneOptionalResult detaches a present result.
func cloneOptionalResult(result mo.Option[Result]) mo.Option[Result] {
	return result.MapValue(cloneResult)
}

// cloneHandlerSet detaches every snapshotted registration list.
func cloneHandlerSet(set HandlerSet) HandlerSet {
	return HandlerSet{
		Requests:   slices.Clone(set.Requests),
		Generators: slices.Clone(set.Generators),
		Results: slices.Clone(
			set.Results,
		),
		Successes: slices.Clone(set.Successes),
		Failures:  slices.Clone(set.Failures),
	}
}

// cloneOutcome detaches every mutable outcome value.
func cloneOutcome(outcome OutcomeInvocation) OutcomeInvocation {
	outcome.OriginalRequest = cloneRequest(outcome.OriginalRequest)
	outcome.CurrentRequest = cloneRequest(outcome.CurrentRequest)
	outcome.Result = cloneOptionalResult(outcome.Result)
	outcome.Committed = outcome.Committed.MapValue(session.Entry.Clone)
	return outcome
}

// cloneInputEntries detaches projected entries and preserves their Host estimates.
func cloneInputEntries(entries []InputEntry) []InputEntry {
	if entries == nil {
		return nil
	}
	cloned := make([]InputEntry, len(entries))
	for index := range entries {
		cloned[index] = InputEntry{
			Entry: entries[index].Clone(), EstimatedTokens: entries[index].EstimatedTokens,
		}
	}
	return cloned
}

// cloneEntries returns independent persisted entries in source order.
func cloneEntries(entries []session.Entry) []session.Entry {
	if entries == nil {
		return nil
	}
	cloned := make([]session.Entry, len(entries))
	for index := range entries {
		cloned[index] = entries[index].Clone()
	}
	return cloned
}

// appendBytes detaches one opaque byte sequence.
func appendBytes(value []byte) []byte { return append([]byte(nil), value...) }
