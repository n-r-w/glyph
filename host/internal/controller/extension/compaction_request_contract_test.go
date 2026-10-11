//go:build !integration

package extension

import (
	"testing"
	"time"

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
	t := s.T()
	// Arrange an original request with extension-owned opaque metadata and persisted cost.
	previous := session.CompactionEntry{
		Summary:          "preceding summary",
		FirstKeptEntryID: "retained",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("producer"),
			Model:       mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.Some(session.EstimatedCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10}),
		Details:       mo.Some([]byte("opaque preceding data")),
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
			Model:       mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.Some(session.EstimatedCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10}),
		Details:       mo.Some([]byte("opaque preceding data")),
	}
	entry := session.Entry{
		ID:               "marker",
		ParentID:         mo.None[string](),
		CreatedAt:        time.Time{},
		Information:      mo.None[session.Information](),
		User:             mo.None[session.UserMessage](),
		Model:            mo.None[session.ModelResponse](),
		ToolResult:       mo.None[session.ToolResult](),
		Extension:        mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
		BranchSummary:    mo.None[session.BranchSummaryEntry](),
		Compaction:       mo.Some(previous),
		EstimatedCost:    mo.None[session.EstimatedCost](),
	}
	// Act through the entry's public representation.
	wire, err := encodePolicyRequestEntry(entry)
	require.NoError(t, err)
	decoded, err := DecodeCompactionRequestEntry(wire)
	// Assert the marker's complete state.
	require.NoError(t, err)
	require.Equal(t, previous, decoded.Compaction.MustGet())
}
