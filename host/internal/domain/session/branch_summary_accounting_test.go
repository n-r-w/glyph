//go:build !integration

package session

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestSummaryCostRequiresModelUsage rejects cost on extension output or an unreported model execution.
func TestSummaryCostRequiresModelUsage(t *testing.T) {
	t.Parallel()

	// Arrange source alternatives with absent and present-zero reported usage.
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	tests := []struct {
		name   string
		source BranchSummarySource
		cost   mo.Option[EstimatedCost]
		valid  bool
	}{
		{
			name:   "extension without cost",
			source: BranchSummarySource{ExtensionID: mo.Some("producer"), Model: mo.None[BranchSummaryModelSource]()},
			cost:   mo.None[EstimatedCost](),
			valid:  true,
		},
		{
			name:   "extension with cost",
			source: BranchSummarySource{ExtensionID: mo.Some("producer"), Model: mo.None[BranchSummaryModelSource]()},
			cost:   mo.Some(EstimatedCost{}),
			valid:  false,
		},
		{
			name: "unreported usage with cost",
			source: BranchSummarySource{
				ExtensionID: mo.None[string](),
				Model:       mo.Some(BranchSummaryModelSource{Selection: selection, Usage: mo.None[TokenUsage]()}),
			},
			cost:  mo.Some(EstimatedCost{}),
			valid: false,
		},
		{
			name: "zero reported usage",
			source: BranchSummarySource{
				ExtensionID: mo.None[string](),
				Model:       mo.Some(BranchSummaryModelSource{Selection: selection, Usage: mo.Some(TokenUsage{})}),
			},
			cost:  mo.Some(EstimatedCost{}),
			valid: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			summary := BranchSummaryEntry{
				Summary:       "context",
				FirstEntryID:  "first",
				LastEntryID:   "last",
				Source:        test.source,
				EstimatedCost: test.cost,
			}

			// Act by validating the complete persisted accounting relationship.
			err := summary.ValidateAccounting()

			// Assert absent accounting stays optional while cost requires actual model usage.
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
