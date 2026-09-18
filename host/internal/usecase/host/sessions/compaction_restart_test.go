//go:build !integration

package sessions

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestResumeActiveRebuildsCompactedContextAndOriginalClientHistory verifies restart projection separation.
func TestResumeActiveRebuildsCompactedContextAndOriginalClientHistory(t *testing.T) {
	t.Parallel()

	// Arrange one persisted branch with a compaction marker and a later original message.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	entries := []session.Entry{
		compactionUserEntry("u1", mo.None[string](), "old"),
		compactionUserEntry("u2", mo.Some("u1"), "kept"),
		compactionMarkerEntry("c1", "u2", "summary", "u2"),
		compactionUserEntry("u3", mo.Some("c1"), "new"),
	}
	tree, err := session.NewTree(entries, mo.Some("u3"), nil)
	require.NoError(t, err)
	repository.EXPECT().Load(gomock.Any(), session.ID("session")).Return(LoadedSession{
		Header:      session.Header{ID: "session", CreatedAt: time.Unix(1, 0).UTC(), WorkingDirectory: "/project"},
		StoragePath: "/sessions/session.jsonl", Tree: tree,
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}, nil)
	service := New(repository, nil, nil, nil, "/project")

	// Act by restoring the active session after process restart.
	_, restored, err := service.ResumeActive(t.Context(), "session")

	// Assert durable entries remain complete while model context uses the persisted marker.
	require.NoError(t, err)
	require.Len(t, restored, 4)
	require.Len(t, service.ClientSnapshot(), 3)
	contextHistory := service.Snapshot()
	require.Len(t, contextHistory, 3)
	require.Contains(t, contextHistory[0].User.MustGet().Text(""), "summary")
	require.Equal(t, "kept", contextHistory[1].User.MustGet().Text(""))
	require.Equal(t, "new", contextHistory[2].User.MustGet().Text(""))
}
