//go:build !integration

package sessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestAppendUsesCurrentActiveLeafForEverySupportedEntry verifies continuation entries follow the selected branch.
func TestAppendUsesCurrentActiveLeafForEverySupportedEntry(t *testing.T) {
	t.Parallel()

	// Arrange a branched tree whose active leaf is not the last persisted entry.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	createdAt := time.Unix(1, 0).UTC()
	root := treeBehaviorUserEntry("root", mo.None[string](), createdAt)
	active := treeBehaviorUserEntry("active", mo.Some("root"), createdAt.Add(time.Second))
	abandoned := treeBehaviorUserEntry("abandoned", mo.Some("root"), createdAt.Add(2*time.Second))
	tree, err := session.NewTree([]session.Entry{root, active, abandoned}, mo.Some("active"), nil)
	require.NoError(t, err)
	service := New(repository, ids, clock, nil, "/project")
	service.active = LoadedSession{
		Header: session.Header{
			Version:          formatVersion,
			ID:               "session",
			CreatedAt:        createdAt,
			WorkingDirectory: "/project",
		},
		StoragePath:          "/sessions/session.jsonl",
		Tree:                 tree,
		Information:          mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}
	entryIDs := []string{"user", "model", "tool", "extension"}
	entryTimes := []time.Time{
		createdAt.Add(3 * time.Second), createdAt.Add(4 * time.Second),
		createdAt.Add(5 * time.Second), createdAt.Add(6 * time.Second),
	}
	for index := range entryIDs {
		ids.EXPECT().NewID().Return(entryIDs[index], nil)
		clock.EXPECT().Now().Return(entryTimes[index])
	}
	parents := make([]mo.Option[string], 0, len(entryIDs))
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			parents = append(parents, command.Mutation.Entry.MustGet().ParentID)
			return ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil
		},
	).Times(len(entryIDs))

	// Act by appending one entry of each service-owned kind.
	require.NoError(t, service.Append(t.Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("new branch")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}))
	require.NoError(t, service.Append(t.Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
		Model: mo.Some(model.Response{
			Content: nil, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
			ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
		}),
		ToolResult: mo.None[agent.ToolResult](),
	}))
	require.NoError(t, service.Append(t.Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(agent.ToolResult{
			CallID: "call", ToolName: "tool", Contents: tool.TextContents("result"), IsError: false,
		}),
	}))
	require.NoError(t, service.AppendExtension(t.Context(), session.ExtensionEnvelope{
		ExtensionID: "extension", EntryType: "state", Data: []byte(`{"value":true}`),
	}))

	// Assert every persisted parent is the preceding committed active leaf and all branches remain stored.
	assert.Equal(t, []mo.Option[string]{
		mo.Some("active"), mo.Some("user"), mo.Some("model"), mo.Some("tool"),
	}, parents)
	assert.Equal(t, mo.Some("extension"), service.Tree().ActiveLeafID())
	assert.Len(t, service.Tree().Entries(), 7)
}

// TestAppendFailureKeepsCurrentActiveLeaf verifies failed persistence does not publish candidate tree state.
func TestAppendFailureKeepsCurrentActiveLeaf(t *testing.T) {
	t.Parallel()

	// Arrange one active root and a repository failure for its candidate child.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	createdAt := time.Unix(1, 0).UTC()
	tree, err := session.NewTree(
		[]session.Entry{treeBehaviorUserEntry("root", mo.None[string](), createdAt)}, mo.Some("root"), nil,
	)
	require.NoError(t, err)
	service := New(repository, ids, clock, nil, "/project")
	service.active = LoadedSession{
		Header: session.Header{
			Version:          formatVersion,
			ID:               "session",
			CreatedAt:        createdAt,
			WorkingDirectory: "/project",
		},
		StoragePath:          "/sessions/session.jsonl",
		Tree:                 tree,
		Information:          mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}
	ids.EXPECT().NewID().Return("candidate", nil)
	clock.EXPECT().Now().Return(createdAt.Add(time.Second))
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(ApplyResult{}, errors.New("sync failed"))

	// Act by appending a user entry whose durable write fails.
	err = service.Append(t.Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("candidate")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	})

	// Assert the published tree retains only the previously committed active leaf.
	require.Error(t, err)
	assert.Equal(t, mo.Some("root"), service.Tree().ActiveLeafID())
	assert.Equal(t, []string{"root"}, treeBehaviorEntryIDs(service.Tree().Entries()))
}

// TestTreeReturnsDefensiveSnapshot verifies callers cannot mutate active entries, labels, or extension bytes.
func TestTreeReturnsDefensiveSnapshot(t *testing.T) {
	t.Parallel()

	// Arrange an active tree with mutable message bytes, extension bytes, and a label.
	createdAt := time.Unix(1, 0).UTC()
	user := treeBehaviorUserEntry("user", mo.None[string](), createdAt)
	user.User = mo.Some(model.Message{Content: []model.InputContent{
		{
			Kind:      model.InputContentImage,
			Text:      mo.None[string](),
			MediaType: mo.Some("image/png"),
			Data:      mo.Some([]byte{1, 2, 3}),
		},
	}})
	extension := session.Entry{
		ID:            "extension",
		ParentID:      mo.Some("user"),
		CreatedAt:     createdAt.Add(time.Second),
		Information:   mo.None[session.Information](),
		User:          mo.None[session.UserMessage](),
		Model:         mo.None[session.ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult:    mo.None[session.ToolResult](),
		Extension: mo.Some(
			session.ExtensionEnvelope{ExtensionID: "extension", EntryType: "state", Data: []byte{4, 5, 6}},
		),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	tree, err := session.NewTree(
		[]session.Entry{user, extension}, mo.Some("extension"), map[string]string{"user": "checkpoint"},
	)
	require.NoError(t, err)
	service := New(nil, nil, nil, nil, "/project")
	service.active = LoadedSession{
		Header: session.Header{}, StoragePath: "", Tree: tree,
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}

	// Act by changing every mutable value reachable from one returned snapshot.
	snapshot := service.Tree()
	entries := snapshot.Entries()
	entries[0].User.MustGet().Content[0].Data.MustGet()[0] = 9
	entries[1].Extension.MustGet().Data[0] = 9
	require.NoError(t, snapshot.SetLabel("user", "mutated"))
	require.NoError(t, snapshot.Add(treeBehaviorUserEntry("child", mo.Some("extension"), createdAt.Add(2*time.Second))))

	// Assert a later snapshot retains the committed values and structure.
	later := service.Tree()
	assert.Equal(t, []byte{1, 2, 3}, later.Entries()[0].User.MustGet().Content[0].Data.MustGet())
	assert.Equal(t, []byte{4, 5, 6}, later.Entries()[1].Extension.MustGet().Data)
	assert.Equal(t, map[string]string{"user": "checkpoint"}, later.Labels())
	assert.Equal(t, []string{"user", "extension"}, treeBehaviorEntryIDs(later.Entries()))
}

// treeBehaviorUserEntry creates one valid text user entry for tree behavior tests.
func treeBehaviorUserEntry(id string, parentID mo.Option[string], createdAt time.Time) session.Entry {
	return session.Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[session.Information](), User: mo.Some(model.TextMessage(id)),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
}

// treeBehaviorEntryIDs projects entry identifiers for concise cross-file assertions.
func treeBehaviorEntryIDs(entries []session.Entry) []string {
	return lo.Map(entries, func(entry session.Entry, _ int) string {
		return entry.ID
	})
}
