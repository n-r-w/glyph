package sessiontree

import (
	"context"
	"fmt"

	"github.com/samber/mo"
)

// invokeRequestHandler invokes one accepted request handler and validates its response variant.
func (s *Service) invokeRequestHandler(
	ctx context.Context,
	handler Handler,
	invocation RequestHandlerInvocation,
) (RequestHandlerAction, error) {
	response, err := s.runtime.HandleHandler(ctx, handler.ExtensionID, handler.HandlerID, HandlerRequest{
		Request:  mo.Some(invocation),
		Result:   mo.None[ResultHandlerInvocation](),
		Observer: mo.None[TreeObserverInvocation](),
	})
	if err != nil {
		return RequestHandlerAction{}, err
	}
	action, present := response.Request.Get()
	kind, valid := response.Kind()
	if !present || !valid || kind != HandlerKindRequest {
		return RequestHandlerAction{}, fmt.Errorf("request handler %q returned no valid action", handler.HandlerID)
	}
	return action, nil
}

// invokeResultHandler invokes one accepted result handler and validates its response variant.
func (s *Service) invokeResultHandler(
	ctx context.Context,
	handler Handler,
	invocation ResultHandlerInvocation,
) (ResultHandlerAction, error) {
	response, err := s.runtime.HandleHandler(ctx, handler.ExtensionID, handler.HandlerID, HandlerRequest{
		Request:  mo.None[RequestHandlerInvocation](),
		Result:   mo.Some(invocation),
		Observer: mo.None[TreeObserverInvocation](),
	})
	if err != nil {
		return ResultHandlerAction{}, err
	}
	action, present := response.Result.Get()
	kind, valid := response.Kind()
	if !present || !valid || kind != HandlerKindResult {
		return ResultHandlerAction{}, fmt.Errorf("result handler %q returned no valid action", handler.HandlerID)
	}
	return action, nil
}

// invokeObserver invokes one accepted observer and validates its acknowledgement variant.
func (s *Service) invokeObserver(ctx context.Context, handler Handler, invocation TreeObserverInvocation) error {
	response, err := s.runtime.HandleHandler(ctx, handler.ExtensionID, handler.HandlerID, HandlerRequest{
		Request:  mo.None[RequestHandlerInvocation](),
		Result:   mo.None[ResultHandlerInvocation](),
		Observer: mo.Some(invocation),
	})
	if err != nil {
		return err
	}
	_, present := response.Observer.Get()
	kind, valid := response.Kind()
	if !present || !valid || kind != HandlerKindObserver {
		return fmt.Errorf("session-tree observer %q returned no valid action", handler.HandlerID)
	}
	return nil
}
