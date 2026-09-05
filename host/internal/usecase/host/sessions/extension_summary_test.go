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

// TestCommitExtensionSummaryPreservesSource verifies an extension summary commits without model pricing.
func TestCommitExtensionSummaryPreservesSource(t *testing.T) {
	t.Parallel()

	// Arrange one abandoned branch and no pricing expectation for the extension result.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	pricing := NewMockPricingCatalog(controller)
	createdAt := time.Unix(1, 0).UTC()
	service := New(repository, ids, clock, pricing, "/project")
	service.active = commitNavigationLoadedSession(commitNavigationTree(t, createdAt), createdAt)
	source := session.BranchSummarySource{
		ExtensionID: mo.Some("producer"),
		Model:       mo.None[session.BranchSummaryModelSource](),
	}
	ids.EXPECT().NewID().Return("summary", nil)
	clock.EXPECT().Now().Return(createdAt)
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			summary := command.Mutation.Navigation.OrEmpty().BranchSummary.OrEmpty().BranchSummary.OrEmpty()
			assert.Equal(t, source, summary.Source)
			assert.True(t, summary.EstimatedCost.IsNone())
			return ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil
		},
	)

	// Act by atomically committing the extension result with navigation.
	tree, err := service.CommitNavigation(t.Context(), sessiontree.CommitCommand{
		ExpectedActiveLeafID: mo.Some("abandoned"), DestinationID: mo.Some("destination"),
		BranchSummary: mo.Some(sessiontree.BranchSummaryDraft{
			Summary: "extension context", FirstEntryID: "abandoned", LastEntryID: "abandoned",
			CommonAncestorID: mo.Some("destination"), Source: source,
		}),
	})

	// Assert the persisted result becomes the active leaf.
	require.NoError(t, err)
	assert.Equal(t, mo.Some("summary"), tree.ActiveLeafID())
}

// TestExtensionSummaryKeepsModelTotalsComplete verifies non-model work does not make accounting incomplete.
func TestExtensionSummaryKeepsModelTotalsComplete(t *testing.T) {
	t.Parallel()

	// Arrange a fully accounted model response and one extension-source summary.
	response := branchAccountingModelEntry(
		"response",
		mo.None[string](),
		mo.Some(model.Usage{}),
		mo.Some(session.EstimatedCost{}),
	)
	entry := branchAccountingSummaryEntry(mo.None[session.TokenUsage](), mo.None[session.EstimatedCost]())
	summary := entry.BranchSummary.OrEmpty()
	summary.Source = session.BranchSummarySource{
		ExtensionID: mo.Some("producer"),
		Model:       mo.None[session.BranchSummaryModelSource](),
	}
	entry.BranchSummary = mo.Some(summary)
	before := statisticsFromEntries([]session.Entry{response})

	// Act by adding non-model summary work to session statistics.
	after := statisticsFromEntries([]session.Entry{response, entry})

	// Assert complete model totals and groups are retained without charging the extension result.
	assert.Equal(t, before, after)
}
