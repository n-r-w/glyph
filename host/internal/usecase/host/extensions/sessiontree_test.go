//go:build !integration

package extensions

import (
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// TestServiceMapsSessionTreeRequestHandler verifies the consumer-owned runner contract reaches typed runtime dispatch.
func (s *ServiceSuite) TestServiceMapsSessionTreeRequestHandler() {
	t := s.T()

	// Arrange one registered request handler and one complete consumer-owned invocation.
	runtime := NewMockExtensionRuntime(s.controller)
	s.catalog.EXPECT().Discover(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(Discovery{
		Candidates: []Candidate{{ID: "tree", Path: "/tree"}}, Issues: nil,
	}, nil)
	s.factory.EXPECT().Start(t.Context(), Candidate{ID: "tree", Path: "/tree"}).Return(runtime, nil)
	runtime.EXPECT().Register(t.Context()).Return(Registration{Tools: nil, Handlers: []HandlerDescriptor{
		{ID: "request", Kind: HandlerKindSessionBeforeTreeRequest},
	}}, nil)
	service := New(s.catalog, s.factory, discardRuntimeFailure)
	_, err := service.Load(t.Context(), Directory{Path: "/plugins", Explicit: true})
	require.NoError(t, err)
	handler := sessiontree.Handler{ExtensionID: "tree", HandlerID: "request"}
	request := sessionnavigation.Request{
		TargetEntryID: "target", SummaryMode: sessionnavigation.SummaryModeNoSummary,
		CustomFocus: mo.None[string](),
	}
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	preparation := session.NavigationPreparation{
		DestinationID: mo.None[string](), NextInput: mo.None[string](),
		CommonAncestorID: mo.None[string](), AbandonedPath: nil,
	}
	state := sessiontree.HandlerNavigationState{
		SessionID: "session", PrecedingActiveLeafID: mo.None[string](),
		Request:     sessiontree.HandlerNavigationRequest{Navigation: request, SummaryModel: selection},
		Preparation: preparation,
	}
	runtime.EXPECT().Handle(t.Context(), "request", HandlerRequest{
		SessionBeforeTreeRequest: mo.Some(SessionBeforeTreeRequestInvocation{
			Original: NavigationState{
				SessionID: "session", PrecedingActiveLeafID: mo.None[string](),
				Request: NavigationRequest{Navigation: request, SummaryModel: selection}, Preparation: preparation,
			},
			Current: NavigationState{
				SessionID: "session", PrecedingActiveLeafID: mo.None[string](),
				Request: NavigationRequest{Navigation: request, SummaryModel: selection}, Preparation: preparation,
			},
			CurrentResult: mo.None[BranchSummaryResult](),
		}),
		SessionBeforeTreeResult: mo.None[SessionBeforeTreeResultInvocation](),
		SessionTree:             mo.None[SessionTreeInvocation](),
	}).Return(HandlerResponse{
		SessionBeforeTreeRequest: mo.Some(SessionBeforeTreeRequestAction{
			Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[NavigationRequest](),
			ResultAction: ResultActionClear, Result: mo.None[BranchSummaryResult](),
		}),
		SessionBeforeTreeResult: mo.None[SessionBeforeTreeResultAction](),
		SessionTree:             mo.None[SessionTreeAction](),
	}, nil)

	// Act through the session-tree-owned interface.
	registered := service.Handlers(sessiontree.HandlerKindRequest)
	action, err := service.HandleRequest(t.Context(), handler, sessiontree.RequestHandlerInvocation{
		Original: state, Current: state, CurrentResult: mo.None[sessiontree.HandlerBranchSummaryResult](),
	})

	// Assert registration order and typed actions map without applying session-tree validation.
	require.NoError(t, err)
	assert.Equal(t, []sessiontree.Handler{handler}, registered)
	assert.Equal(t, sessiontree.RequestHandlerAction{
		Cancel: false, RequestAction: sessiontree.RequestActionPreserve,
		Request: mo.None[sessiontree.HandlerNavigationRequest](), ResultAction: sessiontree.ResultActionClear,
		Result: mo.None[sessiontree.HandlerBranchSummaryResult](),
	}, action)
	runtime.EXPECT().Close()
	service.Close()
}
