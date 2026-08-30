package plugin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

const (
	// treeContentSeparator separates public entry text fields.
	treeContentSeparator = " "
	// treeImagePrefix starts an image placeholder.
	treeImagePrefix = "[image: "
	// treeImageSuffix ends an image placeholder.
	treeImageSuffix = "]"
)

// mapTreeRequest maps every Host tree, replacement, label, and failure frame.
func mapTreeRequest(request *uiv1.OpenRequest) (presentationdomain.Event, bool, error) {
	switch {
	case request.GetSessionTree() != nil:
		tree, err := mapSessionTree(request.GetSessionTree().GetTree())
		return treeEvent(presentationdomain.EventSessionTree, presentationdomain.TreeEvent{
			Tree: mo.Some(tree), NavigationStatus: presentationdomain.TreeNavigationUnspecified,
			SessionInfo: mo.None[presentationdomain.SessionInfo](), RestoredTranscript: nil,
			NextInput: mo.None[string](), Issues: nil, FailureMessage: mo.None[string](),
		}), true, err
	case request.GetSessionTreeNavigation() != nil:
		event, err := mapTreeNavigation(request.GetSessionTreeNavigation())
		return treeEvent(presentationdomain.EventSessionTreeNavigation, event), true, err
	case request.GetSessionTreeFailed() != nil:
		failure := request.GetSessionTreeFailed()
		if !failure.HasCode() || !failure.HasMessage() {
			return presentationdomain.Event{}, true, errors.New("session tree failure is incomplete")
		}
		return treeEvent(presentationdomain.EventSessionTreeFailed, presentationdomain.TreeEvent{
			Tree:               mo.None[presentationdomain.SessionTree](),
			NavigationStatus:   presentationdomain.TreeNavigationUnspecified,
			SessionInfo:        mo.None[presentationdomain.SessionInfo](),
			RestoredTranscript: nil,
			NextInput:          mo.None[string](),
			Issues:             nil,
			FailureMessage:     mo.Some(failure.GetMessage()),
		}), true, nil
	case request.GetSessionForked() != nil:
		mapped, err := mapReplacement(request.GetSessionForked().GetSession())
		if err != nil {
			return presentationdomain.Event{}, true, err
		}
		if request.GetSessionForked().HasNextInput() {
			mapped.NextInput = mo.Some(request.GetSessionForked().GetNextInput())
		}
		return treeEvent(presentationdomain.EventSessionForked, mapped), true, nil
	case request.GetSessionCloned() != nil:
		mapped, err := mapReplacement(request.GetSessionCloned().GetSession())
		return treeEvent(presentationdomain.EventSessionCloned, mapped), true, err
	case request.GetEntryLabelSet() != nil:
		tree, err := mapSessionTree(request.GetEntryLabelSet().GetTree())
		return treeEvent(presentationdomain.EventEntryLabelSet, presentationdomain.TreeEvent{
			Tree: mo.Some(tree), NavigationStatus: presentationdomain.TreeNavigationUnspecified,
			SessionInfo: mo.None[presentationdomain.SessionInfo](), RestoredTranscript: nil,
			NextInput: mo.None[string](), Issues: nil, FailureMessage: mo.None[string](),
		}), true, err
	default:
		return presentationdomain.Event{}, false, nil
	}
}

// mapTreeNavigation maps committed or canceled navigation without speculative state.
func mapTreeNavigation(value *uiv1.SessionTreeNavigationResult) (presentationdomain.TreeEvent, error) {
	if value == nil || !value.HasStatus() {
		return presentationdomain.TreeEvent{}, errors.New("session tree navigation status is required")
	}
	issues, err := lo.MapErr(
		value.GetIssues(),
		func(issue *uiv1.OperationIssue, index int) (presentationdomain.OperationIssue, error) {
			if issue == nil || !issue.HasCode() || !issue.HasMessage() {
				return presentationdomain.OperationIssue{}, fmt.Errorf("operation issue %d is incomplete", index)
			}
			return presentationdomain.OperationIssue{
				Code: issue.GetCode().String(), ExtensionID: issue.GetExtensionId(), HandlerID: issue.GetHandlerId(),
				Message: issue.GetMessage(),
			}, nil
		},
	)
	if err != nil {
		return presentationdomain.TreeEvent{}, err
	}
	mapped := presentationdomain.TreeEvent{
		Tree: mo.None[presentationdomain.SessionTree](), NavigationStatus: presentationdomain.TreeNavigationUnspecified,
		SessionInfo: mo.None[presentationdomain.SessionInfo](), RestoredTranscript: nil,
		NextInput: mo.None[string](), Issues: issues, FailureMessage: mo.None[string](),
	}
	switch value.GetStatus() {
	case uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED:
		tree, mapErr := mapSessionTree(value.GetTree())
		if mapErr != nil {
			return presentationdomain.TreeEvent{}, mapErr
		}
		transcript, mapErr := mapRestoredTranscript(value.GetActiveBranch())
		if mapErr != nil {
			return presentationdomain.TreeEvent{}, mapErr
		}
		mapped.Tree = mo.Some(tree)
		mapped.NavigationStatus = presentationdomain.TreeNavigationCommitted
		mapped.RestoredTranscript = transcript
		if value.HasNextInput() {
			mapped.NextInput = mo.Some(value.GetNextInput())
		}
	case uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED:
		mapped.NavigationStatus = presentationdomain.TreeNavigationCanceled
	case uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_UNSPECIFIED:
		return presentationdomain.TreeEvent{}, errors.New("session tree navigation status is unspecified")
	default:
		return presentationdomain.TreeEvent{}, fmt.Errorf(
			"unknown session tree navigation status %d",
			value.GetStatus(),
		)
	}
	return mapped, nil
}

// mapReplacement maps a durable fork or clone result.
func mapReplacement(value *uiv1.SessionChanged) (presentationdomain.TreeEvent, error) {
	if value == nil {
		return presentationdomain.TreeEvent{}, errors.New("replacement session is required")
	}
	info, err := mapSessionInfo(value.GetInfo())
	if err != nil {
		return presentationdomain.TreeEvent{}, err
	}
	transcript, err := mapRestoredTranscript(value.GetEntries())
	if err != nil {
		return presentationdomain.TreeEvent{}, err
	}
	return presentationdomain.TreeEvent{
		Tree: mo.None[presentationdomain.SessionTree](), NavigationStatus: presentationdomain.TreeNavigationUnspecified,
		SessionInfo: mo.Some(info), RestoredTranscript: transcript, NextInput: mo.None[string](), Issues: nil,
		FailureMessage: mo.None[string](),
	}, nil
}

// mapSessionTree maps every public entry in Host persistence order.
func mapSessionTree(value *uiv1.SessionTree) (presentationdomain.SessionTree, error) {
	if value == nil {
		return presentationdomain.SessionTree{}, errors.New("session tree is required")
	}
	entries, err := lo.MapErr(
		value.GetEntries(),
		func(entry *uiv1.SessionTreeEntry, index int) (presentationdomain.TreeEntry, error) {
			mapped, mapErr := mapSessionTreeEntry(entry)
			if mapErr != nil {
				return presentationdomain.TreeEntry{}, fmt.Errorf("map session tree entry %d: %w", index, mapErr)
			}
			return mapped, nil
		},
	)
	if err != nil {
		return presentationdomain.SessionTree{}, err
	}
	activeLeafID := mo.None[string]()
	if value.HasActiveLeafId() {
		activeLeafID = mo.Some(value.GetActiveLeafId())
	}
	return presentationdomain.SessionTree{Entries: entries, ActiveLeafID: activeLeafID}, nil
}

// mapSessionTreeEntry maps one closed public tree payload.
func mapSessionTreeEntry(value *uiv1.SessionTreeEntry) (presentationdomain.TreeEntry, error) {
	if value == nil || !value.HasId() || !value.HasCreatedTime() {
		return presentationdomain.TreeEntry{}, errors.New("session tree entry is incomplete")
	}
	if err := value.GetCreatedTime().CheckValid(); err != nil {
		return presentationdomain.TreeEntry{}, fmt.Errorf("session tree entry time: %w", err)
	}
	parentID := mo.None[string]()
	if value.HasParentId() {
		parentID = mo.Some(value.GetParentId())
	}
	kind, text, err := mapTreeEntryContent(value)
	if err != nil {
		return presentationdomain.TreeEntry{}, err
	}
	return presentationdomain.TreeEntry{
		ID: value.GetId(), ParentID: parentID, CreatedAt: value.GetCreatedTime().AsTime(), Label: value.GetLabel(),
		Kind: kind, Text: text,
	}, nil
}

// mapTreeEntryContent maps public payload text without private extension data.
func mapTreeEntryContent(value *uiv1.SessionTreeEntry) (presentationdomain.TreeEntryKind, string, error) {
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
		return presentationdomain.TreeEntryUser, strings.Join(parts, treeContentSeparator), nil
	case value.GetModel() != nil:
		model := value.GetModel()
		text := model.GetText()
		if text == "" {
			text = model.GetErrorMessage()
		}
		return presentationdomain.TreeEntryModel, text, nil
	case value.GetToolResult() != nil:
		toolResult := value.GetToolResult()
		parts := lo.FilterMap(toolResult.GetContents(), func(content *uiv1.ToolResultContent, _ int) (string, bool) {
			if content != nil && content.HasText() {
				return content.GetText(), true
			}
			return "", false
		})
		return presentationdomain.TreeEntryToolResult,
			strings.TrimSpace(
				toolResult.GetToolName() + treeContentSeparator + strings.Join(parts, treeContentSeparator),
			), nil
	case value.GetExtension() != nil:
		extension := value.GetExtension()
		if !extension.HasExtensionId() || !extension.HasEntryType() {
			return presentationdomain.TreeEntryUnspecified, "", errors.New("extension tree entry is incomplete")
		}
		return presentationdomain.TreeEntryExtension,
			strings.TrimSpace(extension.GetExtensionId() + treeContentSeparator + extension.GetEntryType()), nil
	case value.GetBranchSummary() != nil:
		summary := value.GetBranchSummary()
		if !summary.HasSummary() {
			return presentationdomain.TreeEntryUnspecified, "", errors.New("branch summary text is required")
		}
		return presentationdomain.TreeEntryBranchSummary, summary.GetSummary(), nil
	default:
		return presentationdomain.TreeEntryUnspecified, "", errors.New("session tree entry payload is missing")
	}
}

// treeEvent creates one complete presentation event with a typed tree payload.
func treeEvent(kind presentationdomain.EventKind, tree presentationdomain.TreeEvent) presentationdomain.Event {
	return presentationdomain.Event{
		Kind:                 kind,
		Startup:              nil,
		RestoredTranscript:   nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:            mo.Some(tree),
	}
}

// mapTreeCommand maps one presentation tree command to the public UI contract.
func mapTreeCommand(command presentationdomain.Command) (*uiv1.OpenResponse, bool, error) {
	if command.Kind < presentationdomain.CommandGetSessionTree ||
		command.Kind > presentationdomain.CommandSetEntryLabel {
		return nil, false, nil
	}
	treeCommand, present := command.TreeCommand.Get()
	if !present {
		return nil, true, errors.New("UI tree command payload is missing")
	}
	switch command.Kind {
	case presentationdomain.CommandGetSessionTree:
		//nolint:exhaustruct_v5 // The protobuf builder sets only the active GetSessionTree field.
		return uiv1.OpenResponse_builder{GetSessionTree: &uiv1.GetSessionTreeCommand{}}.Build(), true, nil
	case presentationdomain.CommandNavigateSessionTree:
		response, err := mapNavigateTreeResponse(treeCommand)
		return response, true, err
	case presentationdomain.CommandForkSession:
		response, err := mapForkTreeResponse(treeCommand)
		return response, true, err
	case presentationdomain.CommandCloneSession:
		//nolint:exhaustruct_v5 // The protobuf builder sets only the active CloneSession field.
		return uiv1.OpenResponse_builder{CloneSession: &uiv1.CloneSessionCommand{}}.Build(), true, nil
	case presentationdomain.CommandSetEntryLabel:
		response, err := mapEntryLabelResponse(treeCommand)
		return response, true, err
	case presentationdomain.CommandUnspecified, presentationdomain.CommandSubmit, presentationdomain.CommandStop,
		presentationdomain.CommandRetryAuthentication, presentationdomain.CommandQuit,
		presentationdomain.CommandSelectModel, presentationdomain.CommandSelectReasoningChoice,
		presentationdomain.CommandCreateSession, presentationdomain.CommandListSessions,
		presentationdomain.CommandResumeSession, presentationdomain.CommandSetSessionName,
		presentationdomain.CommandGetSessionInfo:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// mapNavigateTreeResponse maps a validated navigation payload.
func mapNavigateTreeResponse(command presentationdomain.TreeCommand) (*uiv1.OpenResponse, error) {
	targetID, present := command.TargetEntryID.Get()
	if !present || targetID == "" {
		return nil, errors.New("UI tree navigation target is missing")
	}
	summaryMode, err := mapSummaryMode(command.SummaryMode)
	if err != nil {
		return nil, err
	}
	builder := uiv1.NavigateSessionTreeCommand_builder{
		TargetEntryId: new(targetID), SummaryMode: new(summaryMode), CustomFocus: nil,
	}
	if customFocus, customPresent := command.CustomFocus.Get(); customPresent {
		builder.CustomFocus = new(customFocus)
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active NavigateSessionTree field.
	return uiv1.OpenResponse_builder{NavigateSessionTree: builder.Build()}.Build(), nil
}

// mapForkTreeResponse maps a validated fork target.
func mapForkTreeResponse(command presentationdomain.TreeCommand) (*uiv1.OpenResponse, error) {
	targetID, present := command.TargetEntryID.Get()
	if !present || targetID == "" {
		return nil, errors.New("UI fork target is missing")
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active ForkSession field.
	return uiv1.OpenResponse_builder{
		ForkSession: uiv1.ForkSessionCommand_builder{TargetEntryId: new(targetID)}.Build(),
	}.Build(), nil
}

// mapEntryLabelResponse maps a label set or clear payload.
func mapEntryLabelResponse(command presentationdomain.TreeCommand) (*uiv1.OpenResponse, error) {
	targetID, targetPresent := command.TargetEntryID.Get()
	label, labelPresent := command.Label.Get()
	if !targetPresent || targetID == "" || !labelPresent {
		return nil, errors.New("UI entry label command is incomplete")
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active SetEntryLabel field.
	return uiv1.OpenResponse_builder{
		SetEntryLabel: uiv1.SetEntryLabelCommand_builder{TargetEntryId: new(targetID), Label: new(label)}.Build(),
	}.Build(), nil
}

// mapSummaryMode maps one closed presentation summary mode.
func mapSummaryMode(mode presentationdomain.SummaryMode) (uiv1.SummaryMode, error) {
	switch mode {
	case presentationdomain.SummaryModeNoSummary:
		return uiv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY, nil
	case presentationdomain.SummaryModeSummarize:
		return uiv1.SummaryMode_SUMMARY_MODE_SUMMARIZE, nil
	case presentationdomain.SummaryModeCustomFocus:
		return uiv1.SummaryMode_SUMMARY_MODE_SUMMARIZE_WITH_CUSTOM_PROMPT, nil
	case presentationdomain.SummaryModeUnspecified:
		return uiv1.SummaryMode_SUMMARY_MODE_UNSPECIFIED, errors.New("UI summary mode is unspecified")
	default:
		return uiv1.SummaryMode_SUMMARY_MODE_UNSPECIFIED, fmt.Errorf("unknown UI summary mode %d", mode)
	}
}
