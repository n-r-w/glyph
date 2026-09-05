// Package sessiontree coordinates internal session-tree navigation.
package sessiontree

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=sessiontree

// BranchSummaryDraft contains validated summary output for the atomic session commit.
type BranchSummaryDraft struct {
	// Source identifies the actual producer and its model usage.
	Source session.BranchSummarySource
	// Summary contains the nonempty generated context.
	Summary string
	// FirstEntryID identifies the first abandoned entry.
	FirstEntryID string
	// LastEntryID identifies the preceding active leaf.
	LastEntryID string
	// CommonAncestorID identifies the last entry shared with the destination branch.
	CommonAncestorID mo.Option[string]
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
	// SessionID returns the active session identifier.
	SessionID() string
	// Tree returns an independent active-session tree snapshot.
	Tree() session.Tree
	// CommitNavigation persists one destination change and optional summary when the active leaf is unchanged.
	CommitNavigation(context.Context, CommitCommand) (session.Tree, error)
}

// HandlerKind identifies one session-tree extension point.
type HandlerKind uint8

const (
	// HandlerKindRequest transforms navigation request and optional result state.
	HandlerKindRequest HandlerKind = iota + 1
	// HandlerKindResult transforms an existing branch-summary result.
	HandlerKindResult
	// HandlerKindObserver observes committed navigation.
	HandlerKindObserver
)

// Handler identifies one registered extension handler.
type Handler struct {
	// ExtensionID identifies the owning extension.
	ExtensionID string
	// HandlerID identifies the handler within its extension.
	HandlerID string
}

// RequestAction identifies how a request handler changes the current request.
type RequestAction uint8

const (
	// RequestActionPreserve keeps the current request.
	RequestActionPreserve RequestAction = iota + 1
	// RequestActionReplace uses the supplied replacement request.
	RequestActionReplace
)

// ResultAction identifies how a handler changes summary result state.
type ResultAction uint8

const (
	// ResultActionPreserve keeps the current result state.
	ResultActionPreserve ResultAction = iota + 1
	// ResultActionReplace sets or replaces the current result.
	ResultActionReplace
	// ResultActionClear removes the current result.
	ResultActionClear
)

// HandlerNavigationRequest contains navigation behavior and its summary-model selection.
type HandlerNavigationRequest struct {
	// Navigation contains the selected target and summary behavior.
	Navigation sessionnavigation.Request
	// SummaryModel contains the configured model used for summarization.
	SummaryModel model.Selection
}

// HandlerNavigationState contains one request and its Host-computed preparation.
type HandlerNavigationState struct {
	// SessionID identifies the active session.
	SessionID string
	// PrecedingActiveLeafID identifies the source position when present.
	PrecedingActiveLeafID mo.Option[string]
	// Request contains navigation behavior and summary-model selection.
	Request HandlerNavigationRequest
	// Preparation contains the destination and abandoned-path state.
	Preparation session.NavigationPreparation
}

// HandlerBranchSummaryResult contains extension-visible summary output.
type HandlerBranchSummaryResult struct {
	// Source identifies the actual producer and its model usage.
	Source session.BranchSummarySource
	// Summary contains generated branch context.
	Summary string
}

// RequestHandlerInvocation contains immutable original state and mutable current state.
type RequestHandlerInvocation struct {
	// Original contains the immutable operation input and preparation.
	Original HandlerNavigationState
	// Current contains state produced by preceding request handlers.
	Current HandlerNavigationState
	// CurrentResult contains a summary produced by a preceding handler when present.
	CurrentResult mo.Option[HandlerBranchSummaryResult]
}

// ResultHandlerInvocation contains immutable original values and mutable summary output.
type ResultHandlerInvocation struct {
	// Original contains the immutable operation input and preparation.
	Original HandlerNavigationState
	// Current contains the final request-handler state.
	Current HandlerNavigationState
	// OriginalResult contains immutable summary output entering result handling.
	OriginalResult HandlerBranchSummaryResult
	// CurrentResult contains output produced by preceding result handlers.
	CurrentResult HandlerBranchSummaryResult
}

// TreeObserverInvocation reports one committed navigation.
type TreeObserverInvocation struct {
	// SessionID identifies the active session.
	SessionID string
	// TargetEntryID identifies the final selected tree entry.
	TargetEntryID string
	// PrecedingActiveLeafID identifies the source position when present.
	PrecedingActiveLeafID mo.Option[string]
	// NavigationDestinationID identifies the committed destination when present.
	NavigationDestinationID mo.Option[string]
	// CommittedActiveLeafID identifies the committed active position when present.
	CommittedActiveLeafID mo.Option[string]
	// CreatedSummary contains the complete committed summary entry when present.
	CreatedSummary mo.Option[session.Entry]
}

// RequestHandlerAction changes request-handler state or cancels navigation.
type RequestHandlerAction struct {
	// Cancel stops navigation without a state commit.
	Cancel bool
	// RequestAction identifies request preservation or replacement.
	RequestAction RequestAction
	// Request contains the replacement request when required.
	Request mo.Option[HandlerNavigationRequest]
	// ResultAction identifies result preservation, replacement, or clearing.
	ResultAction ResultAction
	// Result contains the replacement summary when required.
	Result mo.Option[HandlerBranchSummaryResult]
}

// ResultHandlerAction changes summary output or cancels navigation.
type ResultHandlerAction struct {
	// Cancel stops navigation without a state commit.
	Cancel bool
	// ResultAction identifies result preservation or replacement.
	ResultAction ResultAction
	// Result contains the replacement summary when required.
	Result mo.Option[HandlerBranchSummaryResult]
}

// ObserverAction acknowledges one committed-navigation observation.
type ObserverAction struct{}

// HandlerRequest is one typed session-tree handler payload.
type HandlerRequest struct {
	// Request contains a request-handler invocation when present.
	Request mo.Option[RequestHandlerInvocation]
	// Result contains a result-handler invocation when present.
	Result mo.Option[ResultHandlerInvocation]
	// Observer contains a committed-navigation observer invocation when present.
	Observer mo.Option[TreeObserverInvocation]
}

// Kind returns the single request kind and whether exactly one payload is present.
func (request HandlerRequest) Kind() (HandlerKind, bool) {
	kind := HandlerKind(0)
	count := 0
	if request.Request.IsSome() {
		kind, count = HandlerKindRequest, count+1
	}
	if request.Result.IsSome() {
		kind, count = HandlerKindResult, count+1
	}
	if request.Observer.IsSome() {
		kind, count = HandlerKindObserver, count+1
	}
	return kind, count == 1
}

// HandlerResponse is one typed session-tree handler action.
type HandlerResponse struct {
	// Request contains a request-handler action when present.
	Request mo.Option[RequestHandlerAction]
	// Result contains a result-handler action when present.
	Result mo.Option[ResultHandlerAction]
	// Observer contains an observer acknowledgement when present.
	Observer mo.Option[ObserverAction]
}

// Kind returns the single response kind and whether exactly one action is present.
func (response HandlerResponse) Kind() (HandlerKind, bool) {
	kind := HandlerKind(0)
	count := 0
	if response.Request.IsSome() {
		kind, count = HandlerKindRequest, count+1
	}
	if response.Result.IsSome() {
		kind, count = HandlerKindResult, count+1
	}
	if response.Observer.IsSome() {
		kind, count = HandlerKindObserver, count+1
	}
	return kind, count == 1
}

// Runtime supplies availability and low-level invocation for one accepted handler.
type Runtime interface {
	// HandlerRuntimeAvailable reports whether one accepted extension can handle operations.
	HandlerRuntimeAvailable(extensionID string) bool
	// HandleHandler invokes one handler on its owning extension runtime.
	HandleHandler(
		ctx context.Context,
		extensionID string,
		handlerID string,
		request HandlerRequest,
	) (HandlerResponse, error)
}

// ModelRequester supplies active selection and validates models only when executing requests.
type ModelRequester interface {
	// ActiveSelection returns the active provider, model, and reasoning choice.
	ActiveSelection() model.Selection
	// Request executes one model request without changing the active selection.
	Request(
		ctx context.Context,
		selection model.Selection,
		instructions string,
		history []agent.HistoryEntry,
	) (model.Response, error)
}
