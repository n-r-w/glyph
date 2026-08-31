//go:build !integration

package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// TestCommitNavigationBuildsAndPersistsOneValidatedSummaryMutation verifies atomic summary ownership and accounting.
func TestCommitNavigationBuildsAndPersistsOneValidatedSummaryMutation(t *testing.T) {
	t.Parallel()

	// Arrange strict storage, identity, clock, and pricing dependencies for one summary commit.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	pricing := NewMockPricingCatalog(controller)
	createdAt := time.Unix(1, 0).UTC()
	service := New(repository, ids, clock, pricing, "/project")
	service.active = commitNavigationLoadedSession(commitNavigationTree(t, createdAt), createdAt)
	selection := model.Selection{
		Provider:        "provider",
		Model:           "summary-model",
		ReasoningChoice: model.ReasoningChoiceHigh,
	}
	usage := session.TokenUsage{
		InputTokens:      5,
		OutputTokens:     4,
		CacheReadTokens:  3,
		CacheWriteTokens: 2,
		ReasoningTokens:  1,
		TotalTokens:      14,
	}
	ids.EXPECT().NewID().Return("summary-entry", nil)
	clock.EXPECT().Now().Return(createdAt.Add(3 * time.Second))
	pricing.EXPECT().Pricing(selection.Provider, selection.Model).Return(mo.Some(model.Pricing{
		Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Tiers: nil,
	}))
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			assert.True(t, command.Mutation.Entry.IsNone())
			navigation := command.Mutation.Navigation.MustGet()
			assert.Equal(t, mo.Some("destination"), navigation.DestinationID)
			require.True(t, navigation.BranchSummary.IsSome())
			entry := navigation.BranchSummary.OrEmpty()
			assert.Equal(t, "summary-entry", entry.ID)
			assert.Equal(t, mo.Some("destination"), entry.ParentID)
			summary := entry.BranchSummary.MustGet()
			assert.Equal(t, "generated", summary.Summary)
			assert.Equal(t, "abandoned", summary.FirstEntryID)
			assert.Equal(t, "abandoned", summary.LastEntryID)
			assert.Equal(t, selection.Provider, summary.Provider)
			assert.Equal(t, selection.Model, summary.Model)
			assert.Equal(t, selection.ReasoningChoice, summary.ReasoningChoice)
			assert.Equal(t, mo.Some(usage), summary.Usage)
			cost := summary.EstimatedCost.MustGet()
			assert.InDelta(t, 0.000005, cost.Input, 1e-12)
			assert.InDelta(t, 0.000008, cost.Output, 1e-12)
			assert.InDelta(t, 0.000009, cost.CacheRead, 1e-12)
			assert.InDelta(t, 0.000008, cost.CacheWrite, 1e-12)
			assert.InDelta(t, 0.00003, cost.Total, 1e-12)
			return ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil
		},
	)

	// Act by committing navigation with a validated summary draft.
	committed, err := service.CommitNavigation(t.Context(), sessiontree.CommitCommand{
		ExpectedActiveLeafID: mo.Some("abandoned"), DestinationID: mo.Some("destination"),
		BranchSummary: mo.Some(sessiontree.BranchSummaryDraft{
			Summary: "generated", FirstEntryID: "abandoned", LastEntryID: "abandoned",
			CommonAncestorID: mo.Some("destination"), Selection: selection, Usage: mo.Some(usage),
		}),
	})

	// Assert the summary becomes the published active leaf only after persistence succeeds.
	require.NoError(t, err)
	assert.Equal(t, mo.Some("summary-entry"), committed.ActiveLeafID())
	assert.Equal(t, mo.Some("summary-entry"), service.Tree().ActiveLeafID())
	require.Len(t, committed.ActiveBranch(), 3)
	assert.Equal(t, "summary-entry", committed.ActiveBranch()[2].ID)
}

// TestCommitNavigationKeepsUsageAndCostAbsentWhenProviderUsageIsAbsent verifies missing accounting stays missing.
func TestCommitNavigationKeepsUsageAndCostAbsentWhenProviderUsageIsAbsent(t *testing.T) {
	t.Parallel()

	// Arrange strict dependencies with no pricing expectation because provider usage is absent.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	pricing := NewMockPricingCatalog(controller)
	createdAt := time.Unix(1, 0).UTC()
	service := New(repository, ids, clock, pricing, "/project")
	service.active = commitNavigationLoadedSession(commitNavigationTree(t, createdAt), createdAt)
	ids.EXPECT().NewID().Return("summary-entry", nil)
	clock.EXPECT().Now().Return(createdAt.Add(3 * time.Second))
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			summary := command.Mutation.Navigation.OrEmpty().BranchSummary.OrEmpty().BranchSummary.OrEmpty()
			assert.True(t, summary.Usage.IsNone())
			assert.True(t, summary.EstimatedCost.IsNone())
			return ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil
		},
	)

	// Act by committing a generated summary without usage.
	_, err := service.CommitNavigation(t.Context(), sessiontree.CommitCommand{
		ExpectedActiveLeafID: mo.Some("abandoned"), DestinationID: mo.Some("destination"),
		BranchSummary: mo.Some(sessiontree.BranchSummaryDraft{
			Summary:          "generated",
			FirstEntryID:     "abandoned",
			LastEntryID:      "abandoned",
			CommonAncestorID: mo.Some("destination"),
			Selection: model.Selection{
				Provider:        "provider",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceOff,
			},
			Usage: mo.None[session.TokenUsage](),
		}),
	})

	// Assert persistence succeeds without inventing usage or cost.
	require.NoError(t, err)
}
