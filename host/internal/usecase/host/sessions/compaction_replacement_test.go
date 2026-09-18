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

// TestForkAndCloneRebuildCompactedContextForRetainedBranch verifies replacement-session projection.
func TestForkAndCloneRebuildCompactedContextForRetainedBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		replace    func(*testing.T, *Service) error
		wantLength int
		wantFirst  string
	}{
		{
			name: "fork before compaction",
			replace: func(t *testing.T, service *Service) error {
				t.Helper()
				_, _, nextInput, err := service.ForkActive(t.Context(), "u2")
				require.Equal(t, "kept", nextInput)
				return err
			},
			wantLength: 1,
			wantFirst:  "old",
		},
		{
			name: "clone after compaction",
			replace: func(t *testing.T, service *Service) error {
				t.Helper()
				_, _, err := service.CloneActive(t.Context())
				return err
			},
			wantLength: 3,
			wantFirst:  "summary",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one source session with a compaction marker and later original message.
			controller := gomock.NewController(t)
			repository := NewMockRepository(controller)
			ids := NewMockIDGenerator(controller)
			clock := NewMockClock(controller)
			entries := []session.Entry{
				compactionUserEntry("u1", mo.None[string](), "old"),
				compactionUserEntry("u2", mo.Some("u1"), "kept"),
				compactionMarkerEntry("c1", "u2", "summary", "u2"),
				compactionUserEntry("u3", mo.Some("c1"), "new"),
			}
			tree, err := session.NewTree(entries, mo.Some("u3"), nil)
			require.NoError(t, err)
			ids.EXPECT().NewID().Return("replacement", nil)
			clock.EXPECT().Now().Return(time.Unix(10, 0).UTC())
			repository.EXPECT().CreateSnapshot(gomock.Any(), gomock.Any()).Return(
				CreateSnapshotResult{StoragePath: "/sessions/replacement.jsonl"}, nil,
			)
			service := New(repository, ids, clock, nil, "/project")
			service.active = LoadedSession{
				Header: session.Header{
					ID:               "source",
					CreatedAt:        time.Unix(1, 0).UTC(),
					WorkingDirectory: "/project",
				},
				StoragePath:          "/sessions/source.jsonl",
				Tree:                 tree,
				Information:          mo.None[session.Information](),
				InformationUpdatedAt: mo.None[time.Time](),
			}

			// Act by creating the selected replacement session.
			err = test.replace(t, service)

			// Assert model context reflects only markers retained by that replacement branch.
			require.NoError(t, err)
			contextHistory := service.Snapshot()
			require.Len(t, contextHistory, test.wantLength)
			require.Contains(t, contextHistory[0].User.MustGet().Text(""), test.wantFirst)
		})
	}
}
