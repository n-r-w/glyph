//go:build !integration

package extension

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// compactionRequestValuesSuite checks transport preservation of extension-owned request values.
type compactionRequestValuesSuite struct {
	// Suite supplies scenario assertions.
	suite.Suite
}

// TestCompactionRequestValues checks complete marker metadata through request encoding.
func TestCompactionRequestValues(t *testing.T) {
	t.Parallel()
	// Arrange isolated request and marker round-trip scenarios.
	// Act through public request and entry conversion.
	// Assert preserved accounting and detached metadata in each scenario.
	suite.Run(t, &compactionRequestValuesSuite{Suite: suite.Suite{}})
}

// TestPreviousMetadata checks that request replacement retains preceding marker details and accounting.
func (s *compactionRequestValuesSuite) TestPreviousMetadata() {
	const providerID = "summary-provider"
	const modelID = "summary-model"

	t := s.T()
	// Arrange an original request with extension-owned opaque metadata and persisted cost.
	previous := session.CompactionEntry{
		Summary:          "preceding summary",
		FirstKeptEntryID: "retained",
		Source: session.CompactionSource{
			ExtensionID: mo.None[string](),
			Requests: []session.CompactionModelRequest{{
				Selection: model.Selection{
					Provider: providerID, Model: modelID, ReasoningChoice: model.ReasoningChoiceOff,
				},
				Usage: mo.Some(session.TokenUsage{
					InputTokens: 1, OutputTokens: 0, CacheReadTokens: 0,
					CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 1,
				}),
				EstimatedCost: mo.Some(session.EstimatedCost{
					Input: 0.25, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0.25,
				}),
			}},
		},
		Details: mo.Some([]byte("opaque preceding data")),
	}
	request := CompactionRequestValue{
		Trigger:                CompactionRequestManual,
		RetryIntent:            false,
		Instructions:           mo.None[string](),
		Model:                  model.Descriptor{},
		ReasoningChoice:        "",
		Prefix:                 nil,
		Suffix:                 nil,
		Previous:               mo.Some(previous),
		ContextTokens:          0,
		ContextTokensEstimated: true,
		ContextWindow:          0,
		ResponseBudget:         0,
		RetainedBudget:         0,
	}
	// Act through the public request representation and its decoder.
	wire, err := request.encodePolicyRequest()
	require.NoError(t, err)
	decoded, err := decodePolicyRequest(wire)
	incoming, sourceErr := mapPolicyCandidateSource(wire.GetPrevious().GetSource())
	// Assert incoming results omit supplied costs while preceding markers retain saved accounting.
	require.NoError(t, sourceErr)
	require.Len(t, incoming.Requests, 1)
	require.Equal(t, previous.Source.Requests[0].Selection, incoming.Requests[0].Selection)
	require.Equal(t, previous.Source.Requests[0].Usage, incoming.Requests[0].Usage)
	require.True(t, incoming.Requests[0].EstimatedCost.IsNone())
	// Assert complete preceding state and detached mutable details.
	require.NoError(t, err)
	require.Equal(t, previous, decoded.Previous.MustGet())
	wire.GetPrevious().GetDetails()[0] = 'X'
	require.Equal(t, []byte("opaque preceding data"), previous.Details.MustGet())
	require.Equal(t, previous, decoded.Previous.MustGet())
}

// TestEntryMarkerMetadata checks metadata preservation for marker entries in composed requests.
func (s *compactionRequestValuesSuite) TestEntryMarkerMetadata() {
	t := s.T()
	// Arrange a public compaction-marker entry with independent metadata.
	previous := session.CompactionEntry{
		Summary:          "preceding summary",
		FirstKeptEntryID: "retained",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("producer"),
			Requests:    nil,
		},

		Details: mo.Some([]byte("opaque preceding data")),
	}
	// Act through the preceding marker's public representation.
	wire := MapCompactionMarker(previous)
	decoded := DecodeCompactionPrevious(wire)
	// Assert the marker's complete state.
	require.Equal(t, previous, decoded.MustGet())
}
