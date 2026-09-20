package sessions

import (
	"errors"
	"slices"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
)

// ephemeralCompactionID identifies the temporary marker used only while projecting compacted history.
const ephemeralCompactionID = "__glyph_compaction_projection__"

// CompactionSnapshot returns one detached active-branch and model-context snapshot under one read lock.
func (s *Service) CompactionSnapshot() contextcompaction.Snapshot {
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
	return contextcompaction.Snapshot{
		Identity:     identity,
		ActiveLeafID: s.active.Tree.ActiveLeafID(), Entries: entries,
		Context: cloneStoredHistory(s.contextHistory, false), Previous: previous,
	}
}

// ProjectCompaction projects a captured branch with one candidate compaction applied.
func (s *Service) ProjectCompaction(entries []session.Entry, compaction session.CompactionEntry) []agent.HistoryEntry {
	if len(entries) == 0 {
		return nil
	}
	owned := cloneEntries(entries)
	leafID := owned[len(owned)-1].ID
	tree, err := session.NewTree(owned, mo.Some(leafID), nil)
	if err != nil || tree.ValidateCompactionBoundary(compaction.FirstKeptEntryID, mo.Some(leafID)) != nil {
		return nil
	}
	owned = append(owned, session.Entry{
		ID: ephemeralCompactionID, ParentID: mo.Some(leafID), CreatedAt: time.Time{},
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), ExtensionMessage: mo.None[session.ExtensionMessage](),
		EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(compaction.Clone()),
	})
	return agentrun.ProjectHistory(compactedHistoryFromEntries(owned))
}

// ProjectSuffix projects a captured branch suffix through the shared Core history algorithm.
func (s *Service) ProjectSuffix(entries []session.Entry, firstKeptEntryID string) ([]agent.HistoryEntry, error) {
	if len(entries) == 0 {
		return nil, errors.New("captured active branch is empty")
	}
	leafID := entries[len(entries)-1].ID
	tree, err := session.NewTree(entries, mo.Some(leafID), nil)
	if err != nil {
		return nil, err
	}
	if boundaryErr := tree.ValidateCompactionBoundary(firstKeptEntryID, mo.Some(leafID)); boundaryErr != nil {
		return nil, boundaryErr
	}
	boundary := -1
	for index := range entries {
		if entries[index].ID == firstKeptEntryID {
			boundary = index
			break
		}
	}
	if boundary < 0 {
		return nil, errors.New("compaction suffix boundary is outside captured branch")
	}
	return agentrun.ProjectHistory(historyFromEntries(entries[boundary:])), nil
}
