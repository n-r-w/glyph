//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
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
	request := extensionruntime.Navigation{
		TargetEntryID: "target",
		SummaryMode:   2,
		CustomFocus:   mo.None[string](),
		SummaryModel:  selection,
	}
	entry := extensionruntime.TreeEntry{
		ID:               "extension-entry",
		User:             mo.None[session.UserMessage](),
		Model:            mo.None[[]extensionruntime.Content](),
		ToolResult:       mo.None[session.ToolResult](),
		BranchSummary:    mo.None[string](),
		Extension:        mo.Some(extensionruntime.ExtensionIdentity{ExtensionID: "owner", EntryType: "private"}),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
	}
	state := extensionruntime.Preparation{
		SessionID:             "session",
		PrecedingActiveLeafID: mo.Some("leaf"),
		Request:               request,
		DestinationID:         mo.Some("destination"),
		CommonAncestorID:      mo.Some("common"),
		Entries:               []extensionruntime.TreeEntry{entry},
	}
	invocation := handlerInvocation(extensionruntime.InvocationRequest)
	invocation.Context = extension.Context{
		ID:                "binding",
		ExtensionID:       "extension",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/project",
	}
	invocation.Original = state
	invocation.Current = state
	invocation.CurrentResult = mo.Some(
		extensionruntime.Summary{
			Summary: "ready",
			Source: session.BranchSummarySource{
				ExtensionID: mo.Some("producer"),
				Model:       mo.None[session.BranchSummaryModelSource](),
			},
		},
	)
	// Act by encoding the runtime-filtered invocation.
	mapped, err := mapHandleRequest("handler", invocation)

	// Assert identity, configured model, Host preparation, handler order payload, and opaque extension projection.
	require.NoError(t, err)
	assert.Equal(t, "handler", mapped.GetHandlerId())
	assert.Equal(t, "binding", mapped.GetContext().GetContextId())
	assert.Equal(t, "extension", mapped.GetContext().GetExtensionId())
	assert.Equal(t, "runtime", mapped.GetContext().GetRuntimeInstanceId())
	assert.Equal(t, "session", mapped.GetContext().GetSessionId())
	assert.Equal(t, "/project", mapped.GetContext().GetCwd())
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
	//nolint:exhaustruct_v5 // The response builder sets only the ordinary error outcome.
	response := extensionpb.HandleResponse_builder{
		Error: extensionpb.HandlerError_builder{Message: new("handler failed")}.Build(),
	}.Build()

	// Act by mapping the typed ordinary failure.
	mapped, err := mapHandleResponse(handlerInvocation(extensionruntime.InvocationObserver), response)

	// Assert no action is returned and the safe handler failure is preserved.
	assert.Empty(t, mapped)
	require.EqualError(t, err, "handler failed")
}

// TestMapHandleResponseRejectsAnotherActionKind verifies a registered kind cannot return another typed action.
func TestMapHandleResponseRejectsAnotherActionKind(t *testing.T) {
	t.Parallel()

	// Arrange a request-handler invocation and an observer-only response.
	//nolint:exhaustruct_v5 // The response builder sets only the observer action.
	response := extensionpb.HandleResponse_builder{
		SessionTree: extensionpb.SessionTreeAction_builder{}.Build(),
	}.Build()

	// Act by validating the response against the invoked kind.
	mapped, err := mapHandleResponse(handlerInvocation(extensionruntime.InvocationRequest), response)

	// Assert the protocol mismatch is rejected without an action.
	assert.Empty(t, mapped)
	require.ErrorContains(t, err, "another action kind")
}

// handlerInvocation constructs an empty typed process payload for transport variant tests.
func handlerInvocation(kind extensionruntime.InvocationKind) extensionruntime.HandlerInvocation {
	return extensionruntime.HandlerInvocation{
		Context:        extension.Context{},
		Kind:           kind,
		Original:       extensionruntime.Preparation{},
		Current:        extensionruntime.Preparation{},
		OriginalResult: mo.None[extensionruntime.Summary](),
		CurrentResult:  mo.None[extensionruntime.Summary](),
		Commit:         mo.None[extensionruntime.TreeCommit](),
	}
}
