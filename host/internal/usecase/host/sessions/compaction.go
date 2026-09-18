package sessions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
)

var _ contextcompaction.SessionState = (*Service)(nil)

// CommitCompaction validates and appends one compaction entry to the expected active branch.
func (s *Service) CommitCompaction(
	ctx context.Context,
	expected contextcompaction.SessionIdentity,
	expectedLeafID mo.Option[string],
	compaction session.CompactionEntry,
) (session.Entry, error) {
	if err := validateCompactionPayload(compaction); err != nil {
		return session.Entry{}, err
	}
	s.mutex.Lock()
	if err := s.validateExpectedSessionLocked(ctx, expected); err != nil {
		s.mutex.Unlock()
		return session.Entry{}, err
	}
	if !equalOptionalString(s.active.Tree.ActiveLeafID(), expectedLeafID) {
		s.mutex.Unlock()
		return session.Entry{}, fmt.Errorf("%w: active branch changed before compaction commit", session.ErrUnavailable)
	}
	if err := s.active.Tree.ValidateCompactionBoundary(compaction.FirstKeptEntryID, expectedLeafID); err != nil {
		s.mutex.Unlock()
		return session.Entry{}, err
	}
	entry := session.Entry{
		ID: "", ParentID: mo.None[string](), CreatedAt: time.Time{},
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.None[session.ExtensionMessage](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(compaction.Clone()),
	}
	committed, err := s.appendEntryLocked(ctx, entry)
	if err != nil {
		s.mutex.Unlock()
		if errors.Is(err, agent.ErrPersistenceUnavailable) {
			return session.Entry{}, fmt.Errorf("%w: %w", session.ErrPersistenceUnavailable, err)
		}
		return session.Entry{}, err
	}
	// Rebuild model context only after the durable tree contains the marker.
	s.contextHistory = storedCompactedHistoryFromEntries(s.active.Tree.ActiveBranch())
	var wait func(context.Context) error
	var publishErr error
	if s.publisher == nil {
		publishErr = errors.New("compaction entry publisher is not bound")
	} else {
		wait, publishErr = s.publisher.PublishSessionEntry(committed)
	}
	s.mutex.Unlock()
	if publishErr != nil {
		return committed, fmt.Errorf("publish committed compaction entry: %w", publishErr)
	}
	if wait == nil {
		return committed, errors.New("publish committed compaction entry: delivery wait is required")
	}
	if waitErr := wait(ctx); waitErr != nil {
		return committed, fmt.Errorf("deliver committed compaction entry: %w", waitErr)
	}
	return committed, nil
}

// validateCompactionPayload checks fields that do not require active-branch state.
func validateCompactionPayload(compaction session.CompactionEntry) error {
	if strings.TrimSpace(compaction.Summary) == "" {
		return errors.New("compaction summary is empty")
	}
	if strings.TrimSpace(compaction.FirstKeptEntryID) == "" {
		return errors.New("compaction first kept entry ID is empty")
	}
	if err := compaction.ValidateAccounting(); err != nil {
		return fmt.Errorf("validate compaction accounting: %w", err)
	}
	return nil
}

// equalOptionalString compares optional branch identities without treating absence as an empty identifier.
func equalOptionalString(left, right mo.Option[string]) bool {
	leftValue, leftPresent := left.Get()
	rightValue, rightPresent := right.Get()
	return leftPresent == rightPresent && (!leftPresent || leftValue == rightValue)
}
