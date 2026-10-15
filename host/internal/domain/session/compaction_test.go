//go:build !integration

package session

import (
	"math"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestCompactionSourceValidatesEveryRequest checks producer exclusivity and individual accounting consistency.
func TestCompactionSourceValidatesEveryRequest(t *testing.T) {
	t.Parallel()
	// Arrange ordered request records with independently optional accounting.
	request := CompactionModelRequest{
		Selection: model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff},
		Usage:     mo.Some(TokenUsage{}), EstimatedCost: mo.Some(EstimatedCost{}),
	}
	tests := []struct {
		name          string
		source        CompactionSource
		expectedError string
	}{
		{
			name:          "extension only",
			source:        CompactionSource{ExtensionID: mo.Some("extension"), Requests: nil},
			expectedError: "",
		},
		{
			name: "zero usage and cost",
			source: CompactionSource{
				ExtensionID: mo.None[string](),
				Requests:    []CompactionModelRequest{request},
			},
			expectedError: "",
		},
		{
			name:          "missing source",
			source:        CompactionSource{},
			expectedError: "requires an extension source or contributing model requests",
		},
		{
			name:          "empty request collection",
			source:        CompactionSource{ExtensionID: mo.None[string](), Requests: []CompactionModelRequest{}},
			expectedError: "requires an extension source or contributing model requests",
		},
		{
			name:          "empty extension",
			source:        CompactionSource{ExtensionID: mo.Some(" "), Requests: nil},
			expectedError: "extension source is empty",
		},
		{
			name: "mixed sources",
			source: CompactionSource{
				ExtensionID: mo.Some("extension"),
				Requests:    []CompactionModelRequest{request},
			},
			expectedError: "requires an extension source or contributing model requests",
		},
	}
	for _, name := range []string{"missing usage", "missing pricing", "invalid selection", "invalid usage", "cost without usage", "invalid cost"} {
		second := request
		expectedError := ""
		switch name {
		case "missing usage":
			second.Usage = mo.None[TokenUsage]()
			second.EstimatedCost = mo.None[EstimatedCost]()
		case "missing pricing":
			second.EstimatedCost = mo.None[EstimatedCost]()
		case "invalid selection":
			second.Selection.Model = ""
			expectedError = "compaction request 1: compaction model selection is invalid"
		case "invalid usage":
			second.Usage = mo.Some(
				TokenUsage{
					InputTokens:      -1,
					OutputTokens:     0,
					CacheReadTokens:  0,
					CacheWriteTokens: 0,
					ReasoningTokens:  0,
					TotalTokens:      -1,
				},
			)
			expectedError = "compaction request 1: compaction model usage is invalid"
		case "cost without usage":
			second.Usage = mo.None[TokenUsage]()
			expectedError = "compaction request 1: compaction cost requires reported model usage"
		case "invalid cost":
			second.EstimatedCost = mo.Some(
				EstimatedCost{Input: math.NaN(), Output: 0, CacheRead: 0, CacheWrite: 0, Total: math.NaN()},
			)
			expectedError = "compaction request 1: compaction estimated cost is invalid"
		}
		tests = append(tests, struct {
			name          string
			source        CompactionSource
			expectedError string
		}{
			name: name,
			source: CompactionSource{
				ExtensionID: mo.None[string](),
				Requests:    []CompactionModelRequest{request, second},
			},
			expectedError: expectedError,
		})
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Act through the producer-accounting boundary.
			err := testCase.source.Validate()
			// Assert every contributing record follows the accounting contract.
			if testCase.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, testCase.expectedError)
			}
		})
	}
}
