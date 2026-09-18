//go:build !integration

package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
)

// TestCommitCompactionPersistsProjectsPublishesAndAccountsOnce verifies the complete session-owned commit boundary.
func TestCommitCompactionPersistsProjectsPublishesAndAccountsOnce(t *testing.T) {
	// Arrange one active branch ending in a complete model tool-call group.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	publisher := NewMockEntryPublisher(controller)
	entries := []session.Entry{
		compactionUserEntry("u1", mo.None[string](), "old"),
		compactionModelEntry("m1", "u1", "call"),
		compactionToolEntry("r1", "m1", "call"),
	}
	response := entries[1].Model.MustGet()
	response.Usage = mo.Some(model.Usage{
		InputTokens: 6, OutputTokens: 4, CachedInputTokens: 0,
		CacheWriteTokens: 0, ReasoningTokens: 1, TotalTokens: 10,
	})
	entries[1].Model = mo.Some(response)
	tree, err := session.NewTree(entries, mo.Some("r1"), nil)
	require.NoError(t, err)
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			persisted := command.Mutation.Entry.MustGet()
			require.Equal(t, "r1", persisted.ParentID.MustGet())
			require.Equal(t, "summary", persisted.Compaction.MustGet().Summary)
			return ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil
		},
	)
	ids.EXPECT().NewID().Return("c1", nil)
	clock.EXPECT().Now().Return(time.Unix(10, 0).UTC())
	publisher.EXPECT().PublishSessionEntry(gomock.Any()).DoAndReturn(
		func(entry session.Entry) (func(context.Context) error, error) {
			require.Equal(t, "c1", entry.ID)
			return func(context.Context) error { return nil }, nil
		},
	)
	service := New(repository, ids, clock, nil, "/project")
	service.active = LoadedSession{
		Header:      session.Header{ID: "session", CreatedAt: time.Unix(1, 0).UTC(), WorkingDirectory: "/project"},
		StoragePath: "/sessions/session.jsonl", Tree: tree,
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}
	identity := contextcompaction.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 2}
	service.contextIdentity.Store(&identity)
	service.history = storedHistoryFromEntries(entries)
	service.contextHistory = storedCompactedHistoryFromEntries(entries)
	service.BindEntryPublisher(publisher)
	compaction := session.CompactionEntry{
		Summary: "summary", FirstKeptEntryID: "m1",
		Source: session.CompactionSource{
			ExtensionID: mo.None[string](), Model: mo.Some(session.BranchSummaryModelSource{
				Selection: model.Selection{
					Provider: "summary-provider", Model: "summary-model", ReasoningChoice: model.ReasoningChoiceLow,
				},
				Usage: mo.Some(session.TokenUsage{
					InputTokens: 3, OutputTokens: 2, CacheReadTokens: 0,
					CacheWriteTokens: 0, ReasoningTokens: 1, TotalTokens: 5,
				}),
			}),
		},
		EstimatedCost: mo.Some(session.EstimatedCost{
			Input: 0.03, Output: 0.02, CacheRead: 0, CacheWrite: 0, Total: 0.05,
		}),
		Details: mo.Some([]byte("details")),
	}

	// Act by committing one compaction against the captured incarnation and active leaf.
	committed, err := service.CommitCompaction(
		t.Context(), identity, mo.Some("r1"), compaction,
	)

	// Assert persistence and publication completed with one durable marker.
	require.NoError(t, err)
	require.Equal(t, "c1", committed.ID)
	require.Len(t, service.ActiveEntries(), 4)

	// Assert model context is compacted while client history retains every original record.
	modelHistory := service.Snapshot()
	require.Len(t, modelHistory, 3)
	require.Contains(t, modelHistory[0].User.MustGet().Text(""), "summary")
	require.Equal(t, "call", modelHistory[2].ToolResult.MustGet().CallID)
	require.Len(t, service.ClientSnapshot(), 3)

	// Assert persisted model and compaction usage are counted exactly once.
	statistics := service.ActiveStatistics()
	require.Equal(t, int64(15), statistics.TokenUsage.MustGet().TotalTokens)
}

// TestCommitCompactionRejectsBoundaryInsideToolGroup verifies a tool result cannot start the retained suffix.
func TestCommitCompactionRejectsBoundaryInsideToolGroup(t *testing.T) {
	t.Parallel()

	// Arrange one active complete tool group with a hidden entry between its call and result.
	hidden := compactionBaseEntry("x1", mo.Some("m1"))
	hidden.Extension = mo.Some(session.ExtensionEnvelope{
		ExtensionID: "extension", EntryType: "hidden", Data: []byte(`{"state":true}`),
	})
	entries := []session.Entry{
		compactionUserEntry("u1", mo.None[string](), "old"),
		compactionModelEntry("m1", "u1", "call"),
		hidden,
		compactionToolEntry("r1", "x1", "call"),
	}
	service, identity := newCompactionBoundaryTestService(t, entries, "r1")

	// Act by selecting the hidden entry between the owning model response and its result.
	_, err := service.CommitCompaction(
		t.Context(), identity, mo.Some("r1"),
		session.CompactionEntry{
			Summary: "summary", FirstKeptEntryID: "x1",
			Source: session.CompactionSource{
				ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
			},
			EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
		},
	)

	// Assert validation fails before any storage or publication work.
	require.Error(t, err)
	require.Contains(t, err.Error(), "tool")
	require.Len(t, service.ActiveEntries(), 4)
}

// TestCommitCompactionAssociatesRepeatedToolIDsWithTheirModelResponses verifies exact retained turn ownership.
func TestCommitCompactionAssociatesRepeatedToolIDsWithTheirModelResponses(t *testing.T) {
	t.Parallel()

	// Arrange two complete model/tool turns that reuse one response-local call ID.
	entries := []session.Entry{
		compactionUserEntry("u1", mo.None[string](), "first"),
		compactionModelEntry("m1", "u1", "same"),
		compactionToolEntry("r1", "m1", "same"),
		compactionUserEntry("u2", mo.Some("r1"), "second"),
		compactionModelEntry("m2", "u2", "same"),
		compactionToolEntry("r2", "m2", "same"),
	}
	service, identity := newCompactionBoundaryTestService(t, entries, "r2")

	// Act by retaining the complete second model/tool turn.
	_, err := service.CommitCompaction(
		t.Context(), identity, mo.Some("r2"), boundaryTestCompaction("m2"),
	)

	// Assert commit succeeds and model context contains only the summary and exact retained second turn.
	require.NoError(t, err)
	require.Len(t, service.ActiveEntries(), 7)
	projected := service.Snapshot()
	require.Len(t, projected, 3)
	require.Equal(t, agent.HistoryEntryUser, projected[0].Kind)
	require.Equal(t, agent.HistoryEntryModel, projected[1].Kind)
	require.Equal(t, "same", projected[1].Model.MustGet().Content[0].ToolCall.MustGet().ID)
	require.Equal(t, agent.HistoryEntryToolResult, projected[2].Kind)
	require.Equal(t, "same", projected[2].ToolResult.MustGet().CallID)
}

// TestCommitCompactionAllowsLaterBoundaryAfterFailedOrAbortedCalls verifies model-hidden calls do not block commit.
func TestCommitCompactionAllowsLaterBoundaryAfterFailedOrAbortedCalls(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		outcome model.Outcome
	}{
		{name: "failed", outcome: model.OutcomeFailed},
		{name: "aborted", outcome: model.OutcomeAborted},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a failed or aborted response with calls but no persisted results, followed by a user message.
			entries := []session.Entry{
				compactionUserEntry("u1", mo.None[string](), "old"),
				compactionModelEntry("m1", "u1", "call"),
			}
			response := entries[1].Model.MustGet()
			response.Outcome = mo.Some(test.outcome)
			entries[1].Model = mo.Some(response)
			entries = append(entries, compactionUserEntry("u2", mo.Some("m1"), "later"))
			service, identity := newCompactionBoundaryTestService(t, entries, "u2")

			// Act by committing after the model-hidden failed response.
			_, err := service.CommitCompaction(
				t.Context(), identity, mo.Some("u2"), boundaryTestCompaction("u2"),
			)

			// Assert absent results do not infer an active tool batch.
			require.NoError(t, err)
			require.Len(t, service.ActiveEntries(), 4)
		})
	}
}

// TestCommitCompactionAllowsLaterBoundaryAfterInterruptedBatch verifies skipped results use model-visible projection.
func TestCommitCompactionAllowsLaterBoundaryAfterInterruptedBatch(t *testing.T) {
	t.Parallel()

	// Arrange a stopped two-call batch with only the interrupted active result persisted.
	entries := []session.Entry{
		compactionUserEntry("u1", mo.None[string](), "old"),
		compactionModelEntry("m1", "u1", "active"),
	}
	response := entries[1].Model.MustGet()
	response.Content = append(response.Content, model.Content{
		Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall: mo.Some(model.ToolCall{
			ID: "skipped", Name: "tool", Arguments: testToolCallArguments(`{}`),
		}),
	})
	entries[1].Model = mo.Some(response)
	entries = append(
		entries,
		compactionToolEntry("r1", "m1", "active"),
		compactionUserEntry("u2", mo.Some("r1"), "later"),
	)
	service, identity := newCompactionBoundaryTestService(t, entries, "u2")

	// Act by committing after the interrupted batch and its single persisted result.
	_, err := service.CommitCompaction(
		t.Context(), identity, mo.Some("u2"), boundaryTestCompaction("u2"),
	)

	// Assert the omitted skipped result does not infer an active tool batch.
	require.NoError(t, err)
	require.Len(t, service.ActiveEntries(), 5)
}

// TestCommitCompactionRejectsBoundaryBeforeLatestCompaction verifies commit enforces monotonic retained boundaries.
func TestCommitCompactionRejectsBoundaryBeforeLatestCompaction(t *testing.T) {
	t.Parallel()

	// Arrange an active branch with one compaction followed by a new retained message.
	entries := []session.Entry{
		compactionUserEntry("u1", mo.None[string](), "summarized"),
		compactionUserEntry("u2", mo.Some("u1"), "first retained"),
		compactionMarkerEntry("c1", "u2", "first summary", "u2"),
		compactionUserEntry("u3", mo.Some("c1"), "later retained"),
	}
	service, identity := newCompactionBoundaryTestService(t, entries, "u3")

	// Act by moving the next retained boundary behind the preceding compaction boundary.
	_, err := service.CommitCompaction(
		t.Context(), identity, mo.Some("u3"),
		session.CompactionEntry{
			Summary: "second summary", FirstKeptEntryID: "u1",
			Source: session.CompactionSource{
				ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
			},
			EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
		},
	)

	// Assert validation fails before persistence can reintroduce the summarized prefix.
	require.Error(t, err)
	require.Contains(t, err.Error(), "preceding compaction")
	require.Len(t, service.ActiveEntries(), 4)
}

// boundaryTestCompaction creates one extension-sourced marker for boundary behavior tests.
func boundaryTestCompaction(firstKeptEntryID string) session.CompactionEntry {
	return session.CompactionEntry{
		Summary: "summary", FirstKeptEntryID: firstKeptEntryID,
		Source: session.CompactionSource{
			ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
	}
}

// newCompactionBoundaryTestService supplies harmless commit dependencies for boundary tests.
func newCompactionBoundaryTestService(
	t *testing.T,
	entries []session.Entry,
	leafID string,
) (*Service, contextcompaction.SessionIdentity) {
	t.Helper()
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	publisher := NewMockEntryPublisher(controller)
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(
		ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil,
	).AnyTimes()
	ids.EXPECT().NewID().Return("compaction", nil).AnyTimes()
	clock.EXPECT().Now().Return(time.Unix(10, 0).UTC()).AnyTimes()
	publisher.EXPECT().PublishSessionEntry(gomock.Any()).Return(
		func(context.Context) error { return nil }, nil,
	).AnyTimes()
	tree, err := session.NewTree(entries, mo.Some(leafID), nil)
	require.NoError(t, err)
	service := New(repository, ids, clock, nil, "/project")
	service.active = LoadedSession{
		Header:      session.Header{ID: "session", CreatedAt: time.Unix(1, 0).UTC(), WorkingDirectory: "/project"},
		StoragePath: "/sessions/session.jsonl", Tree: tree, Information: mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}
	identity := contextcompaction.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	service.contextIdentity.Store(&identity)
	service.BindEntryPublisher(publisher)
	return service, identity
}
