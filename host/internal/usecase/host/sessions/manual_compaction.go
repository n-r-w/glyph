// Package sessions owns session history and compaction state.
package sessions

import (
	"context"
	"slices"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// CaptureCompaction supplies one immutable original branch and prepared model input.
func (s *Service) CaptureCompaction() extensioncontext.CompactionSnapshot {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	entries := cloneEntries(s.active.Tree.ActiveBranch())
	previous := mo.None[session.CompactionEntry]()
	for index := range slices.Backward(entries) {
		if value, present := entries[index].Compaction.Get(); present {
			previous = mo.Some(value.Clone())
			break
		}
	}
	identity := s.ContextSession()
	return extensioncontext.CompactionSnapshot{
		Identity: identity, ActiveLeafID: s.active.Tree.ActiveLeafID(), Entries: entries,
		History: agentrun.ProjectHistory(cloneStoredHistory(s.contextHistory, false)), Previous: previous,
	}
}

// ProjectReplacement supplies the candidate history with complete structural validation causes.
func (s *Service) ProjectReplacement(
	snapshot extensioncontext.CompactionSnapshot,
	candidate session.CompactionEntry,
) ([]agent.HistoryEntry, error) {
	return projectCompactionHistory(snapshot.Entries, snapshot.ActiveLeafID, candidate)
}

// CommitReplacement persists an extension-selected session history replacement.
func (s *Service) CommitReplacement(
	ctx context.Context,
	snapshot extensioncontext.CompactionSnapshot,
	candidate session.CompactionEntry,
	guard extensioncontext.ContextCommitGuard,
	record func(session.Entry),
) (session.Entry, error) {
	return s.commitCompaction(ctx, snapshot.Identity, snapshot.ActiveLeafID, candidate, guard, record)
}
