//go:build integration

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
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// TestSnapshotFailurePreservesActiveState verifies reconstruction stops mutations and resume before publication.
func TestSnapshotFailurePreservesActiveState(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"navigation", "label", "append", "resume"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			// Arrange invalid state using only public Tree value aliasing and strict repository expectations.
			controller := gomock.NewController(t)
			repository := NewMockRepository(controller)
			ids := NewMockIDGenerator(controller)
			clock := NewMockClock(controller)
			invalid, err := session.NewTree(nil, mo.None[string](), nil)
			require.NoError(t, err)
			alias := invalid
			require.NoError(t, alias.Add(snapshotFailureEntry()))
			require.NoError(t, invalid.SetActiveLeaf(mo.Some("x")))
			service := New(repository, ids, clock, nil, "/project")
			service.active = snapshotFailureLoaded(invalid)
			before := service.active
			if operation == "append" {
				ids.EXPECT().NewID().Return("next", nil)
				clock.EXPECT().Now().Return(time.Unix(2, 0).UTC())
			}
			if operation == "resume" {
				service.active = snapshotFailureLoaded(alias)
				before = service.active
				repository.EXPECT().Load(gomock.Any(), session.ID("loaded")).Return(snapshotFailureLoaded(invalid), nil)
			}

			// Act through the operation whose candidate depends on reconstruction.
			err = runSnapshotFailureOperation(t.Context(), service, operation)

			// Assert the cause reaches the caller, with no repository write or publisher call.
			require.ErrorContains(t, err, "active leaf does not exist")
			require.Equal(t, before, service.active)
			require.False(t, service.writeUnavailable)
			valid, err := alias.Clone()
			require.NoError(t, err)
			require.Len(t, valid.Entries(), 1)
		})
	}
}

// TestSessionSnapshotsReportInvalidLabels verifies aggregate and service snapshots retain reconstruction diagnostics.
func TestSessionSnapshotsReportInvalidLabels(t *testing.T) {
	t.Parallel()
	// Arrange a shared lookup and label whose entry is absent from the original value's entry slice.
	controller := gomock.NewController(t)
	invalid, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	alias := invalid
	require.NoError(t, alias.Add(snapshotFailureEntry()))
	require.NoError(t, alias.SetLabel("x", "label"))
	loaded := snapshotFailureLoaded(invalid)
	service := New(
		NewMockRepository(controller),
		NewMockIDGenerator(controller),
		NewMockClock(controller),
		nil,
		"/project",
	)
	service.active = loaded

	// Act through both snapshot contracts.
	_, aggregateErr := loaded.Clone()
	_, serviceErr := service.Tree()

	// Assert full reconstruction diagnostics survive, and later valid snapshots succeed in this process.
	require.ErrorContains(t, aggregateErr, `label target "x" does not exist`)
	require.ErrorContains(t, serviceErr, `label target "x" does not exist`)
	service.active = snapshotFailureLoaded(alias)
	cloned, err := service.active.Clone()
	require.NoError(t, err)
	snapshot, err := service.Tree()
	require.NoError(t, err)
	require.Equal(t, cloned.Tree.Entries(), snapshot.Entries())
	require.Equal(t, alias.Labels(), snapshot.Labels())
}

// TestNavigationRetainsCommitAfterSnapshotPublicationFailure verifies reconstruction errors preserve durable facts.
func TestNavigationRetainsCommitAfterSnapshotPublicationFailure(t *testing.T) {
	t.Parallel()
	// Arrange valid active state and a publisher whose independent aliased tree cannot be reconstructed.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	valid, err := session.NewTree([]session.Entry{snapshotFailureEntry()}, mo.Some("x"), nil)
	require.NoError(t, err)
	invalid, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	alias := invalid
	require.NoError(t, alias.Add(snapshotFailureEntry()))
	require.NoError(t, invalid.SetActiveLeaf(mo.Some("x")))
	service := New(repository, NewMockIDGenerator(controller), NewMockClock(controller), nil, "/project")
	service.active = snapshotFailureLoaded(valid)
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(ApplyResult{StoragePath: "/committed.jsonl"}, nil)
	var publicationCause error
	publisher := func(session.Tree) error {
		_, publicationCause = invalid.Clone()
		return publicationCause
	}

	// Act by committing navigation before the dependent publisher attempts its snapshot.
	commit, err := service.CommitNavigation(t.Context(), sessiontree.CommitCommand{
		ExpectedActiveLeafID: mo.Some("x"), DestinationID: mo.None[string](),
		BranchSummary: mo.None[sessiontree.BranchSummaryDraft](),
	}, publisher)

	// Assert the complete cause and committed facts survive together, with subsequent snapshots still usable.
	require.ErrorIs(t, err, publicationCause)
	require.ErrorContains(t, err, "active leaf does not exist")
	require.True(t, commit.Committed)
	require.True(t, commit.Tree.ActiveLeafID().IsNone())
	require.Len(t, commit.Tree.Entries(), 1)
	require.Equal(t, "/committed.jsonl", service.active.StoragePath)
	snapshot, snapshotErr := service.Tree()
	require.NoError(t, snapshotErr)
	require.Equal(t, commit.Tree.Entries(), snapshot.Entries())
	require.True(t, snapshot.ActiveLeafID().IsNone())
}

// runSnapshotFailureOperation invokes mutation and replacement boundaries without reconstructing fixtures.
func runSnapshotFailureOperation(ctx context.Context, service *Service, operation string) error {
	switch operation {
	case "navigation":
		_, err := service.CommitNavigation(ctx, sessiontree.CommitCommand{
			ExpectedActiveLeafID: mo.Some("x"), DestinationID: mo.None[string](),
			BranchSummary: mo.None[sessiontree.BranchSummaryDraft](),
		}, nil)
		return err
	case "label":
		_, err := service.SetLabel(ctx, "x", "label")
		return err
	case "append":
		return service.Append(ctx, agent.HistoryEntry{
			Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("next")),
			Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
		})
	default:
		_, _, err := service.ResumeActive(ctx, "loaded")
		return err
	}
}

// snapshotFailureLoaded supplies complete aggregate metadata around a public Tree fixture.
func snapshotFailureLoaded(tree session.Tree) LoadedSession {
	return LoadedSession{
		Header:      session.Header{ID: "active", CreatedAt: time.Unix(1, 0).UTC(), WorkingDirectory: "/project"},
		StoragePath: "/sessions/active.jsonl", Tree: tree,
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}
}

// snapshotFailureEntry supplies a valid root user entry for public alias sequences.
func snapshotFailureEntry() session.Entry {
	return session.Entry{
		ID:               "x",
		ParentID:         mo.None[string](),
		CreatedAt:        time.Unix(1, 0).UTC(),
		Information:      mo.None[session.Information](),
		User:             mo.Some(model.TextMessage("input")),
		Model:            mo.None[session.ModelResponse](),
		EstimatedCost:    mo.None[session.EstimatedCost](),
		ToolResult:       mo.None[session.ToolResult](),
		Extension:        mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
		BranchSummary:    mo.None[session.BranchSummaryEntry](),
		Compaction:       mo.None[session.CompactionEntry](),
	}
}
