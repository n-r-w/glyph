// Package contextcompaction owns conversation context sizing and compaction coordination.
package contextcompaction

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=contextcompaction

// SessionState supplies active-session identity and compaction persistence.
type SessionState interface {
	// ContextSession returns one atomic active-session identity snapshot.
	ContextSession() session.Identity
	// CompactionSnapshot returns one detached active-branch and model-context snapshot.
	CompactionSnapshot() Snapshot
	// ProjectCompaction projects the supplied captured branch with one candidate compaction applied.
	ProjectCompaction(entries []session.Entry, compaction session.CompactionEntry) []agent.HistoryEntry
	// ProjectSuffix projects a captured branch suffix through the shared Core history algorithm.
	ProjectSuffix(entries []session.Entry, firstKeptEntryID string) ([]agent.HistoryEntry, error)
	// CommitCompaction validates and appends one compaction entry to the expected active branch.
	CommitCompaction(
		ctx context.Context,
		expected session.Identity,
		expectedLeafID mo.Option[string],
		compaction session.CompactionEntry,
	) (session.Entry, error)
}
