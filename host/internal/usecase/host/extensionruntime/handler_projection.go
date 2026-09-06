package extensionruntime

import (
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// projectHandler separates capability state from the filtered process operation.
func (s *Service) projectHandler(request sessiontree.HandlerRequest) (HandlerInvocation, error) {
	kind, valid := request.Kind()
	if !valid {
		return HandlerInvocation{}, errors.New("handler request has no single payload")
	}
	result := HandlerInvocation{
		Context:        request.Context,
		Kind:           InvocationKind(kind),
		Original:       Preparation{},
		Current:        Preparation{},
		OriginalResult: mo.None[Summary](),
		CurrentResult:  mo.None[Summary](),
		Commit:         mo.None[TreeCommit](),
	}
	if value, ok := request.Request.Get(); ok {
		result.Original = projectPreparation(value.Original)
		result.Current = projectPreparation(value.Current)
		result.CurrentResult = projectSummary(value.CurrentResult)
	}
	if value, ok := request.Result.Get(); ok {
		result.Original = projectPreparation(value.Original)
		result.Current = projectPreparation(value.Current)
		result.OriginalResult = projectSummary(mo.Some(value.OriginalResult))
		result.CurrentResult = projectSummary(mo.Some(value.CurrentResult))
	}
	if value, ok := request.Observer.Get(); ok {
		created := mo.None[CommittedSummary]()
		if entry, exists := value.CreatedSummary.Get(); exists {
			if summary, present := entry.BranchSummary.Get(); present {
				created = mo.Some(CommittedSummary{ID: entry.ID, Summary: summary})
			}
		}
		result.Commit = mo.Some(
			TreeCommit{
				SessionID:               value.SessionID,
				TargetEntryID:           value.TargetEntryID,
				PrecedingActiveLeafID:   value.PrecedingActiveLeafID,
				NavigationDestinationID: value.NavigationDestinationID,
				CommittedActiveLeafID:   value.CommittedActiveLeafID,
				CreatedSummary:          created,
			},
		)
	}
	return result, nil
}

// projectNavigation preserves raw navigation intent for the process boundary.
func projectNavigation(request sessiontree.HandlerNavigationRequest) Navigation {
	return Navigation{
		TargetEntryID: request.Navigation.TargetEntryID,
		SummaryMode:   int32(request.Navigation.SummaryMode),
		CustomFocus:   request.Navigation.CustomFocus,
		SummaryModel:  request.SummaryModel,
	}
}

// projectPreparation excludes internal tree preparation fields and private entry payloads.
func projectPreparation(state sessiontree.HandlerNavigationState) Preparation {
	entries := make([]TreeEntry, len(state.Preparation.AbandonedPath))
	for index := range state.Preparation.AbandonedPath {
		entries[index] = projectTreeEntry(state.Preparation.AbandonedPath[index])
	}
	return Preparation{
		SessionID:             state.SessionID,
		PrecedingActiveLeafID: state.PrecedingActiveLeafID,
		Request:               projectNavigation(state.Request),
		DestinationID:         state.Preparation.DestinationID,
		CommonAncestorID:      state.Preparation.CommonAncestorID,
		Entries:               entries,
	}
}

// projectTreeEntry removes provider replay context and hidden extension state before transport sees them.
func projectTreeEntry(entry session.Entry) TreeEntry {
	result := TreeEntry{
		ID:               entry.ID,
		User:             entry.User,
		Model:            mo.None[[]Content](),
		ToolResult:       entry.ToolResult,
		BranchSummary:    mo.None[string](),
		Extension:        mo.None[ExtensionIdentity](),
		ExtensionMessage: entry.ExtensionMessage,
	}
	if response, present := entry.Model.Get(); present {
		content := make([]Content, len(response.Content))
		for index := range response.Content {
			content[index] = projectContent(response.Content[index])
		}
		result.Model = mo.Some(content)
	}
	if summary, present := entry.BranchSummary.Get(); present {
		result.BranchSummary = mo.Some(summary.Summary)
	}
	if hidden, present := entry.Extension.Get(); present {
		result.Extension = mo.Some(ExtensionIdentity{ExtensionID: hidden.ExtensionID, EntryType: hidden.EntryType})
	}
	return result
}

// projectContent excludes opaque provider context while retaining public value presence.
func projectContent(content model.Content) Content {
	return Content{Kind: content.Kind, Text: content.Text, ToolCall: content.ToolCall}
}

// projectSummary retains result absence independently from empty summary text.
func projectSummary(result mo.Option[sessiontree.HandlerBranchSummaryResult]) mo.Option[Summary] {
	value, present := result.Get()
	if !present {
		return mo.None[Summary]()
	}
	return mo.Some(Summary{Source: value.Source, Summary: value.Summary})
}

// capabilityNavigation converts raw process intent without deciding whether the capability accepts it.
func capabilityNavigation(value Navigation) sessiontree.HandlerNavigationRequest {
	mode := sessiontree.SummaryMode(0)
	if value.SummaryMode >= int32(sessiontree.SummaryModeNoSummary) &&
		value.SummaryMode <= int32(sessiontree.SummaryModeSummarizeWithCustomPrompt) {
		mode = sessiontree.SummaryMode(value.SummaryMode)
	}
	return sessiontree.HandlerNavigationRequest{
		Navigation: sessiontree.NavigationRequest{
			TargetEntryID: value.TargetEntryID,
			SummaryMode:   mode,
			CustomFocus:   value.CustomFocus,
		},
		SummaryModel: value.SummaryModel,
	}
}

// capabilityAction maps transport-validated variants without applying cancel or replacement policy.
func (s *Service) capabilityAction(action HandlerAction) sessiontree.HandlerResponse {
	request := mo.None[sessiontree.HandlerNavigationRequest]()
	if value, present := action.Request.Get(); present {
		request = mo.Some(capabilityNavigation(value))
	}
	summary := mo.None[sessiontree.HandlerBranchSummaryResult]()
	if value, present := action.Result.Get(); present {
		summary = mo.Some(sessiontree.HandlerBranchSummaryResult{Source: value.Source, Summary: value.Summary})
	}
	requestAction := sessiontree.RequestAction(0)
	if action.RequestAction >= int32(sessiontree.RequestActionPreserve) &&
		action.RequestAction <= int32(sessiontree.RequestActionReplace) {
		requestAction = sessiontree.RequestAction(action.RequestAction)
	}
	resultAction := sessiontree.ResultAction(0)
	if action.ResultAction >= int32(sessiontree.ResultActionPreserve) &&
		action.ResultAction <= int32(sessiontree.ResultActionClear) {
		resultAction = sessiontree.ResultAction(action.ResultAction)
	}
	result := sessiontree.HandlerResponse{
		Request:  mo.None[sessiontree.RequestHandlerAction](),
		Result:   mo.None[sessiontree.ResultHandlerAction](),
		Observer: mo.None[sessiontree.ObserverAction](),
	}
	switch action.Kind {
	case InvocationRequest:
		result.Request = mo.Some(
			sessiontree.RequestHandlerAction{
				Cancel:        action.Cancel,
				RequestAction: requestAction,
				Request:       request,
				ResultAction:  resultAction,
				Result:        summary,
			},
		)
	case InvocationResult:
		result.Result = mo.Some(
			sessiontree.ResultHandlerAction{Cancel: action.Cancel, ResultAction: resultAction, Result: summary},
		)
	case InvocationObserver:
		result.Observer = mo.Some(sessiontree.ObserverAction{})
	}
	return result
}
