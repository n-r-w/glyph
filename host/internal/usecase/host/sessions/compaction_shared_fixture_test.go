// Package sessions owns session history and compaction state.
package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// newCompactionBoundaryTestService supplies harmless commit dependencies for boundary tests.
func newCompactionBoundaryTestService(
	t *testing.T,
	entries []session.Entry,
	leafID string,
	publisher mo.Option[EntryPublisher],
) (*Service, session.Identity) {
	t.Helper()
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(
		ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil,
	).AnyTimes()
	ids.EXPECT().NewID().Return("compaction", nil).AnyTimes()
	clock.EXPECT().Now().Return(time.Unix(10, 0).UTC()).AnyTimes()
	entryPublisher, supplied := publisher.Get()
	if !supplied {
		defaultPublisher := NewMockEntryPublisher(controller)
		defaultPublisher.EXPECT().
			PublishSessionEntry(gomock.Any()).
			Return(func(context.Context) error { return nil }, nil).
			AnyTimes()
		entryPublisher = defaultPublisher
	}
	tree, err := session.NewTree(entries, mo.Some(leafID), nil)
	require.NoError(t, err)
	service := New(repository, ids, clock, nil, "/project")
	service.active = LoadedSession{
		Header:      session.Header{ID: "session", CreatedAt: time.Unix(1, 0).UTC(), WorkingDirectory: "/project"},
		StoragePath: "/sessions/session.jsonl", Tree: tree, Information: mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	service.contextIdentity.Store(&identity)
	service.BindEntryPublisher(entryPublisher)
	return service, identity
}

// newCompactionHistoryTestService arranges the active branch for projection and capture.
func newCompactionHistoryTestService(
	t *testing.T,
	entries []session.Entry,
	leafID string,
) (*Service, session.Identity) {
	t.Helper()
	tree, err := session.NewTree(entries, mo.Some(leafID), nil)
	require.NoError(t, err)
	service := New(nil, nil, nil, nil, "/project")
	service.active = LoadedSession{
		Header:      session.Header{ID: "session", CreatedAt: time.Unix(1, 0).UTC(), WorkingDirectory: "/project"},
		StoragePath: "/sessions/session.jsonl", Tree: tree, Information: mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	service.contextIdentity.Store(&identity)
	return service, identity
}

// newCompactionUserEntry returns one text user entry.
func newCompactionUserEntry(id string, parentID mo.Option[string], text string) session.Entry {
	entry := newCompactionBaseEntry(id, parentID)
	entry.User = mo.Some(model.TextMessage(text))
	return entry
}

// newCompactionMarkerEntry returns one persisted compaction marker.
func newCompactionMarkerEntry(id, parentID, summary, firstKeptID string) session.Entry {
	entry := newCompactionBaseEntry(id, mo.Some(parentID))
	entry.Compaction = mo.Some(session.CompactionEntry{
		Summary: summary, FirstKeptEntryID: firstKeptID,
		Source: session.CompactionSource{
			ExtensionID: mo.Some("extension"), Requests: nil,
		},
		Details: mo.None[[]byte](),
	})
	return entry
}

// newCompactionBaseEntry returns one empty test entry with all payload alternatives initialized.
func newCompactionBaseEntry(id string, parentID mo.Option[string]) session.Entry {
	return session.Entry{
		ID: id, ParentID: parentID, CreatedAt: time.Unix(1, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.None[session.ExtensionMessage](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.None[session.CompactionEntry](),
	}
}

// newCompactionBoundaryEntry creates one extension-sourced marker for boundary behavior tests.
func newCompactionBoundaryEntry(firstKeptEntryID string) session.CompactionEntry {
	return session.CompactionEntry{
		Summary: "summary", FirstKeptEntryID: firstKeptEntryID,
		Source: session.CompactionSource{
			ExtensionID: mo.Some("extension"), Requests: nil,
		},
		Details: mo.None[[]byte](),
	}
}
