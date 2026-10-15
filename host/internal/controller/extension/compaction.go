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
	return extensionpb.CommittedCompaction_builder{
		EntryId: new(entry.ID), Compaction: MapCompactionMarker(compaction),
	}.Build(), nil
}

// MapCompactionSource projects contributing request accounting.
func MapCompactionSource(source session.CompactionSource) *extensionpb.CompactionSource {
	mapped := new(extensionpb.CompactionSource)
	if extensionID, present := source.ExtensionID.Get(); present {
		mapped.SetExtensionId(extensionID)
		return mapped
	}
	if len(source.Requests) > 0 {
		requests := make([]*extensionpb.CompactionModelRequest, len(source.Requests))
		for index := range source.Requests {
			request := &source.Requests[index]
			selection := request.Selection
			builder := extensionpb.CompactionModelRequest_builder{
				Selection: extensionpb.ModelSelection_builder{
					ProviderId: new(string(selection.Provider)), ModelId: new(string(selection.Model)),
					ReasoningChoice: new(string(selection.ReasoningChoice)),
				}.Build(), Usage: nil, EstimatedCost: nil,
			}
			if usage, present := request.Usage.Get(); present {
				builder.Usage = extensionpb.TokenUsage_builder{
					InputTokens: new(usage.InputTokens), OutputTokens: new(usage.OutputTokens),
					CacheReadTokens: new(usage.CacheReadTokens), CacheWriteTokens: new(usage.CacheWriteTokens),
					ReasoningTokens: new(usage.ReasoningTokens), TotalTokens: new(usage.TotalTokens),
				}.Build()
			}
			if cost, present := request.EstimatedCost.Get(); present {
				builder.EstimatedCost = extensionpb.EstimatedCost_builder{
					Input: new(cost.Input), Output: new(cost.Output), CacheRead: new(cost.CacheRead),
					CacheWrite: new(cost.CacheWrite), Total: new(cost.Total),
				}.Build()
			}
			requests[index] = builder.Build()
		}
		mapped.SetModelRequests(extensionpb.CompactionModelRequests_builder{Requests: requests}.Build())
	}
	return mapped
}

// MapCompactionMarker supplies a detached complete public replacement marker.
func MapCompactionMarker(value session.CompactionEntry) *extensionpb.SessionTreeCompaction {
	builder := extensionpb.SessionTreeCompaction_builder{
		Summary: new(value.Summary), FirstKeptEntryId: new(value.FirstKeptEntryID),
		Source: MapCompactionSource(value.Source), Details: nil,
	}
	if details, detailsPresent := value.Details.Get(); detailsPresent {
		builder.Details = append([]byte(nil), details...)
	}
	return builder.Build()
}
