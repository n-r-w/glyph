package ui

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/internal/operation"
)

// sessionTreeFrame projects the complete tree without private extension payload bytes.
func sessionTreeFrame(tree session.Tree) (domainui.Frame, error) {
	mapped, err := mapSessionTree(tree)
	if err != nil {
		return domainui.Frame{}, err
	}
	frame := emptyTreeFrame(domainui.FrameSessionTree)
	frame.SessionTree = mo.Some(mapped)
	return frame, nil
}

// navigationProgressCallback maps and enqueues committed navigation state.
func navigationProgressCallback(
	reporter operation.Reporter[domainui.Frame],
) func(sessionnavigation.Progress) error {
	return func(progress sessionnavigation.Progress) error {
		tree, err := mapSessionTree(progress.Tree)
		if err != nil {
			return err
		}
		branch, err := mapSessionEntries(progress.ActiveBranch)
		if err != nil {
			return fmt.Errorf("map active branch: %w", err)
		}
		frame := emptyTreeFrame(domainui.FrameSessionTreeNavigationProgress)
		frame.TreeNavigationProgress = mo.Some(domainui.TreeNavigationProgress{Tree: tree, ActiveBranch: branch})
		return reporter.Report(frame)
	}
}

// navigationFrame projects terminal navigation metadata.
func navigationFrame(result sessionnavigation.Result) (domainui.Frame, error) {
	if result.Canceled {
		return canceledNavigationFrame(mapOperationIssues(result.Issues)), nil
	}
	createdSummary := mo.None[domainui.SessionTreeEntry]()
	if entry, present := result.CreatedSummary.Get(); present {
		mapped, err := mapSessionTreeEntry(entry, "")
		if err != nil {
			return domainui.Frame{}, fmt.Errorf("map created branch summary: %w", err)
		}
		if mapped.BranchSummary.IsNone() {
			return domainui.Frame{}, errors.New("map created branch summary: summary payload is required")
		}
		createdSummary = mo.Some(mapped)
	}
	frame := emptyTreeFrame(domainui.FrameSessionTreeNavigation)
	frame.TreeNavigation = mo.Some(domainui.TreeNavigationResult{
		Status: domainui.TreeNavigationStatusCommitted,
		Committed: mo.Some(domainui.TreeNavigationCommitted{
			DestinationID:  result.DestinationID,
			ActiveLeafID:   result.ActiveLeafID,
			CreatedSummary: createdSummary,
			NextInput:      result.NextInput,
		}),
		Issues: mapOperationIssues(result.Issues),
	})
	return frame, nil
}

// canceledNavigationFrame reports cancellation without speculative state.
func canceledNavigationFrame(issues []domainui.OperationIssue) domainui.Frame {
	frame := emptyTreeFrame(domainui.FrameSessionTreeNavigation)
	frame.TreeNavigation = mo.Some(domainui.TreeNavigationResult{
		Status:    domainui.TreeNavigationStatusCanceled,
		Committed: mo.None[domainui.TreeNavigationCommitted](), Issues: issues,
	})
	return frame
}

// emptyTreeFrame initializes absent fields for one tree result frame.
func emptyTreeFrame(kind domainui.FrameKind) domainui.Frame {
	return domainui.Frame{
		Kind: kind, Initialization: mo.None[domainui.Initialization](), Lifecycle: mo.None[domainui.Lifecycle](),
		AuthorizationURL: mo.None[string](), Text: mo.None[string](), ErrorCode: mo.None[string](),
		ModelSelection: mo.None[domainui.ModelSelection](), SessionInfo: mo.None[session.Info](), Sessions: nil,
		SessionEntries: nil, SessionStatistics: mo.None[session.Statistics](),
		SessionTree:            mo.None[domainui.SessionTree](),
		TreeNavigationProgress: mo.None[domainui.TreeNavigationProgress](),
		TreeNavigation:         mo.None[domainui.TreeNavigationResult](),
		SessionEntryAdded:      mo.None[domainui.SessionTreeEntry](),
	}
}

// mapSessionTree projects every tree entry in persistence order.
func mapSessionTree(tree session.Tree) (domainui.SessionTree, error) {
	entries := tree.Entries()
	labels := tree.Labels()
	mapped, err := lo.MapErr(entries, func(entry session.Entry, index int) (domainui.SessionTreeEntry, error) {
		result, mapErr := mapSessionTreeEntry(entry, labels[entry.ID])
		if mapErr != nil {
			return domainui.SessionTreeEntry{}, fmt.Errorf("map session tree entry %d: %w", index, mapErr)
		}
		return result, nil
	})
	if err != nil {
		return domainui.SessionTree{}, err
	}
	return domainui.SessionTree{Entries: mapped, ActiveLeafID: tree.ActiveLeafID()}, nil
}

// mapSessionTreeEntry maps one closed tree payload without extension data.
func mapSessionTreeEntry(entry session.Entry, label string) (domainui.SessionTreeEntry, error) {
	mapped := domainui.SessionTreeEntry{
		ID: entry.ID, ParentID: entry.ParentID, CreatedAt: entry.CreatedAt, Label: label,
		Kind: domainui.SessionTreeEntryUnspecified,
		User: mo.None[model.Message](), Model: mo.None[domainui.ModelResponse](),
		ToolResult: mo.None[agent.ToolResult](), Extension: mo.None[domainui.ExtensionEntry](),
		BranchSummary: mo.None[domainui.BranchSummary](), ExtensionMessage: mo.None[domainui.ExtensionMessage](),
	}
	if extension, present := entry.Extension.Get(); present {
		mapped.Kind = domainui.SessionTreeEntryExtension
		mapped.Extension = mo.Some(domainui.ExtensionEntry{
			ExtensionID: extension.ExtensionID, EntryType: extension.EntryType,
		})
		return mapped, nil
	}
	if message, present := entry.ExtensionMessage.Get(); present {
		mapped.Kind = domainui.SessionTreeEntryExtensionMessage
		mapped.ExtensionMessage = mo.Some(domainui.ExtensionMessage{
			ExtensionID: message.ExtensionID, EntryType: message.EntryType,
			Text: message.Text, Visibility: message.Visibility,
		})
		return mapped, nil
	}
	projected, err := mapSessionEntries([]session.Entry{entry})
	if err != nil {
		return domainui.SessionTreeEntry{}, err
	}
	if len(projected) != 1 {
		return domainui.SessionTreeEntry{}, errors.New("tree entry has no public payload")
	}
	public := projected[0]
	mapped.User = public.User
	mapped.Model = public.Model
	mapped.ToolResult = public.ToolResult
	mapped.BranchSummary = public.BranchSummary
	mapped.ExtensionMessage = public.ExtensionMessage
	switch public.Kind {
	case domainui.SessionEntryUser:
		mapped.Kind = domainui.SessionTreeEntryUser
	case domainui.SessionEntryModel:
		mapped.Kind = domainui.SessionTreeEntryModel
	case domainui.SessionEntryToolResult:
		mapped.Kind = domainui.SessionTreeEntryToolResult
	case domainui.SessionEntryBranchSummary:
		mapped.Kind = domainui.SessionTreeEntryBranchSummary
	case domainui.SessionEntryExtensionMessage:
		mapped.Kind = domainui.SessionTreeEntryExtensionMessage
	default:
		return domainui.SessionTreeEntry{}, fmt.Errorf("unknown tree entry payload %d", public.Kind)
	}
	return mapped, nil
}

// mapOperationIssues projects safe ordered navigation issues for UI delivery.
func mapOperationIssues(issues []sessionnavigation.OperationIssue) []domainui.OperationIssue {
	return lo.Map(issues, func(issue sessionnavigation.OperationIssue, _ int) domainui.OperationIssue {
		return domainui.OperationIssue{
			Code: domainui.OperationIssueCode(issue.Code), ExtensionID: issue.ExtensionID,
			HandlerID: issue.HandlerID, Message: issue.Message,
		}
	})
}
