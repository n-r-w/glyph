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
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
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
	service.contextIdentity.Store(&extensioncontext.SessionIdentity{
		ID: "session", WorkingDirectory: "/project", Incarnation: 1,
	})
	_, err = service.AppendExtension(
		t.Context(),
		extensioncontext.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
		session.ExtensionEnvelope{ExtensionID: "extension", EntryType: "state", Data: []byte(`{"value":true}`)},
		treeBehaviorCommitGuard,
	)
	require.NoError(t, err)

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

// TestExtensionMessageAppendCommitsBeforePublication verifies exact message persistence and post-commit delivery failure.
func TestExtensionMessageAppendCommitsBeforePublication(t *testing.T) {
	t.Parallel()

	// Arrange one active root and a publisher that observes committed state before returning a delivery failure.
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
	expected := extensioncontext.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	service.contextIdentity.Store(&expected)
	service.history = append(storedHistoryFromEntries(tree.ActiveBranch()), storedHistoryEntry{
		value: treeBehaviorPartialModelHistory(), clientVisible: true,
	})
	ids.EXPECT().NewID().Return("message", nil)
	clock.EXPECT().Now().Return(createdAt.Add(time.Second))
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			assert.Equal(t, "exact\ntext", command.Mutation.Entry.MustGet().ExtensionMessage.MustGet().Text)
			return ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil
		},
	)
	deliveryErr := errors.New("client writer failed")

	// Act by appending a hidden-client message through the state-owner publication boundary.
	committed, err := service.AppendExtensionMessage(t.Context(), expected, session.ExtensionMessage{
		ExtensionID: "extension", EntryType: "note", Text: "exact\ntext", Visibility: session.ClientVisibilityHidden,
	}, treeBehaviorCommitGuard, func(entry session.Entry) (func(context.Context) error, error) {
		assert.Equal(t, mo.Some("message"), service.active.Tree.ActiveLeafID())
		assert.Equal(t, "exact\ntext", entry.ExtensionMessage.MustGet().Text)
		return func(context.Context) error {
			if !service.mutex.TryRLock() {
				return errors.New("delivery wait retained the session lock")
			}
			service.mutex.RUnlock()
			return deliveryErr
		}, nil
	})

	// Assert the delivery cause is returned with the committed entry and no rollback.
	require.ErrorIs(t, err, deliveryErr)
	assert.Equal(t, "message", committed.ID)
	assert.Equal(t, mo.Some("root"), committed.ParentID)
	assert.Equal(t, createdAt.Add(time.Second), committed.CreatedAt)
	assert.Equal(t, mo.Some("message"), service.Tree().ActiveLeafID())
	require.Len(t, service.Snapshot(), 3)
	assert.Equal(t, "partial", service.Snapshot()[1].Model.MustGet().Content[0].Text.OrEmpty())
	require.Len(t, service.ClientSnapshot(), 2)
	assert.Equal(t, "partial", service.ClientSnapshot()[1].Model.MustGet().Content[0].Text.OrEmpty())
}

// TestExtensionMessageWithoutPublisherCommitsBeforeDeliveryFailure verifies missing binding is post-commit.
func TestExtensionMessageWithoutPublisherCommitsBeforeDeliveryFailure(t *testing.T) {
	t.Parallel()

	// Arrange one active root and no Glyph client publisher.
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
			Version: formatVersion, ID: "session", CreatedAt: createdAt, WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/session.jsonl", Tree: tree,
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}
	expected := extensioncontext.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	service.contextIdentity.Store(&expected)
	ids.EXPECT().NewID().Return("message", nil)
	clock.EXPECT().Now().Return(createdAt.Add(time.Second))
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(
		ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil,
	)

	// Act without supplying the required client publisher.
	committed, err := service.AppendExtensionMessage(t.Context(), expected, session.ExtensionMessage{
		ExtensionID: "extension", EntryType: "note", Text: "exact text", Visibility: session.ClientVisibilityVisible,
	}, treeBehaviorCommitGuard, nil)

	// Assert persistence and state commit precede the explicit delivery failure.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publisher is not bound")
	assert.Equal(t, "message", committed.ID)
	assert.Equal(t, mo.Some("message"), service.Tree().ActiveLeafID())
	assert.Len(t, service.Tree().Entries(), 2)
}

// TestExtensionAppendFailurePreservesPublishedState verifies hidden append is atomic with persistence.
func TestExtensionAppendFailurePreservesPublishedState(t *testing.T) {
	t.Parallel()

	// Arrange one active root and a durable write failure for a valid opaque payload.
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
	expected := extensioncontext.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	service.contextIdentity.Store(&expected)
	ids.EXPECT().NewID().Return("candidate", nil)
	clock.EXPECT().Now().Return(createdAt.Add(time.Second))
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(ApplyResult{}, errors.New("sync failed"))

	// Act by appending exact extension data whose persistence fails.
	_, err = service.AppendExtension(t.Context(), expected, session.ExtensionEnvelope{
		ExtensionID: "extension", EntryType: "checkpoint", Data: []byte(`{ "step": 2 }`),
	}, treeBehaviorCommitGuard)

	// Assert failure classification retains the durable cause and publishes no candidate.
	require.ErrorIs(t, err, session.ErrPersistenceUnavailable)
	assert.Contains(t, err.Error(), "sync failed")
	assert.Equal(t, mo.Some("root"), service.Tree().ActiveLeafID())
	assert.Len(t, service.Tree().Entries(), 1)
}

// TestExtensionAppendRejectsStaleOrCanceledWork verifies pre-commit validation performs no mutation.
func TestExtensionAppendRejectsStaleOrCanceledWork(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		// name identifies the rejected append condition.
		name string
		// expected supplies the binding identity presented at commit.
		expected extensioncontext.SessionIdentity
		// canceled selects a canceled operation context.
		canceled bool
	}{
		{name: "stale incarnation", expected: extensioncontext.SessionIdentity{ID: "session", Incarnation: 1}, canceled: false},
		{name: "canceled", expected: extensioncontext.SessionIdentity{ID: "session", Incarnation: 2}, canceled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange an empty active session whose current incarnation is newer than one test input.
			tree, err := session.NewTree(nil, mo.None[string](), nil)
			require.NoError(t, err)
			service := New(nil, nil, nil, nil, "/project")
			service.active = LoadedSession{
				Header:      session.Header{Version: formatVersion, ID: "session", WorkingDirectory: "/project"},
				StoragePath: "", Tree: tree, Information: mo.None[session.Information](),
				InformationUpdatedAt: mo.None[time.Time](),
			}
			current := extensioncontext.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 2}
			service.contextIdentity.Store(&current)
			ctx := t.Context()
			if test.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			// Act before identifier allocation or persistence.
			_, err = service.AppendExtension(ctx, test.expected, session.ExtensionEnvelope{
				ExtensionID: "extension", EntryType: "state", Data: []byte(`{ "step": 2 }`),
			}, treeBehaviorCommitGuard)

			// Assert no session entry is published for stale or canceled work.
			require.Error(t, err)
			assert.Empty(t, service.Tree().Entries())
			assert.Equal(t, mo.None[string](), service.Tree().ActiveLeafID())
		})
	}
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
		BranchSummary: mo.None[session.BranchSummaryEntry](), ExtensionMessage: mo.None[session.ExtensionMessage](),
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

// TestExtensionStateFiltersOneActiveBranch verifies coherent recovery without parent rewriting.
func TestExtensionStateFiltersOneActiveBranch(t *testing.T) {
	t.Parallel()

	// Arrange entries from two extensions and one abandoned checkpoint.
	createdAt := time.Unix(1, 0).UTC()
	root := treeBehaviorUserEntry("root", mo.None[string](), createdAt)
	other := treeBehaviorExtensionEntry("other", mo.Some("root"), createdAt.Add(time.Second), "other")
	active := treeBehaviorExtensionEntry("active", mo.Some("other"), createdAt.Add(2*time.Second), "caller")
	message := treeBehaviorExtensionMessage("message", mo.Some("active"), createdAt.Add(3*time.Second), "caller")
	abandoned := treeBehaviorExtensionEntry("abandoned", mo.Some("root"), createdAt.Add(4*time.Second), "caller")
	tree, err := session.NewTree([]session.Entry{root, other, active, message, abandoned}, mo.Some("message"), nil)
	require.NoError(t, err)
	service := New(nil, nil, nil, nil, "/project")
	service.active = LoadedSession{
		Header: session.Header{
			Version:          formatVersion,
			ID:               "session",
			CreatedAt:        createdAt,
			WorkingDirectory: "/project",
		},
		StoragePath:          "",
		Tree:                 tree,
		Information:          mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}
	expected := extensioncontext.SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 7}
	service.contextIdentity.Store(&expected)

	// Act through the state-owner snapshot operation.
	snapshot, err := service.ExtensionState(t.Context(), expected, "caller")

	// Assert only the caller's active checkpoint remains and its omitted parent is unchanged.
	require.NoError(t, err)
	assert.Equal(t, session.ID("session"), snapshot.SessionID)
	assert.Equal(t, mo.Some("message"), snapshot.ActiveLeafID)
	require.Len(t, snapshot.Entries, 2)
	assert.Equal(t, "active", snapshot.Entries[0].ID)
	assert.Equal(t, mo.Some("other"), snapshot.Entries[0].ParentID)
	assert.Equal(t, "message", snapshot.Entries[1].ID)
	assert.Equal(t, mo.Some("active"), snapshot.Entries[1].ParentID)
	assert.Equal(t, "exact message", snapshot.Entries[1].ExtensionMessage.MustGet().Text)
}

// TestClientSnapshotRetainsProcessLocalHistory verifies ordinary reads keep in-process Agent Core entries.
func TestClientSnapshotRetainsProcessLocalHistory(t *testing.T) {
	t.Parallel()

	// Arrange one empty session and a valid nonterminal model update that is not persisted.
	service := New(nil, nil, nil, nil, "/project")
	require.NoError(t, service.Append(t.Context(), treeBehaviorPartialModelHistory()))

	// Act by reading ordinary client history.
	history := service.ClientSnapshot()

	// Assert the process-local item remains available exactly as before durable message filtering.
	require.Len(t, history, 1)
	assert.Equal(t, "partial", history[0].Model.MustGet().Content[0].Text.OrEmpty())
}

// TestClientSnapshotExcludesOnlyHiddenExtensionMessages verifies ordinary transcript presentation filtering.
func TestClientSnapshotExcludesOnlyHiddenExtensionMessages(t *testing.T) {
	t.Parallel()

	// Arrange one active branch with visible and hidden-client model-visible messages.
	createdAt := time.Unix(1, 0).UTC()
	visible := treeBehaviorExtensionMessage("visible", mo.None[string](), createdAt, "caller")
	hidden := treeBehaviorExtensionMessage("hidden", mo.Some("visible"), createdAt.Add(time.Second), "caller")
	hiddenMessage := hidden.ExtensionMessage.MustGet()
	hiddenMessage.Visibility = session.ClientVisibilityHidden
	hidden.ExtensionMessage = mo.Some(hiddenMessage)
	tree, err := session.NewTree([]session.Entry{visible, hidden}, mo.Some("hidden"), nil)
	require.NoError(t, err)
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	repository.EXPECT().Load(gomock.Any(), session.ID("session")).Return(LoadedSession{
		Header: session.Header{
			Version: formatVersion, ID: "session", CreatedAt: createdAt, WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/session.jsonl", Tree: tree,
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}, nil)
	service := New(repository, nil, nil, nil, "/project")

	// Act by replacing the active session and reading complete and ordinary history.
	_, err = service.ResumeActive(t.Context(), "session")
	require.NoError(t, err)
	complete := service.Snapshot()
	history := service.ClientSnapshot()

	// Assert replacement retains both model inputs while ordinary history omits only the hidden message.
	require.Len(t, complete, 2)
	assert.Equal(t, "exact message", complete[0].User.MustGet().Text("\n"))
	assert.Equal(t, "exact message", complete[1].User.MustGet().Text("\n"))
	require.Len(t, history, 1)
	assert.Equal(t, "exact message", history[0].User.MustGet().Text("\n"))
}

// treeBehaviorCommitGuard supplies a valid runtime guard for isolated session tests.
func treeBehaviorCommitGuard() (func(), error) { return func() {}, nil }

// treeBehaviorPartialModelHistory supplies one process-local model update.
func treeBehaviorPartialModelHistory() agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
		Model: mo.Some(model.Response{
			Content: []model.Content{{
				Kind: model.ContentText, Text: mo.Some("partial"), Final: false,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			}},
			Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
			ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
			Usage: mo.None[model.Usage](), Diagnostics: nil,
		}), ToolResult: mo.None[agent.ToolResult](),
	}
}

// treeBehaviorExtensionEntry creates one model-hidden extension entry.
func treeBehaviorExtensionEntry(
	id string,
	parentID mo.Option[string],
	createdAt time.Time,
	extensionID string,
) session.Entry {
	return session.Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](),
		Extension: mo.Some(session.ExtensionEnvelope{
			ExtensionID: extensionID, EntryType: "state", Data: []byte(`{ "step": 2 }`),
		}),
		BranchSummary: mo.None[session.BranchSummaryEntry](), ExtensionMessage: mo.None[session.ExtensionMessage](),
	}
}

// treeBehaviorExtensionMessage creates one model-visible extension message.
func treeBehaviorExtensionMessage(
	id string,
	parentID mo.Option[string],
	createdAt time.Time,
	extensionID string,
) session.Entry {
	return session.Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.Some(session.ExtensionMessage{
			ExtensionID: extensionID,
			EntryType:   "note",
			Text:        "exact message",
			Visibility:  session.ClientVisibilityVisible,
		}), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
}

// treeBehaviorUserEntry creates one valid text user entry for tree behavior tests.
func treeBehaviorUserEntry(id string, parentID mo.Option[string], createdAt time.Time) session.Entry {
	return session.Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[session.Information](), User: mo.Some(model.TextMessage(id)),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](), ExtensionMessage: mo.None[session.ExtensionMessage](),
	}
}

// treeBehaviorEntryIDs projects entry identifiers for concise cross-file assertions.
func treeBehaviorEntryIDs(entries []session.Entry) []string {
	return lo.Map(entries, func(entry session.Entry, _ int) string {
		return entry.ID
	})
}
