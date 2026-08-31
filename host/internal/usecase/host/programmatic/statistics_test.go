//go:build !integration

package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestSessionStatisticsQueryReturnsSnapshotDuringActiveRun verifies statistics are an immediate query.
func TestSessionStatisticsQueryReturnsSnapshotDuringActiveRun(t *testing.T) {
	t.Parallel()

	// Arrange an active-run query and a consumer-owned session control expectation.
	control := NewMockSessionControl(gomock.NewController(t))
	statistics := session.Statistics{
		UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
		TokenUsage: mo.Some(session.TokenUsage{}),
		EstimatedCost: mo.Some(session.EstimatedCost{
			Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
		}),
		CostBreakdown: []session.ProviderModelCost{{
			Provider: "provider", Model: "model", EstimatedCost: mo.Some(session.EstimatedCost{
				Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
			}),
		}},
	}
	control.EXPECT().Statistics().Return(statistics)
	service := New(nil, nil, nil, nil, control, nil)
	command := testProgrammaticCommand("stats", controller.CommandGetSessionStats)

	// Act by handling the query while an active run marker is present.
	response, handled, err := service.handleImmediate(t.Context(), command, new(activeRun))

	// Assert the query is handled without run coordination and returns the snapshot.
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, controller.ResponseSessionStats, response.Kind)
	actual, present := response.SessionStatistics.Get()
	assert.True(t, present)
	assert.Equal(t, statistics, actual)
}
