package sessions

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// encodeBranchSummarySource converts domain source metadata to its storage representation.
func encodeBranchSummarySource(source session.BranchSummarySource) branchSummarySourceRecord {
	// Preserve explicit alternative presence instead of inferring a producer from configuration.
	record := branchSummarySourceRecord{
		ExtensionID: source.ExtensionID,
		Model:       mo.None[branchSummaryModelSourceRecord](),
	}
	if value, present := source.Model.Get(); present {
		// A nil record distinguishes unreported usage from reported zero tokens.
		var usage *sessionUsageRecord
		if tokens, reported := value.Usage.Get(); reported {
			usage = &sessionUsageRecord{
				InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens,
				CacheReadTokens: tokens.CacheReadTokens, CacheWriteTokens: tokens.CacheWriteTokens,
				ReasoningTokens: tokens.ReasoningTokens, TotalTokens: tokens.TotalTokens,
			}
		}
		record.Model = mo.Some(branchSummaryModelSourceRecord{
			Provider: string(value.Selection.Provider), Model: string(value.Selection.Model),
			ReasoningChoice: value.Selection.ReasoningChoice, Usage: usage,
		})
	}
	return record
}

// decodeBranchSummarySource restores source metadata without consulting provider configuration.
func decodeBranchSummarySource(record branchSummarySourceRecord) session.BranchSummarySource {
	// Restore historical identity without accessing the model catalog.
	source := session.BranchSummarySource{
		ExtensionID: record.ExtensionID,
		Model:       mo.None[session.BranchSummaryModelSource](),
	}
	if value, present := record.Model.Get(); present {
		// Reported usage retains its presence independently of its numeric values.
		usage := mo.None[session.TokenUsage]()
		if value.Usage != nil {
			usage = mo.Some(session.TokenUsage{
				InputTokens: value.Usage.InputTokens, OutputTokens: value.Usage.OutputTokens,
				CacheReadTokens: value.Usage.CacheReadTokens, CacheWriteTokens: value.Usage.CacheWriteTokens,
				ReasoningTokens: value.Usage.ReasoningTokens, TotalTokens: value.Usage.TotalTokens,
			})
		}
		source.Model = mo.Some(session.BranchSummaryModelSource{
			Selection: model.Selection{
				Provider:        model.ProviderID(value.Provider),
				Model:           model.ID(value.Model),
				ReasoningChoice: value.ReasoningChoice,
			},
			Usage: usage,
		})
	}
	return source
}
