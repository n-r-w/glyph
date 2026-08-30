package extensions

import (
	"context"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// Handlers returns an ordered snapshot for one session-tree extension point.
func (s *Service) Handlers(kind sessiontree.HandlerKind) []sessiontree.Handler {
	runtimeKind, valid := runtimeHandlerKind(kind)
	if !valid {
		return nil
	}
	registered := s.registeredHandlers(runtimeKind)
	handlers := make([]sessiontree.Handler, len(registered))
	for index := range registered {
		handlers[index] = sessiontree.Handler{
			ExtensionID: registered[index].ExtensionID,
			HandlerID:   registered[index].ID,
		}
	}
	return handlers
}

// HandleRequest invokes one registered session-tree request handler.
func (s *Service) HandleRequest(
	ctx context.Context,
	handler sessiontree.Handler,
	invocation sessiontree.RequestHandlerInvocation,
) (sessiontree.RequestHandlerAction, error) {
	response, err := s.handle(ctx, registeredHandler(handler, HandlerKindSessionBeforeTreeRequest), HandlerRequest{
		SessionBeforeTreeRequest: mo.Some(mapRequestInvocation(invocation)),
		SessionBeforeTreeResult:  mo.None[SessionBeforeTreeResultInvocation](),
		SessionTree:              mo.None[SessionTreeInvocation](),
	})
	if err != nil {
		return sessiontree.RequestHandlerAction{}, err
	}
	action, present := response.SessionBeforeTreeRequest.Get()
	if !present {
		return sessiontree.RequestHandlerAction{}, fmt.Errorf(
			"%w: request handler returned no action",
			ErrExtensionUnavailable,
		)
	}
	return mapRequestAction(action), nil
}

// HandleResult invokes one registered session-tree result handler.
func (s *Service) HandleResult(
	ctx context.Context,
	handler sessiontree.Handler,
	invocation sessiontree.ResultHandlerInvocation,
) (sessiontree.ResultHandlerAction, error) {
	response, err := s.handle(ctx, registeredHandler(handler, HandlerKindSessionBeforeTreeResult), HandlerRequest{
		SessionBeforeTreeRequest: mo.None[SessionBeforeTreeRequestInvocation](),
		SessionBeforeTreeResult:  mo.Some(mapResultInvocation(invocation)),
		SessionTree:              mo.None[SessionTreeInvocation](),
	})
	if err != nil {
		return sessiontree.ResultHandlerAction{}, err
	}
	action, present := response.SessionBeforeTreeResult.Get()
	if !present {
		return sessiontree.ResultHandlerAction{}, fmt.Errorf("%w: result handler returned no action", ErrExtensionUnavailable)
	}
	return mapResultAction(action), nil
}

// Observe invokes one registered post-commit session-tree observer.
func (s *Service) Observe(
	ctx context.Context,
	handler sessiontree.Handler,
	invocation sessiontree.TreeObserverInvocation,
) error {
	_, err := s.handle(ctx, registeredHandler(handler, HandlerKindSessionTree), HandlerRequest{
		SessionBeforeTreeRequest: mo.None[SessionBeforeTreeRequestInvocation](),
		SessionBeforeTreeResult:  mo.None[SessionBeforeTreeResultInvocation](),
		SessionTree: mo.Some(SessionTreeInvocation{
			SessionID: invocation.SessionID, TargetEntryID: invocation.TargetEntryID,
			PrecedingActiveLeafID:   invocation.PrecedingActiveLeafID,
			NavigationDestinationID: invocation.NavigationDestinationID,
			CommittedActiveLeafID:   invocation.CommittedActiveLeafID,
			CreatedSummary: invocation.CreatedSummary.MapValue(func(entry session.Entry) session.Entry {
				return entry.Clone()
			}),
		}),
	})
	return err
}

// runtimeHandlerKind maps the consumer-owned extension point to the runtime registry.
func runtimeHandlerKind(kind sessiontree.HandlerKind) (HandlerKind, bool) {
	switch kind {
	case sessiontree.HandlerKindRequest:
		return HandlerKindSessionBeforeTreeRequest, true
	case sessiontree.HandlerKindResult:
		return HandlerKindSessionBeforeTreeResult, true
	case sessiontree.HandlerKindObserver:
		return HandlerKindSessionTree, true
	default:
		return HandlerKind(0), false
	}
}

// registeredHandler supplies the runtime kind owned by the invoked consumer method.
func registeredHandler(handler sessiontree.Handler, kind HandlerKind) RegisteredHandler {
	return RegisteredHandler{ExtensionID: handler.ExtensionID, ID: handler.HandlerID, Kind: kind}
}

// mapRequestInvocation maps the consumer-owned request invocation to runtime transport state.
func mapRequestInvocation(invocation sessiontree.RequestHandlerInvocation) SessionBeforeTreeRequestInvocation {
	return SessionBeforeTreeRequestInvocation{
		Original:      mapNavigationState(invocation.Original),
		Current:       mapNavigationState(invocation.Current),
		CurrentResult: mapRuntimeSummaryOption(invocation.CurrentResult),
	}
}

// mapResultInvocation maps the consumer-owned result invocation to runtime transport state.
func mapResultInvocation(invocation sessiontree.ResultHandlerInvocation) SessionBeforeTreeResultInvocation {
	return SessionBeforeTreeResultInvocation{
		Original: mapNavigationState(invocation.Original), Current: mapNavigationState(invocation.Current),
		OriginalResult: BranchSummaryResult{
			Summary: invocation.OriginalResult.Summary, Usage: invocation.OriginalResult.Usage,
		},
		CurrentResult: BranchSummaryResult{
			Summary: invocation.CurrentResult.Summary, Usage: invocation.CurrentResult.Usage,
		},
	}
}

// mapNavigationState maps session-tree-owned state without changing preparation semantics.
func mapNavigationState(state sessiontree.HandlerNavigationState) NavigationState {
	return NavigationState{
		SessionID: state.SessionID, PrecedingActiveLeafID: state.PrecedingActiveLeafID,
		Request: NavigationRequest{
			Navigation: state.Request.Navigation, SummaryModel: state.Request.SummaryModel,
		},
		Preparation: state.Preparation,
	}
}

// mapRequestAction maps runtime action values without validating their business semantics.
func mapRequestAction(action SessionBeforeTreeRequestAction) sessiontree.RequestHandlerAction {
	return sessiontree.RequestHandlerAction{
		Cancel: action.Cancel, RequestAction: sessiontree.RequestAction(action.RequestAction),
		Request:      mapSessionTreeRequestOption(action.Request),
		ResultAction: sessiontree.ResultAction(action.ResultAction),
		Result:       mapSessionTreeSummaryOption(action.Result),
	}
}

// mapResultAction maps runtime action values without validating their business semantics.
func mapResultAction(action SessionBeforeTreeResultAction) sessiontree.ResultHandlerAction {
	return sessiontree.ResultHandlerAction{
		Cancel: action.Cancel, ResultAction: sessiontree.ResultAction(action.ResultAction),
		Result: mapSessionTreeSummaryOption(action.Result),
	}
}

// mapRuntimeSummaryOption maps optional consumer summary output to runtime state.
func mapRuntimeSummaryOption(
	value mo.Option[sessiontree.HandlerBranchSummaryResult],
) mo.Option[BranchSummaryResult] {
	result, present := value.Get()
	if !present {
		return mo.None[BranchSummaryResult]()
	}
	return mo.Some(BranchSummaryResult{Summary: result.Summary, Usage: result.Usage})
}

// mapSessionTreeSummaryOption maps optional runtime summary output to consumer state.
func mapSessionTreeSummaryOption(
	value mo.Option[BranchSummaryResult],
) mo.Option[sessiontree.HandlerBranchSummaryResult] {
	result, present := value.Get()
	if !present {
		return mo.None[sessiontree.HandlerBranchSummaryResult]()
	}
	return mo.Some(sessiontree.HandlerBranchSummaryResult{Summary: result.Summary, Usage: result.Usage})
}

// mapSessionTreeRequestOption maps an optional runtime request replacement to consumer state.
func mapSessionTreeRequestOption(
	value mo.Option[NavigationRequest],
) mo.Option[sessiontree.HandlerNavigationRequest] {
	request, present := value.Get()
	if !present {
		return mo.None[sessiontree.HandlerNavigationRequest]()
	}
	return mo.Some(sessiontree.HandlerNavigationRequest{
		Navigation: request.Navigation, SummaryModel: request.SummaryModel,
	})
}
