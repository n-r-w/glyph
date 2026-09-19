//go:build !integration

package runtime

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
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

// TestMapSelectionHandlerRoundTripPreservesTargetsAndReplacement verifies selection payload mapping in both directions.
func TestMapSelectionHandlerRoundTripPreservesTargetsAndReplacement(t *testing.T) {
	t.Parallel()

	// Arrange one model-selection invocation with distinct original and current targets.
	original := model.Selection{Provider: "original", Model: "one", ReasoningChoice: model.ReasoningChoiceLow}
	current := model.Selection{Provider: "current", Model: "two", ReasoningChoice: model.ReasoningChoiceMedium}
	replacement := model.Selection{Provider: "final", Model: "three", ReasoningChoice: model.ReasoningChoiceHigh}
	invocation := handlerInvocation(extensionruntime.InvocationModelSelection)
	invocation.OriginalSelection = original
	invocation.CurrentSelection = current

	// Act by encoding the invocation and decoding a complete replacement action.
	mapped, err := mapHandleRequest("selection", invocation)
	require.NoError(t, err)
	response := extensionpb.HandleResponse_builder{
		Retry: nil, ModelSelection: extensionpb.SelectionHandlerAction_builder{
			Replace: extensionpb.ModelSelection_builder{
				ProviderId: new(string(replacement.Provider)), ModelId: new(string(replacement.Model)),
				ReasoningChoice: new(string(replacement.ReasoningChoice)),
			}.Build(),
		}.Build(),
	}.Build()
	action, err := mapHandleResponse(invocation, response)

	// Assert every complete target field and the selected action survive the transport mapping.
	require.NoError(t, err)
	assert.Equal(t, "original", mapped.GetModelSelection().GetOriginal().GetProviderId())
	assert.Equal(t, "two", mapped.GetModelSelection().GetCurrent().GetModelId())
	assert.Equal(t, int32(extensionpb.SelectionHandlerAction_Replace_case), action.SelectionAction)
	assert.Equal(t, replacement, action.SelectionReplacement.MustGet())
	assert.True(t, action.SelectionRejection.IsNone())
}

// TestMapSelectionResponseDefersInvalidActionToCapability verifies malformed decisions stay nonfatal.
func TestMapSelectionResponseDefersInvalidActionToCapability(t *testing.T) {
	t.Parallel()

	// Arrange a model-selection invocation and an action for another handler kind.
	invocation := handlerInvocation(extensionruntime.InvocationModelSelection)
	response := extensionpb.HandleResponse_builder{
		Retry:       nil,
		SessionTree: extensionpb.SessionTreeAction_builder{}.Build(),
	}.Build()

	// Act by decoding the mismatched selection action.
	action, err := mapHandleResponse(invocation, response)

	// Assert transport preserves runtime availability and capability receives an invalid zero action.
	require.NoError(t, err)
	assert.Equal(t, extensionruntime.InvocationModelSelection, action.Kind)
	assert.Zero(t, action.SelectionAction)
	assert.True(t, action.SelectionReplacement.IsNone())
	assert.True(t, action.SelectionRejection.IsNone())
}

// TestMapHandleResponseReturnsOrdinaryHandlerError verifies empty normalization and exact nonempty preservation.
func TestMapHandleResponseReturnsOrdinaryHandlerError(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		message  string
		expected string
	}{
		{name: "empty", message: "", expected: "extension handler returned an empty error message"},
		{name: "nonempty", message: "handler failed", expected: "handler failed"},
		{name: "whitespace", message: " \t ", expected: " \t "},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange one typed ordinary handler failure with exact source text.
			//nolint:exhaustruct_v5 // The response builder sets only the ordinary error outcome.
			response := extensionpb.HandleResponse_builder{
				Retry: nil,
				Error: extensionpb.HandlerError_builder{Message: new(test.message)}.Build(),
			}.Build()

			// Act by mapping the typed ordinary failure.
			mapped, err := mapHandleResponse(handlerInvocation(extensionruntime.InvocationObserver), response)

			// Assert no action is returned and only exactly empty text is normalized.
			assert.Empty(t, mapped)
			require.EqualError(t, err, test.expected)
		})
	}
}

// TestMapHandleResponseRejectsAnotherActionKind verifies a registered kind cannot return another typed action.
func TestMapHandleResponseRejectsAnotherActionKind(t *testing.T) {
	t.Parallel()

	// Arrange a request-handler invocation and an observer-only response.
	//nolint:exhaustruct_v5 // The response builder sets only the observer action.
	response := extensionpb.HandleResponse_builder{
		Retry:       nil,
		SessionTree: extensionpb.SessionTreeAction_builder{}.Build(),
	}.Build()

	// Act by validating the response against the invoked kind.
	mapped, err := mapHandleResponse(handlerInvocation(extensionruntime.InvocationRequest), response)

	// Assert the protocol mismatch is rejected without an action.
	assert.Empty(t, mapped)
	require.ErrorContains(t, err, "another action kind")
}

// TestRetryHandlerMappingPreservesDecisionsAndReplacement verifies the public retry extension contract.
func TestRetryHandlerMappingPreservesDecisionsAndReplacement(t *testing.T) {
	t.Parallel()

	// Arrange one retry invocation with distinct immutable original and current decisions.
	invocation := modelexecution.RetryInvocation{
		SourceError: "complete source failure", Classification: modelexecution.ProviderFailureTransient,
		Original: modelexecution.RetryDecision{
			Retryable: true, Retry: true, Delay: time.Second, AttemptLimit: 4,
		},
		Current: modelexecution.RetryDecision{
			Retryable: true, Retry: true, Delay: 2 * time.Second, AttemptLimit: 5,
		},
		CompletedAttempts: 1, ProviderDelay: mo.Some(1500 * time.Millisecond),
	}
	request := extensionruntime.HandlerInvocation{
		Context: extension.Context{}, Kind: extensionruntime.InvocationRetry,
		Original: extensionruntime.Preparation{}, Current: extensionruntime.Preparation{},
		OriginalResult: mo.None[extensionruntime.Summary](), CurrentResult: mo.None[extensionruntime.Summary](),
		Commit: mo.None[extensionruntime.TreeCommit](), OriginalSelection: model.Selection{},
		CurrentSelection: model.Selection{}, Retry: mo.Some(invocation),
	}

	// Act across both public request and response mappings.
	mappedRequest, err := mapHandleRequest("retry", request)
	require.NoError(t, err)
	replacement := extensionpb.RetryDecision_builder{
		Retryable: new(true), Retry: new(true), DelayMilliseconds: new(int64(3000)), AttemptLimit: new(int64(6)),
	}.Build()
	action := new(extensionpb.RetryHandlerAction)
	action.SetReplace(replacement)
	response := new(extensionpb.HandleResponse)
	response.SetRetry(action)
	mappedAction, err := mapHandleResponse(request, response)

	// Assert immutable decisions and the complete replacement survive transport projection.
	require.NoError(t, err)
	assert.Equal(t, int64(1000), mappedRequest.GetRetry().GetOriginal().GetDelayMilliseconds())
	assert.Equal(t, int64(2000), mappedRequest.GetRetry().GetCurrent().GetDelayMilliseconds())
	assert.Equal(t, int64(1500), mappedRequest.GetRetry().GetProviderDelayMilliseconds())
	assert.Equal(t, mo.Some(modelexecution.RetryAction{
		Kind: modelexecution.RetryActionReplace,
		Decision: mo.Some(modelexecution.RetryDecision{
			Retryable: true, Retry: true, Delay: 3 * time.Second, AttemptLimit: 6,
		}),
	}), mappedAction.Retry)
}

// handlerInvocation constructs an empty typed process payload for transport variant tests.
func handlerInvocation(kind extensionruntime.InvocationKind) extensionruntime.HandlerInvocation {
	return extensionruntime.HandlerInvocation{
		Context:           extension.Context{},
		Kind:              kind,
		Original:          extensionruntime.Preparation{},
		Current:           extensionruntime.Preparation{},
		OriginalResult:    mo.None[extensionruntime.Summary](),
		CurrentResult:     mo.None[extensionruntime.Summary](),
		Commit:            mo.None[extensionruntime.TreeCommit](),
		OriginalSelection: model.Selection{},
		CurrentSelection:  model.Selection{},
		Retry:             mo.None[modelexecution.RetryInvocation](),
	}
}
