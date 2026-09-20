package extension

import (
	"errors"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// mapCommittedCompaction projects one durable marker without exposing unrelated session payloads.
func mapCommittedCompaction(entry session.Entry) (*extensionpb.CommittedCompaction, error) {
	compaction, present := entry.Compaction.Get()
	if !present || entry.ID == "" {
		return nil, errors.New("committed compaction entry is incomplete")
	}
	builder := extensionpb.SessionTreeCompaction_builder{
		Summary: new(compaction.Summary), FirstKeptEntryId: new(compaction.FirstKeptEntryID),
		Source: mapCompactionSource(compaction.Source), EstimatedCost: nil, Details: nil,
	}
	if cost, costPresent := compaction.EstimatedCost.Get(); costPresent {
		builder.EstimatedCost = extensionpb.EstimatedCost_builder{
			Input: new(cost.Input), Output: new(cost.Output), CacheRead: new(cost.CacheRead),
			CacheWrite: new(cost.CacheWrite), Total: new(cost.Total),
		}.Build()
	}
	if details, detailsPresent := compaction.Details.Get(); detailsPresent {
		builder.Details = append([]byte(nil), details...)
	}
	return extensionpb.CommittedCompaction_builder{
		EntryId: new(entry.ID), Compaction: builder.Build(),
	}.Build(), nil
}

// mapCompactionSource projects the exclusive extension or model producer.
func mapCompactionSource(source session.CompactionSource) *extensionpb.BranchSummarySource {
	mapped := new(extensionpb.BranchSummarySource)
	if extensionID, present := source.ExtensionID.Get(); present {
		mapped.SetExtensionId(extensionID)
		return mapped
	}
	if modelSource, present := source.Model.Get(); present {
		selection := modelSource.Selection
		builder := extensionpb.BranchSummaryModelSource_builder{
			Selection: extensionpb.ModelSelection_builder{
				ProviderId: new(string(selection.Provider)), ModelId: new(string(selection.Model)),
				ReasoningChoice: new(string(selection.ReasoningChoice)),
			}.Build(),
			Usage: nil,
		}
		if usage, usagePresent := modelSource.Usage.Get(); usagePresent {
			builder.Usage = extensionpb.TokenUsage_builder{
				InputTokens: new(usage.InputTokens), OutputTokens: new(usage.OutputTokens),
				CacheReadTokens: new(usage.CacheReadTokens), CacheWriteTokens: new(usage.CacheWriteTokens),
				ReasoningTokens: new(usage.ReasoningTokens), TotalTokens: new(usage.TotalTokens),
			}.Build()
		}
		mapped.SetModel(builder.Build())
	}
	return mapped
}
