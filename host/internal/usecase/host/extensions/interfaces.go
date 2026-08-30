package extensions

import (
	"context"
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=extensions

// ErrExtensionUnavailable marks process, transport, or protocol failure that invalidates a runtime.
var ErrExtensionUnavailable = errors.New("extension runtime unavailable")

// HandlerKind identifies one supported session-tree extension point.
type HandlerKind uint8

const (
	// HandlerKindSessionBeforeTreeRequest transforms navigation request state.
	HandlerKindSessionBeforeTreeRequest HandlerKind = iota + 1
	// HandlerKindSessionBeforeTreeResult transforms a completed summary result.
	HandlerKindSessionBeforeTreeResult
	// HandlerKindSessionTree observes committed navigation.
	HandlerKindSessionTree
)

// RequestAction identifies how a request handler changes the current request.
type RequestAction uint8

const (
	// RequestActionPreserve keeps the current request.
	RequestActionPreserve RequestAction = iota + 1
	// RequestActionReplace uses the supplied replacement request.
	RequestActionReplace
)

// ResultAction identifies how a handler changes the optional summary result.
type ResultAction uint8

const (
	// ResultActionPreserve keeps the current result state.
	ResultActionPreserve ResultAction = iota + 1
	// ResultActionReplace sets or replaces the current result.
	ResultActionReplace
	// ResultActionClear removes the current result.
	ResultActionClear
)

// Directory identifies the effective extension catalog and its failure policy.
type Directory struct {
	// Path is the effective extension catalog directory.
	Path string
	// Explicit reports whether the invocation supplied Path.
	Explicit bool
}

// Candidate is one normalized executable extension candidate.
type Candidate struct {
	// ID identifies the extension plugin.
	ID string
	// Path is the extension executable path.
	Path string
}

// Issue reports one isolated catalog or runtime failure.
type Issue struct {
	// PluginIDs identifies affected extension plugins.
	PluginIDs []string
	// Path identifies the failed catalog entry.
	Path string
	// Err contains the isolated discovery or runtime failure.
	Err error
}

// Discovery is one filtered extension catalog.
type Discovery struct {
	// Candidates contains valid executable extension plugins.
	Candidates []Candidate
	// Issues contains isolated catalog failures.
	Issues []Issue
}

// HandlerDescriptor registers one extension-local handler.
type HandlerDescriptor struct {
	// ID identifies the handler within its extension.
	ID string
	// Kind identifies the supported extension point.
	Kind HandlerKind
}

// RegisteredHandler identifies one validated handler and its owning extension.
type RegisteredHandler struct {
	// ExtensionID identifies the owning extension.
	ExtensionID string
	// ID identifies the handler within its extension.
	ID string
	// Kind identifies the supported extension point.
	Kind HandlerKind
}

// Registration contains one extension process complete startup registration.
type Registration struct {
	// Tools contains the complete ordered tool catalog.
	Tools []tool.Descriptor
	// Handlers contains the complete ordered handler catalog.
	Handlers []HandlerDescriptor
}

// LoadedExtension identifies one available extension and its registration.
type LoadedExtension struct {
	// ID identifies the available extension.
	ID string
	// Path is the extension executable path.
	Path string
	// Tools contains the extension-owned tool catalog.
	Tools []tool.Descriptor
	// Handlers contains the extension-owned ordered handler catalog.
	Handlers []HandlerDescriptor
}

// LoadReport contains isolated failures and every available loaded extension.
type LoadReport struct {
	// Issues contains isolated catalog and runtime failures.
	Issues []Issue
	// Extensions contains every available loaded extension.
	Extensions []LoadedExtension
}

// NavigationRequest contains requested navigation behavior and summary model selection.
type NavigationRequest struct {
	// Navigation contains the selected target and summary behavior.
	Navigation sessionnavigation.Request
	// SummaryModel contains the configured summary model selection.
	SummaryModel model.Selection
}

// NavigationState contains one request and the Host context computed for it.
type NavigationState struct {
	// SessionID identifies the active session.
	SessionID string
	// PrecedingActiveLeafID identifies the source position when present.
	PrecedingActiveLeafID mo.Option[string]
	// Request contains requested navigation and summary model selection.
	Request NavigationRequest
	// Preparation contains Host-computed destination and abandoned-path state.
	Preparation session.NavigationPreparation
}

// BranchSummaryResult contains extension-produced summary output.
type BranchSummaryResult struct {
	// Summary contains the nonempty generated context.
	Summary string
	// Usage contains normalized provider usage when reported.
	Usage mo.Option[session.TokenUsage]
}

// SessionBeforeTreeRequestInvocation contains immutable original and current navigation state.
type SessionBeforeTreeRequestInvocation struct {
	// Original contains immutable operation input and Host context.
	Original NavigationState
	// Current contains state produced by preceding request handlers.
	Current NavigationState
	// CurrentResult contains a summary produced by preceding handlers when present.
	CurrentResult mo.Option[BranchSummaryResult]
}

// SessionBeforeTreeResultInvocation contains immutable original state and current summary output.
type SessionBeforeTreeResultInvocation struct {
	// Original contains immutable operation input and Host context.
	Original NavigationState
	// Current contains final request-handler state.
	Current NavigationState
	// OriginalResult contains immutable summary output entering result handling.
	OriginalResult BranchSummaryResult
	// CurrentResult contains output produced by preceding result handlers.
	CurrentResult BranchSummaryResult
}

// SessionTreeInvocation reports one committed navigation.
type SessionTreeInvocation struct {
	// SessionID identifies the active session.
	SessionID string
	// TargetEntryID identifies the selected tree entry.
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

// HandlerRequest is one typed session-tree handler payload.
type HandlerRequest struct {
	// SessionBeforeTreeRequest contains a request-handler invocation when present.
	SessionBeforeTreeRequest mo.Option[SessionBeforeTreeRequestInvocation]
	// SessionBeforeTreeResult contains a result-handler invocation when present.
	SessionBeforeTreeResult mo.Option[SessionBeforeTreeResultInvocation]
	// SessionTree contains a committed-navigation observer invocation when present.
	SessionTree mo.Option[SessionTreeInvocation]
}

// Kind returns the single request kind and whether exactly one payload is present.
func (request HandlerRequest) Kind() (HandlerKind, bool) {
	kind := HandlerKind(0)
	count := 0
	if request.SessionBeforeTreeRequest.IsSome() {
		kind, count = HandlerKindSessionBeforeTreeRequest, count+1
	}
	if request.SessionBeforeTreeResult.IsSome() {
		kind, count = HandlerKindSessionBeforeTreeResult, count+1
	}
	if request.SessionTree.IsSome() {
		kind, count = HandlerKindSessionTree, count+1
	}
	return kind, count == 1
}

// SessionBeforeTreeRequestAction contains changes returned by a request handler.
type SessionBeforeTreeRequestAction struct {
	// Cancel stops navigation without a state commit.
	Cancel bool
	// RequestAction identifies request preservation or replacement.
	RequestAction RequestAction
	// Request contains the replacement request and selection when required.
	Request mo.Option[NavigationRequest]
	// ResultAction identifies result preservation, replacement, or clearing.
	ResultAction ResultAction
	// Result contains the replacement summary when required.
	Result mo.Option[BranchSummaryResult]
}

// SessionBeforeTreeResultAction contains changes returned by a result handler.
type SessionBeforeTreeResultAction struct {
	// Cancel stops navigation without a state commit.
	Cancel bool
	// ResultAction identifies result preservation, replacement, or clearing.
	ResultAction ResultAction
	// Result contains the replacement summary when required.
	Result mo.Option[BranchSummaryResult]
}

// SessionTreeAction acknowledges a committed-navigation observation.
type SessionTreeAction struct{}

// HandlerResponse is one typed session-tree handler action.
type HandlerResponse struct {
	// SessionBeforeTreeRequest contains a request-handler action when present.
	SessionBeforeTreeRequest mo.Option[SessionBeforeTreeRequestAction]
	// SessionBeforeTreeResult contains a result-handler action when present.
	SessionBeforeTreeResult mo.Option[SessionBeforeTreeResultAction]
	// SessionTree contains a committed-navigation observer acknowledgement when present.
	SessionTree mo.Option[SessionTreeAction]
}

// Kind returns the single response kind and whether exactly one action is present.
func (response HandlerResponse) Kind() (HandlerKind, bool) {
	kind := HandlerKind(0)
	count := 0
	if response.SessionBeforeTreeRequest.IsSome() {
		kind, count = HandlerKindSessionBeforeTreeRequest, count+1
	}
	if response.SessionBeforeTreeResult.IsSome() {
		kind, count = HandlerKindSessionBeforeTreeResult, count+1
	}
	if response.SessionTree.IsSome() {
		kind, count = HandlerKindSessionTree, count+1
	}
	return kind, count == 1
}

// Catalog discovers executable extension candidates.
type Catalog interface {
	Discover(ctx context.Context, directory Directory) (Discovery, error)
}

// ExtensionRuntime is one independently managed extension process.
type ExtensionRuntime interface {
	Register(ctx context.Context) (Registration, error)
	Handle(ctx context.Context, handlerID string, request HandlerRequest) (HandlerResponse, error)
	Execute(
		ctx context.Context,
		name string,
		argumentsJSON []byte,
		handleProgress tool.ProgressHandler,
	) (tool.Result, error)
	Done() <-chan struct{}
	Close()
}

// RuntimeFactory starts one candidate.
type RuntimeFactory interface {
	Start(ctx context.Context, candidate Candidate) (ExtensionRuntime, error)
}
