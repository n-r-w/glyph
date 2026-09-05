package sessions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// CommitNavigation atomically persists and publishes one active-leaf change with an optional branch summary.
func (s *Service) CommitNavigation(ctx context.Context, command sessiontree.CommitCommand) (session.Tree, error) {
	if err := ctx.Err(); err != nil {
		return session.Tree{}, err
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return session.Tree{}, err
	}
	if s.writeUnavailable {
		return session.Tree{}, session.ErrPersistenceUnavailable
	}
	if s.active.Tree.ActiveLeafID() != command.ExpectedActiveLeafID {
		return session.Tree{}, errors.New("commit session navigation: active leaf changed")
	}

	candidateTree := s.active.Tree.Clone()
	if err := candidateTree.SetActiveLeaf(command.DestinationID); err != nil {
		return session.Tree{}, fmt.Errorf("validate session navigation destination: %w", err)
	}
	summaryEntry := mo.None[session.Entry]()
	if draft, present := command.BranchSummary.Get(); present {
		if err := validateBranchSummaryDraft(s.active.Tree, command.ExpectedActiveLeafID, draft); err != nil {
			return session.Tree{}, fmt.Errorf("validate branch summary: %w", err)
		}
		entry, err := s.buildBranchSummaryEntry(draft, command.DestinationID)
		if err != nil {
			return session.Tree{}, err
		}
		if candidateErr := candidateTree.Add(entry); candidateErr != nil {
			return session.Tree{}, fmt.Errorf("append branch summary candidate: %w", candidateErr)
		}
		summaryEntry = mo.Some(entry)
	}
	if err := ctx.Err(); err != nil {
		return session.Tree{}, err
	}
	result, err := s.repository.Apply(ctx, ApplyCommand{
		Header:      s.active.Header,
		StoragePath: s.active.StoragePath,
		Mutation: Mutation{
			Entry: mo.None[session.Entry](),
			Navigation: mo.Some(NavigationMutation{
				DestinationID: command.DestinationID,
				BranchSummary: summaryEntry,
			}),
			Label:              mo.None[LabelMutation](),
			SessionInformation: mo.None[SessionInformationMutation](),
		},
	})
	if err != nil {
		logPersistenceFailure(ctx, persistenceOperationNavigation, s.active.Header.ID, err)
		// Keep the preceding durable snapshot readable while blocking later process-local mutations.
		s.writeUnavailable = true
		return session.Tree{}, fmt.Errorf("%w: commit session navigation: %w", session.ErrPersistenceUnavailable, err)
	}

	// Publish the complete destination and optional summary only after storage synchronization.
	s.active.StoragePath = result.StoragePath
	s.active.Tree = candidateTree
	s.history = sessiontree.HistoryFromEntries(candidateTree.ActiveBranch())
	return candidateTree.Clone(), nil
}

// buildBranchSummaryEntry assigns identity, time, and cost for the actual summary source.
func (s *Service) buildBranchSummaryEntry(
	draft sessiontree.BranchSummaryDraft,
	destinationID mo.Option[string],
) (session.Entry, error) {
	id, err := s.ids.NewID()
	if err != nil {
		return session.Entry{}, fmt.Errorf("create branch summary entry ID: %w", err)
	}
	// Non-model work has no estimated cost; model work uses only its own reported usage.
	cost := mo.None[session.EstimatedCost]()
	if modelSource, present := draft.Source.Model.Get(); present {
		cost = s.estimatedUsageCost(modelSource.Selection.Provider, modelSource.Selection.Model, modelSource.Usage)
	}
	entry := session.Entry{
		ID: id, ParentID: destinationID, CreatedAt: s.clock.Now(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.Some(session.BranchSummaryEntry{
			Summary: draft.Summary, FirstEntryID: draft.FirstEntryID, LastEntryID: draft.LastEntryID,
			Source: draft.Source, EstimatedCost: cost,
		}),
	}
	summary := entry.BranchSummary.OrEmpty()
	if validationErr := summary.ValidateAccounting(); validationErr != nil {
		return session.Entry{}, validationErr
	}
	return entry, nil
}

// validateBranchSummaryDraft checks exact active-path provenance and persisted field invariants.
func validateBranchSummaryDraft(
	tree session.Tree,
	expectedActiveLeafID mo.Option[string],
	draft sessiontree.BranchSummaryDraft,
) error {
	if strings.TrimSpace(draft.Summary) == "" {
		return errors.New("branch summary text is empty")
	}
	if err := draft.Source.Validate(); err != nil {
		return err
	}
	last, hasLast := expectedActiveLeafID.Get()
	if !hasLast || draft.LastEntryID != last {
		return errors.New("branch summary must end at the preceding active leaf")
	}
	activeBranch := tree.ActiveBranch()
	firstIndex := 0
	if ancestor, present := draft.CommonAncestorID.Get(); present {
		firstIndex = -1
		for index := range activeBranch {
			if activeBranch[index].ID == ancestor {
				firstIndex = index + 1
				break
			}
		}
		if firstIndex < 0 {
			return errors.New("branch summary common ancestor is not active")
		}
	}
	if firstIndex >= len(activeBranch) || activeBranch[firstIndex].ID != draft.FirstEntryID {
		return errors.New("branch summary must start after the last common ancestor")
	}
	boundary := session.BranchSummaryEntry{
		Summary: draft.Summary, FirstEntryID: draft.FirstEntryID, LastEntryID: draft.LastEntryID,
		Source:        draft.Source,
		EstimatedCost: mo.None[session.EstimatedCost](),
	}
	return tree.ValidateSummaryBoundary(boundary)
}
