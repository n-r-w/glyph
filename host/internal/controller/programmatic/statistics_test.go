package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapOpenRequestAcceptsSessionStatisticsQuery verifies the public command reaches the Host model.
func TestMapOpenRequestAcceptsSessionStatisticsQuery(t *testing.T) {
	t.Parallel()

	// Arrange a correlated GetSessionStats request.
	//nolint:exhaustruct // The protobuf builder intentionally sets only the active oneof field.
	request := programmaticv1.OpenRequest_builder{
		CorrelationId: new("stats"), GetSessionStats: programmaticv1.GetSessionStats_builder{}.Build(),
	}.Build()

	// Act by mapping the public request.
	command, err := mapOpenRequest(request)

	// Assert the statistics command and correlation ID are preserved.
	require.NoError(t, err)
	assert.Equal(t, CommandGetSessionStats, command.Kind)
	assert.Equal(t, "stats", command.CorrelationID)
}

// TestMapResponsePreservesSessionStatisticsAvailability verifies counts and optional tokens map independently.
func TestMapResponsePreservesSessionStatisticsAvailability(t *testing.T) {
	t.Parallel()

	// Arrange complete counts with unavailable tokens.
	statistics := session.Statistics{
		UserMessages: 2, ModelResponses: 3, ToolCalls: 4, ToolResults: 5, TotalMessages: 10,
		TokenUsage: mo.None[session.TokenUsage](),
	}
	response := Response{
		CorrelationID: "stats", Kind: ResponseSessionStats,
		State: mo.None[RunStateResult](), Messages: nil, Models: mo.None[ModelsResult](),
		Selection: mo.None[model.Selection](), SessionInfo: mo.None[session.Info](), Sessions: nil,
		SessionEntries: nil, SessionStatistics: mo.Some(statistics), Rejection: mo.None[Rejection](),
	}

	// Act by mapping the Host response to protobuf.
	wire, err := mapResponse(response)

	// Assert counts remain present and the token message remains absent.
	require.NoError(t, err)
	mapped := wire.GetCommandResponse().GetSessionStats().GetStatistics()
	assert.Equal(t, int64(2), mapped.GetUserMessages())
	assert.Equal(t, int64(3), mapped.GetModelResponses())
	assert.Equal(t, int64(4), mapped.GetToolCalls())
	assert.Equal(t, int64(5), mapped.GetToolResults())
	assert.Equal(t, int64(10), mapped.GetTotalMessages())
	assert.False(t, mapped.HasTokens())
}
