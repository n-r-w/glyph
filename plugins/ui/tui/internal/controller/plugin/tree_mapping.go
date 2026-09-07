package plugin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const (
	// treeContentSeparator separates public entry text fields.
	treeContentSeparator = " "
	// treeImagePrefix starts an image placeholder.
	treeImagePrefix = "[image: "
	// treeImageSuffix ends an image placeholder.
	treeImageSuffix = "]"
)

// mapTreeRequest maps every Host tree, replacement, and label completion.
func mapTreeRequest(request *uiv1.HostCompleted) (Payload, bool, error) {
	switch {
	case request.GetSessionTree() != nil:
		tree, err := mapSessionTree(request.GetSessionTree().GetTree())
		return TreePayload(TreeUpdate{
			Kind:             TreeSnapshot,
			Tree:             mo.Some(tree),
			NavigationStatus: TreeNavigationUnspecified,
			SessionInfo:      mo.None[SessionInfo](),
			Transcript:       nil,
			NextInput:        mo.None[string](),
			Issues:           nil,
			AddedEntry:       mo.None[TreeEntry](),
		}), true, err
	case request.GetSessionTreeNavigation() != nil:
		event, err := mapTreeNavigation(request.GetSessionTreeNavigation())
		event.Kind = TreeNavigationCompleted
		return TreePayload(event), true, err
	case request.GetSessionForked() != nil:
		mapped, err := mapReplacement(request.GetSessionForked().GetSession())
		if err != nil {
			return Payload{}, true, err
		}
		if request.GetSessionForked().HasNextInput() {
			mapped.NextInput = mo.Some(request.GetSessionForked().GetNextInput())
		}
		mapped.Kind = TreeForked
		return TreePayload(mapped), true, nil
	case request.GetSessionCloned() != nil:
		mapped, err := mapReplacement(request.GetSessionCloned().GetSession())
		mapped.Kind = TreeCloned
		return TreePayload(mapped), true, err
	case request.GetEntryLabelSet() != nil:
		tree, err := mapSessionTree(request.GetEntryLabelSet().GetTree())
		return TreePayload(TreeUpdate{
			Kind:             TreeLabelSet,
			Tree:             mo.Some(tree),
			NavigationStatus: TreeNavigationUnspecified,
			SessionInfo:      mo.None[SessionInfo](),
			Transcript:       nil,
			NextInput:        mo.None[string](),
			Issues:           nil,
			AddedEntry:       mo.None[TreeEntry](),
		}), true, err
	default:
		return Payload{}, false, nil
	}
}

// mapTreeNavigation maps committed or canceled navigation without speculative state.
// mapTreeNavigationProgress maps committed state before terminal observer completion.
func mapTreeNavigationProgress(
	value *uiv1.SessionTreeNavigationProgress,
) (Payload, error) {
	if value == nil || !value.HasTree() {
		return Payload{}, errors.New("session tree navigation progress is incomplete")
	}
	tree, err := mapSessionTree(value.GetTree())
	if err != nil {
		return Payload{}, err
	}
	transcript, err := mapRestoredTranscript(value.GetActiveBranch())
	if err != nil {
		return Payload{}, err
	}
	return TreePayload(TreeUpdate{
		Kind:             TreeNavigationProgress,
		Tree:             mo.Some(tree),
		NavigationStatus: TreeNavigationUnspecified,
		SessionInfo:      mo.None[SessionInfo](),
		Transcript:       transcript,
		NextInput:        mo.None[string](),
		Issues:           nil,
		AddedEntry:       mo.None[TreeEntry](),
	}), nil
}

// mapTreeNavigation maps terminal navigation metadata.
func mapTreeNavigation(value *uiv1.SessionTreeNavigationResult) (TreeUpdate, error) {
	if value == nil || !value.HasStatus() {
		return TreeUpdate{}, errors.New("session tree navigation status is required")
	}
	issues, err := lo.MapErr(
		value.GetIssues(),
		func(issue *uiv1.OperationIssue, index int) (OperationIssue, error) {
			if issue == nil || !issue.HasCode() || !issue.HasMessage() {
				return OperationIssue{}, fmt.Errorf("operation issue %d is incomplete", index)
			}
			return OperationIssue{
				Code: issue.GetCode().String(), ExtensionID: issue.GetExtensionId(), HandlerID: issue.GetHandlerId(),
				Message: issue.GetMessage(),
			}, nil
		},
	)
	if err != nil {
		return TreeUpdate{}, err
	}
	mapped := TreeUpdate{
		Kind:             TreeNavigationCompleted,
		Tree:             mo.None[SessionTree](),
		NavigationStatus: TreeNavigationUnspecified,
		SessionInfo:      mo.None[SessionInfo](),
		Transcript:       nil,
		NextInput:        mo.None[string](),
		Issues:           issues,
		AddedEntry:       mo.None[TreeEntry](),
	}
	switch value.GetStatus() {
	case uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED:
		mapped.NavigationStatus = TreeNavigationCommitted
		if value.HasNextInput() {
			mapped.NextInput = mo.Some(value.GetNextInput())
		}
	case uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED:
		mapped.NavigationStatus = TreeNavigationCanceled
	case uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_UNSPECIFIED:
		return TreeUpdate{}, errors.New("session tree navigation status is unspecified")
	default:
		return TreeUpdate{}, fmt.Errorf(
			"unknown session tree navigation status %d",
			value.GetStatus(),
		)
	}
	return mapped, nil
}

// mapReplacement maps a durable fork or clone result.
func mapReplacement(value *uiv1.SessionChanged) (TreeUpdate, error) {
	if value == nil {
		return TreeUpdate{}, errors.New("replacement session is required")
	}
	info, err := mapSessionInfo(value.GetInfo())
	if err != nil {
		return TreeUpdate{}, err
	}
	transcript, err := mapRestoredTranscript(value.GetEntries())
	if err != nil {
		return TreeUpdate{}, err
	}
	return TreeUpdate{
		Kind: TreeCloned,
		Tree: mo.None[SessionTree](), NavigationStatus: TreeNavigationUnspecified,
		SessionInfo: mo.Some(info), Transcript: transcript, NextInput: mo.None[string](), Issues: nil,
		AddedEntry: mo.None[TreeEntry](),
	}, nil
}

// mapSessionTree maps every public entry in Host persistence order.
func mapSessionTree(value *uiv1.SessionTree) (SessionTree, error) {
	if value == nil {
		return SessionTree{}, errors.New("session tree is required")
	}
	entries, err := lo.MapErr(
		value.GetEntries(),
		func(entry *uiv1.SessionTreeEntry, index int) (TreeEntry, error) {
			mapped, mapErr := mapSessionTreeEntry(entry)
			if mapErr != nil {
				return TreeEntry{}, fmt.Errorf("map session tree entry %d: %w", index, mapErr)
			}
			return mapped, nil
		},
	)
	if err != nil {
		return SessionTree{}, err
	}
	activeLeafID := mo.None[string]()
	if value.HasActiveLeafId() {
		activeLeafID = mo.Some(value.GetActiveLeafId())
	}
	return SessionTree{Entries: entries, ActiveLeafID: activeLeafID}, nil
}

// mapSessionTreeEntry maps one closed public tree payload.
func mapSessionTreeEntry(value *uiv1.SessionTreeEntry) (TreeEntry, error) {
	if value == nil || !value.HasId() || !value.HasCreatedTime() {
		return TreeEntry{}, errors.New("session tree entry is incomplete")
	}
	if err := value.GetCreatedTime().CheckValid(); err != nil {
		return TreeEntry{}, fmt.Errorf("session tree entry time: %w", err)
	}
	parentID := mo.None[string]()
	if value.HasParentId() {
		parentID = mo.Some(value.GetParentId())
	}
	kind, text, err := mapTreeEntryContent(value)
	if err != nil {
		return TreeEntry{}, err
	}
	message := mo.None[ExtensionMessage]()
	if value.GetExtensionMessage() != nil {
		mapped, mapErr := mapTreeExtensionMessage(value.GetExtensionMessage())
		if mapErr != nil {
			return TreeEntry{}, mapErr
		}
		message = mo.Some(mapped)
	}
	return TreeEntry{
		ID: value.GetId(), ParentID: parentID, CreatedAt: value.GetCreatedTime().AsTime(), Label: value.GetLabel(),
		Kind: kind, ExtensionMessage: message, Text: text,
	}, nil
}

// mapTreeEntryContent maps public payload text without private extension data.
//
//nolint:gocyclo // The closed tree union validates each payload before presentation.
func mapTreeEntryContent(value *uiv1.SessionTreeEntry) (TreeEntryKind, string, error) {
	switch {
	case value.GetUser() != nil:
		parts := lo.FilterMap(value.GetUser().GetContent(), func(content *uiv1.UserContent, _ int) (string, bool) {
			if content == nil {
				return "", false
			}
			if content.HasText() {
				return content.GetText(), true
			}
			if image := content.GetImage(); image != nil {
				return treeImagePrefix + image.GetMediaType() + treeImageSuffix, true
			}
			return "", false
		})
		return TreeEntryUser, strings.Join(parts, treeContentSeparator), nil
	case value.GetModel() != nil:
		model := value.GetModel()
		text := model.GetText()
		if text == "" {
			text = model.GetErrorMessage()
		}
		return TreeEntryModel, text, nil
	case value.GetToolResult() != nil:
		toolResult := value.GetToolResult()
		parts := lo.FilterMap(toolResult.GetContents(), func(content *uiv1.ToolResultContent, _ int) (string, bool) {
			if content != nil && content.HasText() {
				return content.GetText(), true
			}
			return "", false
		})
		return TreeEntryToolResult,
			strings.TrimSpace(
				toolResult.GetToolName() + treeContentSeparator + strings.Join(parts, treeContentSeparator),
			), nil
	case value.GetExtension() != nil:
		extension := value.GetExtension()
		if !extension.HasExtensionId() || !extension.HasEntryType() {
			return TreeEntryUnspecified, "", errors.New("extension tree entry is incomplete")
		}
		return TreeEntryExtension,
			strings.TrimSpace(extension.GetExtensionId() + treeContentSeparator + extension.GetEntryType()), nil
	case value.GetExtensionMessage() != nil:
		message, err := mapTreeExtensionMessage(value.GetExtensionMessage())
		if err != nil {
			return TreeEntryUnspecified, "", err
		}
		return TreeEntryExtensionMessage, message.Text, nil
	case value.GetBranchSummary() != nil:
		summary := value.GetBranchSummary()
		if !summary.HasSummary() {
			return TreeEntryUnspecified, "", errors.New("branch summary text is required")
		}
		return TreeEntryBranchSummary, summary.GetSummary(), nil
	default:
		return TreeEntryUnspecified, "", errors.New("session tree entry payload is missing")
	}
}

// mapTreeExtensionMessage validates exact message data and closed visibility.
func mapTreeExtensionMessage(value *uiv1.ExtensionMessage) (ExtensionMessage, error) {
	if value == nil || !value.HasExtensionId() || !value.HasEntryType() || !value.HasText() || !value.HasVisibility() {
		return ExtensionMessage{}, errors.New("extension message tree entry is incomplete")
	}
	visibility := ClientVisibilityVisible
	switch value.GetVisibility() {
	case uiv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE:
	case uiv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN:
		visibility = ClientVisibilityHidden
	case uiv1.ClientVisibility_CLIENT_VISIBILITY_UNSPECIFIED:
		return ExtensionMessage{}, errors.New("extension message tree visibility is unspecified")
	default:
		return ExtensionMessage{}, errors.New("extension message tree visibility is unknown")
	}
	return ExtensionMessage{
		ExtensionID: value.GetExtensionId(), EntryType: value.GetEntryType(),
		Text: value.GetText(), Visibility: visibility,
	}, nil
}
