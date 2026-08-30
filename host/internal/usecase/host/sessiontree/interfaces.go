// Package sessiontree coordinates internal session-tree navigation.
package sessiontree

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=sessiontree

// BranchSummaryDraft contains validated model output for the atomic session commit.
type BranchSummaryDraft struct {
	// Summary contains the nonempty generated context.
	Summary string
	// FirstEntryID identifies the first abandoned entry.
	FirstEntryID string
	// LastEntryID identifies the preceding active leaf.
	LastEntryID string
	// CommonAncestorID identifies the last entry shared with the destination branch.
	CommonAncestorID mo.Option[string]
	// Selection identifies the exact configured model used.
	Selection model.Selection
	// Usage contains normalized provider usage when reported.
	Usage mo.Option[session.TokenUsage]
}

// CommitCommand contains one optimistic navigation mutation and optional summary.
type CommitCommand struct {
	// ExpectedActiveLeafID identifies the snapshot used to prepare navigation.
	ExpectedActiveLeafID mo.Option[string]
	// DestinationID identifies the navigation destination or implicit root.
	DestinationID mo.Option[string]
	// BranchSummary contains generated summary data when summarization succeeded.
	BranchSummary mo.Option[BranchSummaryDraft]
}

// ActiveSession exposes the tree snapshot and atomic navigation commit needed by navigation.
type ActiveSession interface {
	// Tree returns an independent active-session tree snapshot.
	Tree() session.Tree
	// CommitNavigation persists one destination change and optional summary when the active leaf is unchanged.
	CommitNavigation(context.Context, CommitCommand) (session.Tree, error)
}

// ModelCompleter executes configured provider requests without changing active selection.
type ModelCompleter interface {
	// Selection returns the current configured provider, model, and reasoning choice.
	Selection() model.Selection
	// CompleteConfigured executes the exact configured selection with no active-selection mutation.
	CompleteConfigured(context.Context, model.Selection, string, []agent.HistoryEntry) (model.Response, error)
}
