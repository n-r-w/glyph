//go:build !integration

package session

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestBranchSummarySourceValidation checks producer identity and nested usage without a model catalogue.
func TestBranchSummarySourceValidation(t *testing.T) {
	t.Parallel()

	// Arrange valid extension and model alternatives, including an unconfigured model.
	modelSource := BranchSummaryModelSource{
		Selection: model.Selection{
			Provider:        "external",
			Model:           "unconfigured",
			ReasoningChoice: model.ReasoningChoiceOff,
		},
		Usage: mo.None[TokenUsage](),
	}
	tests := []struct {
		name   string
		source BranchSummarySource
		valid  bool
	}{
		{
			name:   "extension",
			source: BranchSummarySource{ExtensionID: mo.Some("producer"), Model: mo.None[BranchSummaryModelSource]()},
			valid:  true,
		},
		{
			name:   "model without usage",
			source: BranchSummarySource{ExtensionID: mo.None[string](), Model: mo.Some(modelSource)},
			valid:  true,
		},
		{name: "missing", source: BranchSummarySource{}, valid: false},
		{
			name:   "both",
			source: BranchSummarySource{ExtensionID: mo.Some("producer"), Model: mo.Some(modelSource)},
			valid:  false,
		},
		{
			name:   "blank extension",
			source: BranchSummarySource{ExtensionID: mo.Some("  "), Model: mo.None[BranchSummaryModelSource]()},
			valid:  false,
		},
		{
			name:   "empty model",
			source: BranchSummarySource{ExtensionID: mo.None[string](), Model: mo.Some(BranchSummaryModelSource{})},
			valid:  false,
		},
		{
			name: "invalid usage",
			source: BranchSummarySource{ExtensionID: mo.None[string](), Model: mo.Some(BranchSummaryModelSource{
				Selection: modelSource.Selection,
				Usage: mo.Some(
					TokenUsage{
						InputTokens:      -1,
						OutputTokens:     0,
						CacheReadTokens:  0,
						CacheWriteTokens: 0,
						ReasoningTokens:  0,
						TotalTokens:      -1,
					},
				),
			})},
			valid: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Act by validating only the result metadata.
			err := test.source.Validate()

			// Assert malformed metadata fails while historical source identity remains usable.
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
