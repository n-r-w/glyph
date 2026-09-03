//go:build !integration

package runtime

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestMapHandleRequestPreservesTypedNavigationContext verifies request and Host context mapping without extension data
// exposure.
func TestMapHandleRequestPreservesTypedNavigationContext(t *testing.T) {
	t.Parallel()

	// Arrange immutable and current navigation state with one opaque abandoned entry and current summary.
	selection := model.Selection{
		Provider: model.ProviderID("provider"), Model: model.ID("model"),
		ReasoningChoice: model.ReasoningChoice("medium"),
	}
	request := sessiontree.HandlerNavigationRequest{
		Navigation: sessionnavigation.Request{
			TargetEntryID: "target", SummaryMode: sessionnavigation.SummaryModeSummarize,
			CustomFocus: mo.None[string](),
		},
		SummaryModel: selection,
	}
	entry := session.Entry{
		ID: "extension-entry", ParentID: mo.None[string](), CreatedAt: time.Unix(1, 0),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](),
		Extension: mo.Some(session.ExtensionEnvelope{
			ExtensionID: "owner", EntryType: "private", Data: []byte("secret"),
		}),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	state := sessiontree.HandlerNavigationState{
		SessionID: "session", PrecedingActiveLeafID: mo.Some("leaf"), Request: request,
		Preparation: session.NavigationPreparation{
			DestinationID: mo.Some("destination"), NextInput: mo.None[string](),
			CommonAncestorID: mo.Some("common"), AbandonedPath: []session.Entry{entry},
		},
	}
	invocation := sessiontree.RequestHandlerInvocation{
		Original: state, Current: state,
		CurrentResult: mo.Some(sessiontree.HandlerBranchSummaryResult{
			Summary: "ready", Usage: mo.None[session.TokenUsage](),
		}),
	}

	// Act by mapping the typed Host invocation to protobuf.
	mapped, err := mapHandleRequest("handler", sessiontree.HandlerRequest{
		Request:  mo.Some(invocation),
		Result:   mo.None[sessiontree.ResultHandlerInvocation](),
		Observer: mo.None[sessiontree.TreeObserverInvocation](),
	})

	// Assert identity, configured model, Host preparation, handler order payload, and opaque extension projection.
	require.NoError(t, err)
	assert.Equal(t, "handler", mapped.GetHandlerId())
	payload := mapped.GetSessionBeforeTreeRequest()
	require.NotNil(t, payload)
	assert.Equal(t, "target", payload.GetOriginalRequest().GetTargetEntryId())
	assert.Equal(t, "provider", payload.GetCurrentRequest().GetSummaryModel().GetProviderId())
	assert.Equal(t, "leaf", payload.GetCurrentPreparation().GetPrecedingActiveLeafId())
	require.Len(t, payload.GetCurrentPreparation().GetAbandonedEntries(), 1)
	projection := payload.GetCurrentPreparation().GetAbandonedEntries()[0].GetExtension()
	require.NotNil(t, projection)
	assert.Equal(t, "owner", projection.GetExtensionId())
	assert.Equal(t, "private", projection.GetEntryType())
	assert.Equal(t, "ready", payload.GetCurrentResult().GetSummary())
}

// TestMapHandleResponseReturnsOrdinaryHandlerError verifies a typed handler failure does not become a protocol failure.
func TestMapHandleResponseReturnsOrdinaryHandlerError(t *testing.T) {
	t.Parallel()

	// Arrange an observer invocation and one typed ordinary handler failure.
	request := sessiontree.TreeObserverInvocation{
		SessionID: "session", TargetEntryID: "target", PrecedingActiveLeafID: mo.None[string](),
		NavigationDestinationID: mo.None[string](), CommittedActiveLeafID: mo.None[string](),
		CreatedSummary: mo.None[session.Entry](),
	}
	//nolint:exhaustruct_v5 // The response builder sets only the ordinary error outcome.
	response := extensionpb.HandleResponse_builder{
		Error: extensionpb.HandlerError_builder{Message: new("handler failed")}.Build(),
	}.Build()

	// Act by mapping the typed ordinary failure.
	mapped, err := mapHandleResponse(sessiontree.HandlerRequest{
		Request:  mo.None[sessiontree.RequestHandlerInvocation](),
		Result:   mo.None[sessiontree.ResultHandlerInvocation](),
		Observer: mo.Some(request),
	}, response)

	// Assert no action is returned and the safe handler failure is preserved.
	assert.Empty(t, mapped)
	require.EqualError(t, err, "handler failed")
}

// TestMapHandleResponseRejectsAnotherActionKind verifies a registered kind cannot return another typed action.
func TestMapHandleResponseRejectsAnotherActionKind(t *testing.T) {
	t.Parallel()

	// Arrange a request-handler invocation and an observer-only response.
	request := sessiontree.RequestHandlerInvocation{
		Original: sessiontree.HandlerNavigationState{
			SessionID: "", PrecedingActiveLeafID: mo.None[string](),
			Request: sessiontree.HandlerNavigationRequest{
				Navigation: sessionnavigation.Request{
					TargetEntryID: "", SummaryMode: 0, CustomFocus: mo.None[string](),
				},
				SummaryModel: model.Selection{
					Provider: "", Model: "", ReasoningChoice: "",
				},
			},
			Preparation: session.NavigationPreparation{
				DestinationID: mo.None[string](), NextInput: mo.None[string](),
				CommonAncestorID: mo.None[string](), AbandonedPath: nil,
			},
		},
		Current: sessiontree.HandlerNavigationState{
			SessionID: "", PrecedingActiveLeafID: mo.None[string](),
			Request: sessiontree.HandlerNavigationRequest{
				Navigation: sessionnavigation.Request{
					TargetEntryID: "", SummaryMode: 0, CustomFocus: mo.None[string](),
				},
				SummaryModel: model.Selection{
					Provider: "", Model: "", ReasoningChoice: "",
				},
			},
			Preparation: session.NavigationPreparation{
				DestinationID: mo.None[string](), NextInput: mo.None[string](),
				CommonAncestorID: mo.None[string](), AbandonedPath: nil,
			},
		},
		CurrentResult: mo.None[sessiontree.HandlerBranchSummaryResult](),
	}
	//nolint:exhaustruct_v5 // The response builder sets only the observer action.
	response := extensionpb.HandleResponse_builder{
		SessionTree: extensionpb.SessionTreeAction_builder{}.Build(),
	}.Build()

	// Act by validating the response against the invoked kind.
	mapped, err := mapHandleResponse(sessiontree.HandlerRequest{
		Request:  mo.Some(request),
		Result:   mo.None[sessiontree.ResultHandlerInvocation](),
		Observer: mo.None[sessiontree.TreeObserverInvocation](),
	}, response)

	// Assert the protocol mismatch is rejected without an action.
	assert.Empty(t, mapped)
	require.ErrorContains(t, err, "another action kind")
}
