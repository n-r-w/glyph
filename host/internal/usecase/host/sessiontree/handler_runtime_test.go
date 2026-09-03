//go:build !integration

package sessiontree

import (
	"fmt"

	"github.com/samber/mo"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// registerTestHandlers publishes handlers and makes their runtimes available to one navigation test.
func registerTestHandlers(service *Service, runtime *MockRuntime, kind HandlerKind, handlers []Handler) {
	registrations := make([]startup.AcceptedRegistration, 0, len(handlers))
	for _, handler := range handlers {
		registrations = append(registrations, startup.AcceptedRegistration{
			ID: handler.ExtensionID, Path: "", Tools: nil,
			Handlers: []startup.AcceptedHandler{{ID: handler.HandlerID, Kind: testRawHandlerKind(kind)}},
		})
		runtime.EXPECT().HandlerRuntimeAvailable(handler.ExtensionID).Return(true).AnyTimes()
	}
	service.CommitHandlers(registrations)
}

// testRawHandlerKind maps one test handler kind to the startup contract.
func testRawHandlerKind(kind HandlerKind) startup.RawHandlerKind {
	switch kind {
	case HandlerKindRequest:
		return startup.RawHandlerKindSessionBeforeTreeRequest
	case HandlerKindResult:
		return startup.RawHandlerKindSessionBeforeTreeResult
	case HandlerKindObserver:
		return startup.RawHandlerKindSessionTree
	default:
		return startup.RawHandlerKindUnspecified
	}
}

// expectRequestHandler configures one low-level request-handler invocation.
func expectRequestHandler(
	runtime *MockRuntime,
	handler Handler,
	invocation any,
	action RequestHandlerAction,
	err error,
) *gomock.Call {
	response := HandlerResponse{
		Request: mo.Some(action), Result: mo.None[ResultHandlerAction](), Observer: mo.None[ObserverAction](),
	}
	return runtime.EXPECT().HandleHandler(
		gomock.Any(), handler.ExtensionID, handler.HandlerID,
		handlerRequestMatcher{kind: HandlerKindRequest, invocation: asMatcher(invocation)},
	).Return(response, err)
}

// expectResultHandler configures one low-level result-handler invocation.
func expectResultHandler(
	runtime *MockRuntime,
	handler Handler,
	invocation any,
	action ResultHandlerAction,
	err error,
) *gomock.Call {
	response := HandlerResponse{
		Request: mo.None[RequestHandlerAction](), Result: mo.Some(action), Observer: mo.None[ObserverAction](),
	}
	return runtime.EXPECT().HandleHandler(
		gomock.Any(), handler.ExtensionID, handler.HandlerID,
		handlerRequestMatcher{kind: HandlerKindResult, invocation: asMatcher(invocation)},
	).Return(response, err)
}

// expectObserver configures one low-level observer invocation.
func expectObserver(runtime *MockRuntime, handler Handler, invocation any, err error) *gomock.Call {
	response := HandlerResponse{
		Request: mo.None[RequestHandlerAction](), Result: mo.None[ResultHandlerAction](),
		Observer: mo.Some(ObserverAction{}),
	}
	return runtime.EXPECT().HandleHandler(
		gomock.Any(), handler.ExtensionID, handler.HandlerID,
		handlerRequestMatcher{kind: HandlerKindObserver, invocation: asMatcher(invocation)},
	).Return(response, err)
}

// handlerRequestMatcher matches one selected handler payload through an existing invocation matcher.
type handlerRequestMatcher struct {
	// kind identifies the expected handler request variant.
	kind HandlerKind
	// invocation matches the selected invocation payload.
	invocation gomock.Matcher
}

// Matches reports whether one handler request contains the expected single payload.
func (matcher handlerRequestMatcher) Matches(value any) bool {
	request, ok := value.(HandlerRequest)
	if !ok {
		return false
	}
	kind, valid := request.Kind()
	if !valid || kind != matcher.kind {
		return false
	}
	switch matcher.kind {
	case HandlerKindRequest:
		invocation, present := request.Request.Get()
		return present && matcher.invocation.Matches(invocation)
	case HandlerKindResult:
		invocation, present := request.Result.Get()
		return present && matcher.invocation.Matches(invocation)
	case HandlerKindObserver:
		invocation, present := request.Observer.Get()
		return present && matcher.invocation.Matches(invocation)
	default:
		return false
	}
}

// String describes the expected handler request variant.
func (matcher handlerRequestMatcher) String() string {
	return fmt.Sprintf("handler request kind %d with %s", matcher.kind, matcher.invocation.String())
}

// asMatcher preserves explicit gomock matchers and wraps concrete values with equality matching.
func asMatcher(value any) gomock.Matcher {
	if matcher, ok := value.(gomock.Matcher); ok {
		return matcher
	}
	return gomock.Eq(value)
}
