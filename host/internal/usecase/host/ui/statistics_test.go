//go:build !integration

package ui

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestSessionInformationFrameCarriesInfoAndStatistics verifies /session keeps lifecycle fields with accounting.
func TestSessionInformationFrameCarriesInfoAndStatistics(t *testing.T) {
	t.Parallel()

	// Arrange active information, available counts, unavailable tokens, and available cost.
	info := testSessionInfo("active")
	statistics := session.Statistics{
		UserMessages: 1, ModelResponses: 2, ToolCalls: 3, ToolResults: 4, TotalMessages: 7,
		TokenUsage: mo.None[session.TokenUsage](),
		EstimatedCost: mo.Some(session.EstimatedCost{
			Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
		}),
		CostBreakdown: []session.ProviderModelCost{{
			Provider: "provider", Model: "model", EstimatedCost: mo.Some(session.EstimatedCost{
				Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
			}),
		}},
	}

	// Act by building the current session-information frame.
	frame := sessionInformationFrame(info, statistics)

	// Assert lifecycle information is unchanged and statistics are included independently.
	assert.Equal(t, info, frame.SessionInfo.OrEmpty())
	actual, present := frame.SessionStatistics.Get()
	assert.True(t, present)
	assert.Equal(t, statistics, actual)
}

// TestSessionInformationCommandMapsOneSnapshotWithoutAlteration verifies one query result becomes one frame.
func TestSessionInformationCommandMapsOneSnapshotWithoutAlteration(t *testing.T) {
	t.Parallel()

	// Arrange one information result with nonzero counts and token usage.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	control := NewMockSessionControl(controller)
	snapshot := session.InformationSnapshot{
		Info: testSessionInfo("active"),
		Statistics: session.Statistics{
			UserMessages: 2, ModelResponses: 3, ToolCalls: 4, ToolResults: 5, TotalMessages: 10,
			TokenUsage: mo.Some(session.TokenUsage{
				InputTokens: 6, OutputTokens: 7, CacheReadTokens: 8,
				CacheWriteTokens: 9, ReasoningTokens: 10, TotalTokens: 30,
			}),
			EstimatedCost: mo.Some(session.EstimatedCost{
				Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
			}),
			CostBreakdown: []session.ProviderModelCost{{
				Provider: "provider", Model: "model", EstimatedCost: mo.Some(session.EstimatedCost{
					Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
				}),
			}},
		},
	}
	control.EXPECT().Information().Return(snapshot)
	var frame domainui.Frame
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(value domainui.Frame) error {
		frame = value
		return nil
	})

	// Act by applying one session-information command.
	handled, err := NewSession(channel, nil, nil, nil, control, nil).applySessionCommand(
		t.Context(),
		testSessionCommand(domainui.CommandGetSessionInfo, mo.None[string](), mo.None[string]()),
	)

	// Assert the command performs one query and maps both values without alteration.
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, snapshot.Info, frame.SessionInfo.OrEmpty())
	assert.Equal(t, snapshot.Statistics, frame.SessionStatistics.OrEmpty())
}
