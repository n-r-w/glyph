//go:build integration

package sessions_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/infra/persistence/sessionfilesystem"
	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// TestImplicitRootNavigationSurvivesRestartAndStartsANewRoot verifies the complete production repository lifecycle.
func TestImplicitRootNavigationSurvivesRestartAndStartsANewRoot(t *testing.T) {
	t.Parallel()

	// Arrange a durable labeled branch and resume it through the Host service.
	root := t.TempDir()
	project := t.TempDir()
	repository := sessionstore.New(root, project, sessionfilesystem.New())
	require.NoError(t, repository.Initialize(t.Context()))
	sourceTree := restartSourceTree(t)
	_, err := repository.CreateSnapshot(t.Context(), hostsessions.CreateSnapshotCommand{
		Header: session.Header{
			ID:               "source",
			CreatedAt:        time.Unix(1, 0).UTC(),
			WorkingDirectory: project,
		},
		Tree:                 sourceTree,
		Information:          mo.Some(session.Information{Name: "source"}),
		InformationUpdatedAt: mo.Some(time.Unix(2, 0).UTC()),
	})
	require.NoError(t, err)
	controller := gomock.NewController(t)
	service := hostsessions.New(
		repository,
		hostsessions.NewMockIDGenerator(controller),
		hostsessions.NewMockClock(controller),
		hostsessions.NewMockPricingCatalog(controller),
		project,
	)
	_, _, err = service.ResumeActive(t.Context(), "source")
	require.NoError(t, err)
	preparation, err := service.Tree().NavigationPreparation("root")
	require.NoError(t, err)
	require.True(t, preparation.DestinationID.IsNone())
	require.Equal(t, mo.Some("root"), preparation.NextInput)

	// Act by committing no-summary navigation to the implicit root.
	commit, err := service.CommitNavigation(t.Context(), sessiontree.CommitCommand{
		ExpectedActiveLeafID: mo.Some("target"),
		DestinationID:        preparation.DestinationID,
		BranchSummary:        mo.None[sessiontree.BranchSummaryDraft](),
	}, func(published session.Tree) error {
		require.True(t, published.ActiveLeafID().IsNone())
		return nil
	})

	// Assert the commit keeps stored entries and labels while clearing active provider state.
	require.NoError(t, err)
	require.True(t, commit.Tree.ActiveLeafID().IsNone())
	require.Empty(t, service.ActiveEntries())
	require.Empty(t, service.Snapshot())
	require.Equal(t, sourceTree.Entries(), service.Tree().Entries())
	require.Equal(t, sourceTree.Labels(), service.Tree().Labels())

	// Act by restarting the repository and resuming the persisted implicit-root session.
	restartedRepository := sessionstore.New(root, project, sessionfilesystem.New())
	require.NoError(t, restartedRepository.Initialize(t.Context()))
	restartedController := gomock.NewController(t)
	restartedIDs := hostsessions.NewMockIDGenerator(restartedController)
	restartedClock := hostsessions.NewMockClock(restartedController)
	restarted := hostsessions.New(
		restartedRepository,
		restartedIDs,
		restartedClock,
		hostsessions.NewMockPricingCatalog(restartedController),
		project,
	)
	_, activeEntries, err := restarted.ResumeActive(t.Context(), "source")
	require.NoError(t, err)

	// Assert restart preserves the implicit root, full stored tree, labels, and empty projections.
	require.Empty(t, activeEntries)
	require.Empty(t, restarted.ActiveEntries())
	require.Empty(t, restarted.Snapshot())
	require.True(t, restarted.Tree().ActiveLeafID().IsNone())
	require.Equal(t, sourceTree.Entries(), restarted.Tree().Entries())
	require.Equal(t, sourceTree.Labels(), restarted.Tree().Labels())

	// Act by appending the next user input from the implicit root.
	restartedIDs.EXPECT().NewID().Return("later-root", nil)
	restartedClock.EXPECT().Now().Return(time.Unix(100, 0).UTC())
	err = restarted.Append(t.Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("later root input")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	})

	// Assert the new root becomes active while every prior branch and label remains stored.
	require.NoError(t, err)
	persisted := restarted.Tree()
	require.Equal(t, mo.Some("later-root"), persisted.ActiveLeafID())
	require.Len(t, persisted.Entries(), len(sourceTree.Entries())+1)
	laterRoot := persisted.Entries()[len(persisted.Entries())-1]
	require.True(t, laterRoot.ParentID.IsNone())
	require.Equal(t, model.TextMessage("later root input"), laterRoot.User.MustGet())
	require.Equal(t, sourceTree.Entries(), persisted.Entries()[:len(sourceTree.Entries())])
	require.Equal(t, sourceTree.Labels(), persisted.Labels())
}

// TestReplacementAndLabelReplayRestoresExactCommittedState verifies fork, clone, and label durability across repository
// restart.
func TestReplacementAndLabelReplayRestoresExactCommittedState(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		mutate         func(*testing.T, *hostsessions.Service) session.ID
		expectedIDs    []string
		expectedLeaf   mo.Option[string]
		expectedLabels map[string]string
		sourceLabels   map[string]string
	}{
		{
			name: "fork",
			mutate: func(t *testing.T, service *hostsessions.Service) session.ID {
				replacementInfo, _, nextInput, err := service.ForkActive(t.Context(), "target")
				require.NoError(t, err)
				require.Equal(t, "exact target", nextInput)
				return replacementInfo.ID
			},
			expectedIDs:    []string{"root", "extension", "message", "summary"},
			expectedLeaf:   mo.Some("summary"),
			expectedLabels: map[string]string{"summary": "kept"},
			sourceLabels:   map[string]string{"summary": "kept", "target": "source"},
		},
		{
			name: "clone",
			mutate: func(t *testing.T, service *hostsessions.Service) session.ID {
				replacementInfo, _, err := service.CloneActive(t.Context())
				require.NoError(t, err)
				return replacementInfo.ID
			},
			expectedIDs:    []string{"root", "extension", "message", "summary", "target"},
			expectedLeaf:   mo.Some("target"),
			expectedLabels: map[string]string{"summary": "kept", "target": "source"},
			sourceLabels:   map[string]string{"summary": "kept", "target": "source"},
		},
		{
			name: "label",
			mutate: func(t *testing.T, service *hostsessions.Service) session.ID {
				_, err := service.SetLabel(t.Context(), "target", "updated")
				require.NoError(t, err)
				return "source"
			},
			expectedIDs:    []string{"root", "extension", "message", "summary", "target"},
			expectedLeaf:   mo.Some("target"),
			expectedLabels: map[string]string{"summary": "kept", "target": "updated"},
			sourceLabels:   map[string]string{"summary": "kept", "target": "updated"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a durable source and active service with strict generated dependencies.
			root := t.TempDir()
			project := t.TempDir()
			repository := sessionstore.New(root, project, sessionfilesystem.New())
			require.NoError(t, repository.Initialize(t.Context()))
			sourceTree := restartSourceTree(t)
			_, err := repository.CreateSnapshot(t.Context(), hostsessions.CreateSnapshotCommand{
				Header: session.Header{
					ID:               "source",
					CreatedAt:        time.Unix(1, 0).UTC(),
					WorkingDirectory: project,
				},
				Tree:                 sourceTree,
				Information:          mo.Some(session.Information{Name: "source"}),
				InformationUpdatedAt: mo.Some(time.Unix(2, 0).UTC()),
			})
			require.NoError(t, err)
			controller := gomock.NewController(t)
			ids := hostsessions.NewMockIDGenerator(controller)
			clock := hostsessions.NewMockClock(controller)
			pricing := hostsessions.NewMockPricingCatalog(controller)
			if test.name != "label" {
				ids.EXPECT().NewID().Return("replacement", nil)
				clock.EXPECT().Now().Return(time.Unix(100, 0).UTC())
			}
			service := hostsessions.New(repository, ids, clock, pricing, project)
			_, _, err = service.ResumeActive(t.Context(), "source")
			require.NoError(t, err)

			// Act through the Host service, then create a fresh repository instance to replay storage.
			resultID := test.mutate(t, service)
			restarted := sessionstore.New(root, project, sessionfilesystem.New())
			loaded, err := restarted.Load(t.Context(), resultID)

			// Assert exact retained identity, relations, labels, opaque data, summary provenance, and active leaf.
			require.NoError(t, err)
			require.Equal(t, test.expectedIDs, lo.Map(loaded.Tree.Entries(), func(entry session.Entry, _ int) string {
				return entry.ID
			}))
			require.Equal(t, test.expectedLeaf, loaded.Tree.ActiveLeafID())
			require.Equal(t, test.expectedLabels, loaded.Tree.Labels())
			entries := loaded.Tree.Entries()
			if len(entries) > 1 {
				require.True(t, bytes.Equal(
					[]byte(`{ "escaped": "\u0061", "opaque": true }`),
					entries[1].Extension.MustGet().Data,
				))
				require.Equal(t, "exact\ntext", entries[2].ExtensionMessage.MustGet().Text)
				require.Equal(t, session.ClientVisibilityHidden, entries[2].ExtensionMessage.MustGet().Visibility)
				require.Equal(t, "outside-first", entries[3].BranchSummary.MustGet().FirstEntryID)
				require.Equal(t, "outside-last", entries[3].BranchSummary.MustGet().LastEntryID)
			}
			for index := 1; index < len(entries); index++ {
				require.Equal(t, mo.Some(entries[index-1].ID), entries[index].ParentID)
			}
			source, err := restarted.Load(t.Context(), "source")
			require.NoError(t, err)
			require.Equal(
				t,
				[]string{"root", "extension", "message", "summary", "target"},
				lo.Map(source.Tree.Entries(), func(entry session.Entry, _ int) string {
					return entry.ID
				}),
			)
			require.Equal(t, test.sourceLabels, source.Tree.Labels())
		})
	}
}

// restartSourceTree creates one branch with opaque data and unresolved summary provenance.
func restartSourceTree(t *testing.T) session.Tree {
	t.Helper()
	createdAt := time.Unix(1, 0).UTC()
	entries := []session.Entry{
		restartUserEntry("root", mo.None[string](), "root", createdAt),
		{
			ID:            "extension",
			ParentID:      mo.Some("root"),
			CreatedAt:     createdAt.Add(time.Second),
			Information:   mo.None[session.Information](),
			User:          mo.None[session.UserMessage](),
			Model:         mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult:    mo.None[session.ToolResult](),
			Extension: mo.Some(
				session.ExtensionEnvelope{
					ExtensionID: "extension",
					EntryType:   "checkpoint",
					Data:        []byte(`{ "escaped": "\u0061", "opaque": true }`),
				},
			),
			BranchSummary: mo.None[session.BranchSummaryEntry](), Compaction: mo.None[session.CompactionEntry](),
			ExtensionMessage: mo.None[session.ExtensionMessage](),
		},
		{
			ID: "message", ParentID: mo.Some("extension"), CreatedAt: createdAt.Add(2 * time.Second),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
			Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			ExtensionMessage: mo.Some(session.ExtensionMessage{
				ExtensionID: "extension",
				EntryType:   "note",
				Text:        "exact\ntext",
				Visibility:  session.ClientVisibilityHidden,
			}), BranchSummary: mo.None[session.BranchSummaryEntry](),
			Compaction: mo.None[session.CompactionEntry](),
		},
		{
			ID:            "summary",
			ParentID:      mo.Some("message"),
			CreatedAt:     createdAt.Add(3 * time.Second),
			Information:   mo.None[session.Information](),
			User:          mo.None[session.UserMessage](),
			Model:         mo.None[session.ModelResponse](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult:    mo.None[session.ToolResult](),
			Extension:     mo.None[session.ExtensionEnvelope](),
			BranchSummary: mo.Some(session.BranchSummaryEntry{
				Summary: "summary", FirstEntryID: "outside-first", LastEntryID: "outside-last",
				Source: session.BranchSummarySource{
					ExtensionID: mo.None[string](), Model: mo.Some(session.BranchSummaryModelSource{
						Selection: model.Selection{
							Provider:        "provider",
							Model:           "model",
							ReasoningChoice: model.ReasoningChoiceLow,
						},
						Usage: mo.None[session.TokenUsage](),
					}),
				}, EstimatedCost: mo.None[session.EstimatedCost](),
			}), Compaction: mo.None[session.CompactionEntry](),
			ExtensionMessage: mo.None[session.ExtensionMessage](),
		},
		restartUserEntry("target", mo.Some("summary"), "exact target", createdAt.Add(4*time.Second)),
	}
	tree, err := session.NewTree(entries, mo.Some("target"), map[string]string{"summary": "kept", "target": "source"})
	require.NoError(t, err)
	return tree
}

// restartUserEntry creates one complete user entry.
func restartUserEntry(id string, parent mo.Option[string], text string, createdAt time.Time) session.Entry {
	return session.Entry{
		ID:          id,
		ParentID:    parent,
		CreatedAt:   createdAt,
		Information: mo.None[session.Information](),
		User: mo.Some(
			model.TextMessage(text),
		),
		Model:         mo.None[session.ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult:    mo.None[session.ToolResult](),
		Extension:     mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](), Compaction: mo.None[session.CompactionEntry](),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
	}
}
