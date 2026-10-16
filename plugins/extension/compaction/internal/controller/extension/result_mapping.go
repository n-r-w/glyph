package extension

import (
	"slices"

	"github.com/samber/mo"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// DecodeResult maps a public replacement candidate into policy data.
func DecodeResult(value *pb.CompactionResult) compaction.Result {
	result := decodeSource(value.GetSummary(), value.GetFirstKeptEntryId(), value.GetSource())
	if value.HasDetails() {
		result.Details = mo.Some(slices.Clone(value.GetDetails()))
	}
	return result
}

// decodeMarker maps a preceding persisted compaction entry into policy data.
func decodeMarker(value *pb.SessionTreeCompaction) mo.Option[compaction.Result] {
	if value == nil {
		return mo.None[compaction.Result]()
	}
	result := decodeSource(value.GetSummary(), value.GetFirstKeptEntryId(), value.GetSource())
	if value.HasDetails() {
		result.Details = mo.Some(slices.Clone(value.GetDetails()))
	}
	return mo.Some(result)
}

// decodeSource maps attribution and per-request accounting into policy data.
func decodeSource(summary, firstKept string, source *pb.CompactionSource) compaction.Result {
	result := compaction.Result{
		Summary:   summary,
		FirstKept: firstKept,
		Extension: mo.None[string](),
		Requests:  nil,
		Details:   mo.None[[]byte](),
	}
	if source.HasExtensionId() {
		result.Extension = mo.Some(source.GetExtensionId())
	}
	for _, request := range source.GetModelRequests().GetRequests() {
		selection := request.GetSelection()
		value := compaction.ModelRequest{
			Provider:  selection.GetProviderId(),
			Model:     selection.GetModelId(),
			Reasoning: selection.GetReasoningChoice(),
			Usage:     decodeSavedUsage(request.GetUsage()),
			Cost:      mo.None[[5]float64](),
		}
		if cost := request.GetEstimatedCost(); cost != nil {
			value.Cost = mo.Some(
				[5]float64{
					cost.GetInput(),
					cost.GetOutput(),
					cost.GetCacheRead(),
					cost.GetCacheWrite(),
					cost.GetTotal(),
				},
			)
		}
		result.Requests = append(result.Requests, value)
	}
	return result
}

// EncodeResult maps a complete candidate to the public contract.
func EncodeResult(value compaction.Result) *pb.CompactionResult {
	result := pb.CompactionResult_builder{
		Summary:          new(value.Summary),
		FirstKeptEntryId: new(value.FirstKept),
		Source:           encodeSource(value),
		Details:          nil,
	}.Build()
	if details, present := value.Details.Get(); present {
		result.SetDetails(slices.Clone(details))
	}
	return result
}

// encodeMarker maps a preceding persisted replacement.
func encodeMarker(value compaction.Result) *pb.SessionTreeCompaction {
	marker := pb.SessionTreeCompaction_builder{
		Summary:          new(value.Summary),
		FirstKeptEntryId: new(value.FirstKept),
		Source:           encodeSource(value),
		Details:          nil,
	}.Build()
	if details, present := value.Details.Get(); present {
		marker.SetDetails(slices.Clone(details))
	}
	return marker
}

// encodeSource maps attribution and contributing requests.
func encodeSource(value compaction.Result) *pb.CompactionSource {
	source := new(pb.CompactionSource)
	if extension, present := value.Extension.Get(); present {
		source.SetExtensionId(extension)
		return source
	}
	requests := make([]*pb.CompactionModelRequest, 0, len(value.Requests))
	for index := range value.Requests {
		request := &value.Requests[index]
		selection := pb.ModelSelection_builder{
			ProviderId:      new(request.Provider),
			ModelId:         new(request.Model),
			ReasoningChoice: new(request.Reasoning),
		}.Build()
		encoded := pb.CompactionModelRequest_builder{Selection: selection, Usage: nil, EstimatedCost: nil}.Build()
		if usage, present := request.Usage.Get(); present {
			encoded.SetUsage(encodeUsage(usage))
		}
		if cost, present := request.Cost.Get(); present {
			encoded.SetEstimatedCost(
				pb.EstimatedCost_builder{
					Input:      new(cost[0]),
					Output:     new(cost[1]),
					CacheRead:  new(cost[2]),
					CacheWrite: new(cost[3]),
					Total:      new(cost[4]),
				}.Build(),
			)
		}
		requests = append(requests, encoded)
	}
	source.SetModelRequests(pb.CompactionModelRequests_builder{Requests: requests}.Build())
	return source
}
