package sessions

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestResumeProjectsOnlyRootFirstActiveBranch verifies provider history excludes abandoned siblings.
func TestResumeProjectsOnlyRootFirstActiveBranch(t *testing.T) {
	t.Parallel()

	// Arrange a stored tree whose active path differs from persistence order.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	createdAt := time.Unix(1, 0).UTC()
	root := treeBehaviorUserEntry("root", mo.None[string](), createdAt)
	abandoned := treeBehaviorUserEntry("abandoned", mo.Some("root"), createdAt.Add(time.Second))
	active := treeBehaviorUserEntry("active", mo.Some("root"), createdAt.Add(2*time.Second))
	activeModel := branchAccountingModelEntry(
		"active-model",
		mo.Some("active"),
		mo.Some(model.Usage{}),
		mo.None[session.EstimatedCost](),
	)
	tree, err := session.NewTree(
		[]session.Entry{root, abandoned, active, activeModel}, mo.Some("active-model"), nil,
	)
	require.NoError(t, err)
	repository.EXPECT().Load(gomock.Any(), session.ID("stored")).Return(LoadedSession{
		Header: session.Header{
			Version:          formatVersion,
			ID:               "stored",
			CreatedAt:        createdAt,
			WorkingDirectory: "/project",
		},
		StoragePath:          "/sessions/stored.jsonl",
		Tree:                 tree,
		Information:          mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}, nil)
	service := New(repository, nil, nil, nil, "/project")

	// Act by resuming the stored session and requesting provider-neutral history.
	_, err = service.ResumeActive(t.Context(), "stored")
	require.NoError(t, err)
	history := service.Snapshot()

	// Assert history is root-first and contains only the active branch.
	require.Len(t, history, 3)
	assert.Equal(t, "root", history[0].User.MustGet().Content[0].Text.MustGet())
	assert.Equal(t, "active", history[1].User.MustGet().Content[0].Text.MustGet())
	assert.True(t, history[2].Model.IsSome())
}

// TestStatisticsAndStoredSummaryCountAllBranches verifies complete-session accounting ignores active-path selection.
func TestStatisticsAndStoredSummaryCountAllBranches(t *testing.T) {
	t.Parallel()

	// Arrange a tree with one model response on each sibling branch.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	createdAt := time.Unix(1, 0).UTC()
	root := treeBehaviorUserEntry("root", mo.None[string](), createdAt)
	abandoned := branchAccountingModelEntry(
		"abandoned", mo.Some("root"), mo.Some(model.Usage{}), mo.None[session.EstimatedCost](),
	)
	active := branchAccountingModelEntry(
		"active", mo.Some("root"), mo.Some(model.Usage{}), mo.None[session.EstimatedCost](),
	)
	tree, err := session.NewTree([]session.Entry{root, abandoned, active}, mo.Some("active"), nil)
	require.NoError(t, err)
	loaded := LoadedSession{
		Header: session.Header{
			Version:          formatVersion,
			ID:               "stored",
			CreatedAt:        createdAt,
			WorkingDirectory: "/project",
		},
		StoragePath:          "/sessions/stored.jsonl",
		Tree:                 tree,
		Information:          mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}
	repository.EXPECT().List(gomock.Any()).Return([]LoadedSession{loaded}, nil)
	service := New(repository, nil, nil, nil, "/project")
	service.active = loaded

	// Act by requesting active-session statistics and stored-session summaries.
	statistics := service.ActiveStatistics()
	information := service.ActiveInformation()
	stored, err := service.ListStored(t.Context())
	require.NoError(t, err)

	// Assert both complete-session views count the abandoned branch while active entries remain branch-only.
	assert.Equal(t, 1, statistics.UserMessages)
	assert.Equal(t, 2, statistics.ModelResponses)
	assert.Equal(t, 3, statistics.TotalMessages)
	assert.Equal(t, statistics, information.Statistics)
	require.Len(t, stored, 1)
	assert.Equal(t, 3, stored[0].TotalMessages)
	assert.Len(t, service.ActiveEntries(), 2)
}

// TestBranchSummaryContributesAccountingWithoutMessageCounts verifies summary usage and cost use their own identity.
func TestBranchSummaryContributesAccountingWithoutMessageCounts(t *testing.T) {
	t.Parallel()

	// Arrange one ordinary model response and one branch summary with complete accounting.
	modelCost := session.EstimatedCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10}
	summaryCost := session.EstimatedCost{Input: 5, Output: 6, CacheRead: 7, CacheWrite: 8, Total: 26}
	entries := []session.Entry{
		branchAccountingModelEntry("response", mo.None[string](), mo.Some(model.Usage{
			InputTokens: 1, OutputTokens: 2, CachedInputTokens: 3,
			CacheWriteTokens: 4, ReasoningTokens: 1, TotalTokens: 10,
		}), mo.Some(modelCost)),
		branchAccountingSummaryEntry(mo.Some(session.TokenUsage{
			InputTokens: 5, OutputTokens: 6, CacheReadTokens: 7,
			CacheWriteTokens: 8, ReasoningTokens: 2, TotalTokens: 26,
		}), mo.Some(summaryCost)),
	}

	// Act by deriving complete-session statistics.
	statistics := statisticsFromEntries(entries)

	// Assert the summary adds usage and cost but no message or tool count.
	assert.Equal(t, 0, statistics.UserMessages)
	assert.Equal(t, 1, statistics.ModelResponses)
	assert.Equal(t, 0, statistics.ToolCalls)
	assert.Equal(t, 0, statistics.ToolResults)
	assert.Equal(t, 1, statistics.TotalMessages)
	assert.Equal(t, mo.Some(session.TokenUsage{
		InputTokens: 6, OutputTokens: 8, CacheReadTokens: 10,
		CacheWriteTokens: 12, ReasoningTokens: 3, TotalTokens: 36,
	}), statistics.TokenUsage)
	assert.Equal(t, mo.Some(session.EstimatedCost{
		Input: 6, Output: 8, CacheRead: 10, CacheWrite: 12, Total: 36,
	}), statistics.EstimatedCost)
	require.Len(t, statistics.CostBreakdown, 2)
	assert.Equal(t, model.ProviderID("provider"), statistics.CostBreakdown[0].Provider)
	assert.Equal(t, model.ID("model"), statistics.CostBreakdown[0].Model)
	assert.Equal(t, mo.Some(modelCost), statistics.CostBreakdown[0].EstimatedCost)
	assert.Equal(t, model.ProviderID("summary-provider"), statistics.CostBreakdown[1].Provider)
	assert.Equal(t, model.ID("summary-model"), statistics.CostBreakdown[1].Model)
	assert.Equal(t, mo.Some(summaryCost), statistics.CostBreakdown[1].EstimatedCost)
}

// TestBranchSummaryMissingAccountingPreservesCompleteTotalRules verifies absence invalidates only its matching total.
func TestBranchSummaryMissingAccountingPreservesCompleteTotalRules(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		usage        mo.Option[session.TokenUsage]
		cost         mo.Option[session.EstimatedCost]
		usagePresent bool
		costPresent  bool
	}{
		{
			name:         "missing usage",
			usage:        mo.None[session.TokenUsage](),
			cost:         mo.Some(session.EstimatedCost{}),
			usagePresent: false,
			costPresent:  true,
		},
		{
			name:         "missing cost",
			usage:        mo.Some(session.TokenUsage{}),
			cost:         mo.None[session.EstimatedCost](),
			usagePresent: true,
			costPresent:  false,
		},
	}
	for index := range testCases {
		testCase := testCases[index]
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one branch summary with one absent accounting dimension.
			entries := []session.Entry{branchAccountingSummaryEntry(testCase.usage, testCase.cost)}

			// Act by deriving complete-session statistics.
			statistics := statisticsFromEntries(entries)

			// Assert absence affects only the corresponding complete total and provider-model group.
			assert.Equal(t, testCase.usagePresent, statistics.TokenUsage.IsSome())
			assert.Equal(t, testCase.costPresent, statistics.EstimatedCost.IsSome())
			require.Len(t, statistics.CostBreakdown, 1)
			assert.Equal(t, testCase.costPresent, statistics.CostBreakdown[0].EstimatedCost.IsSome())
			assert.Zero(t, statistics.TotalMessages)
		})
	}
}

// branchAccountingModelEntry creates one terminal model entry with explicit accounting state.
func branchAccountingModelEntry(
	id string,
	parentID mo.Option[string],
	usage mo.Option[model.Usage],
	cost mo.Option[session.EstimatedCost],
) session.Entry {
	return session.Entry{
		ID: id, ParentID: parentID, CreatedAt: time.Unix(2, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.Some(model.Response{
			Content: nil, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
			Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
			ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: usage, Diagnostics: nil,
		}),
		EstimatedCost: cost, ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
}

// branchAccountingSummaryEntry creates one summary entry with explicit accounting state.
func branchAccountingSummaryEntry(
	usage mo.Option[session.TokenUsage],
	cost mo.Option[session.EstimatedCost],
) session.Entry {
	return session.Entry{
		ID:            "summary",
		ParentID:      mo.None[string](),
		CreatedAt:     time.Unix(3, 0).UTC(),
		Information:   mo.None[session.Information](),
		User:          mo.None[session.UserMessage](),
		Model:         mo.None[session.ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult:    mo.None[session.ToolResult](),
		Extension:     mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.Some(session.BranchSummaryEntry{
			Summary: "summary", FirstEntryID: "first", LastEntryID: "last",
			Provider: "summary-provider", Model: "summary-model", ReasoningChoice: model.ReasoningChoiceOff,
			Usage: usage, EstimatedCost: cost,
		}),
	}
}
