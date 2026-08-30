package ui

import (
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
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

// navigationFrame projects committed navigation state.
func navigationFrame(result sessionnavigation.Result) (domainui.Frame, error) {
	if result.Canceled {
		return canceledNavigationFrame(mapOperationIssues(result.Issues)), nil
	}
	tree, err := mapSessionTree(result.Tree)
	if err != nil {
		return domainui.Frame{}, err
	}
	branch, err := mapSessionEntries(result.ActiveBranch)
	if err != nil {
		return domainui.Frame{}, fmt.Errorf("map active branch: %w", err)
	}
	frame := emptyTreeFrame(domainui.FrameSessionTreeNavigation)
	frame.TreeNavigation = mo.Some(domainui.TreeNavigationResult{
		Status: domainui.TreeNavigationStatusCommitted,
		Committed: mo.Some(domainui.TreeNavigationCommitted{
			Tree: tree, ActiveBranch: branch, NextInput: result.NextInput,
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

// treeFailureFrame reports one closed navigation failure.
func treeFailureFrame(code domainui.TreeFailureCode, message string) domainui.Frame {
	frame := emptyTreeFrame(domainui.FrameSessionTreeFailed)
	frame.TreeFailure = mo.Some(domainui.TreeFailure{Code: code, Message: message})
	return frame
}

// emptyTreeFrame initializes absent fields for one tree result frame.
func emptyTreeFrame(kind domainui.FrameKind) domainui.Frame {
	return domainui.Frame{
		Kind: kind, Initialization: mo.None[domainui.Initialization](), Lifecycle: mo.None[domainui.Lifecycle](),
		AuthorizationURL: mo.None[string](), Text: mo.None[string](), RetryAuthentication: mo.None[bool](),
		ModelSelection: mo.None[domainui.ModelSelection](), SessionInfo: mo.None[session.Info](), Sessions: nil,
		SessionEntries: nil, SessionStatistics: mo.None[session.Statistics](),
		SessionTree: mo.None[domainui.SessionTree](), TreeNavigation: mo.None[domainui.TreeNavigationResult](),
		TreeFailure: mo.None[domainui.TreeFailure](),
	}
}

// mapSessionTree projects every tree entry in persistence order.
func mapSessionTree(tree session.Tree) (domainui.SessionTree, error) {
	entries := tree.Entries()
	labels := tree.Labels()
	mapped := make([]domainui.SessionTreeEntry, 0, len(entries))
	for index := range entries {
		entry, err := mapSessionTreeEntry(entries[index], labels[entries[index].ID])
		if err != nil {
			return domainui.SessionTree{}, fmt.Errorf("map session tree entry %d: %w", index, err)
		}
		mapped = append(mapped, entry)
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
		BranchSummary: mo.None[domainui.BranchSummary](),
	}
	if extension, present := entry.Extension.Get(); present {
		mapped.Kind = domainui.SessionTreeEntryExtension
		mapped.Extension = mo.Some(domainui.ExtensionEntry{
			ExtensionID: extension.ExtensionID, EntryType: extension.EntryType,
		})
		return mapped, nil
	}
	if summary, present := entry.BranchSummary.Get(); present {
		mapped.Kind = domainui.SessionTreeEntryBranchSummary
		mapped.BranchSummary = mo.Some(domainui.BranchSummary{
			Summary: summary.Summary, FirstEntryID: summary.FirstEntryID, LastEntryID: summary.LastEntryID,
			Provider: summary.Provider, Model: summary.Model, ReasoningChoice: summary.ReasoningChoice,
			Usage: summary.Usage, EstimatedCost: summary.EstimatedCost,
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
	switch public.Kind {
	case domainui.SessionEntryUser:
		mapped.Kind = domainui.SessionTreeEntryUser
	case domainui.SessionEntryModel:
		mapped.Kind = domainui.SessionTreeEntryModel
	case domainui.SessionEntryToolResult:
		mapped.Kind = domainui.SessionTreeEntryToolResult
	default:
		return domainui.SessionTreeEntry{}, fmt.Errorf("unknown tree entry payload %d", public.Kind)
	}
	return mapped, nil
}

// mapOperationIssues projects safe ordered navigation issues for UI delivery.
func mapOperationIssues(issues []sessionnavigation.OperationIssue) []domainui.OperationIssue {
	mapped := make([]domainui.OperationIssue, len(issues))
	for index := range issues {
		mapped[index] = domainui.OperationIssue{
			Code: domainui.OperationIssueCode(issues[index].Code), ExtensionID: issues[index].ExtensionID,
			HandlerID: issues[index].HandlerID, Message: issues[index].Message,
		}
	}
	return mapped
}
