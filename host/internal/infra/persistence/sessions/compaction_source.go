package sessions

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// compactionSourceRecord contains information about a summary's producer.
type compactionSourceRecord struct {
	// ExtensionID identifies the extension-only producer.
	ExtensionID mo.Option[string] `json:"extension_id"`
	// Requests contains ordered model request accounting.
	Requests []compactionModelRequestRecord `json:"requests"`
}

// encodeCompactionSource converts contributing request accounting to storage records.
func encodeCompactionSource(source session.CompactionSource) compactionSourceRecord {
	record := compactionSourceRecord{ExtensionID: source.ExtensionID, Requests: nil}
	if source.Requests != nil {
		record.Requests = make([]compactionModelRequestRecord, len(source.Requests))
	}
	for index := range source.Requests {
		request := &source.Requests[index]
		value := compactionModelRequestRecord{
			Provider: string(request.Selection.Provider), Model: string(request.Selection.Model),
			ReasoningChoice: request.Selection.ReasoningChoice, Usage: nil, EstimatedCost: nil,
		}
		if usage, present := request.Usage.Get(); present {
			value.Usage = &sessionUsageRecord{
				InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
				CacheReadTokens: usage.CacheReadTokens, CacheWriteTokens: usage.CacheWriteTokens,
				ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
			}
		}
		if cost, present := request.EstimatedCost.Get(); present {
			value.EstimatedCost = &estimatedCostRecord{
				Input: new(cost.Input), Output: new(cost.Output), CacheRead: new(cost.CacheRead),
				CacheWrite: new(cost.CacheWrite), Total: new(cost.Total),
			}
		}
		record.Requests[index] = value
	}
	return record
}

// decode restores saved request accounting.
func (record compactionSourceRecord) decode() (session.CompactionSource, error) {
	source := session.CompactionSource{ExtensionID: record.ExtensionID, Requests: nil}
	if record.Requests != nil {
		source.Requests = make([]session.CompactionModelRequest, len(record.Requests))
	}
	for index, value := range record.Requests {
		cost, err := decodeEstimatedCost(value.EstimatedCost)
		if err != nil {
			return session.CompactionSource{}, err
		}
		request := session.CompactionModelRequest{
			Selection: model.Selection{
				Provider:        model.ProviderID(value.Provider),
				Model:           model.ID(value.Model),
				ReasoningChoice: value.ReasoningChoice,
			},
			Usage:         mo.None[session.TokenUsage](),
			EstimatedCost: cost,
		}
		if usage := value.Usage; usage != nil {
			request.Usage = mo.Some(session.TokenUsage{
				InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
				CacheReadTokens: usage.CacheReadTokens, CacheWriteTokens: usage.CacheWriteTokens,
				ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
			})
		}
		source.Requests[index] = request
	}
	return source, nil
}
