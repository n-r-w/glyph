//go:build !integration

package sessiontree

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// TestNavigateComposesRequestHandlersAndPostCommitObservers verifies target preparation, issue order,
// and observer timing.
func TestNavigateComposesRequestHandlersAndPostCommitObservers(t *testing.T) {
	t.Parallel()

	// Arrange two request handlers that replace the target and then fail, plus one failing observer.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockRuntime(controller)
	service := New(active, models, handlers)
	tree := navigationTree(t, time.Unix(1, 0).UTC())
	originalPreparation, err := tree.NavigationPreparation("user")
	require.NoError(t, err)
	originalPreparation = projectPreparation(originalPreparation)
	currentPreparation, err := tree.NavigationPreparation("extension")
	require.NoError(t, err)
	currentPreparation = projectPreparation(currentPreparation)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	original := HandlerNavigationState{
		SessionID: "session", PrecedingActiveLeafID: mo.Some("active"),
		Request: HandlerNavigationRequest{
			Navigation: sessionnavigation.Request{
				TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeNoSummary,
				CustomFocus: mo.None[string](),
			},
			SummaryModel: selection,
		},
		Preparation: originalPreparation,
	}
	currentRequest := HandlerNavigationRequest{
		Navigation: sessionnavigation.Request{
			TargetEntryID: "extension", SummaryMode: sessionnavigation.SummaryModeNoSummary,
			CustomFocus: mo.None[string](),
		},
		SummaryModel: selection,
	}
	first := Handler{ExtensionID: "first-extension", HandlerID: "replace-target"}
	second := Handler{ExtensionID: "second-extension", HandlerID: "ordinary-error"}
	observer := Handler{ExtensionID: "observer-extension", HandlerID: "after-commit"}
	active.EXPECT().Tree().Return(tree)
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().ActiveSelection().Return(selection)
	registerTestHandlers(service, handlers, HandlerKindRequest, []Handler{first, second})
	expectRequestHandler(handlers, first, RequestHandlerInvocation{
		Original: original, Current: original, CurrentResult: mo.None[HandlerBranchSummaryResult](),
	}, RequestHandlerAction{
		Cancel: false, RequestAction: RequestActionReplace, Request: mo.Some(currentRequest),
		ResultAction: ResultActionPreserve, Result: mo.None[HandlerBranchSummaryResult](),
	}, nil)
	current := HandlerNavigationState{
		SessionID: "session", PrecedingActiveLeafID: mo.Some("active"), Request: currentRequest,
		Preparation: currentPreparation,
	}
	expectRequestHandler(handlers, second, RequestHandlerInvocation{
		Original: original, Current: current, CurrentResult: mo.None[HandlerBranchSummaryResult](),
	}, RequestHandlerAction{}, errors.New("load summary rules: open rules.json: permission denied"))
	committed := tree.Clone()
	require.NoError(t, committed.SetActiveLeaf(mo.Some("extension")))
	registerTestHandlers(service, handlers, HandlerKindObserver, []Handler{observer})
	gomock.InOrder(
		active.EXPECT().CommitNavigation(gomock.Any(), CommitCommand{
			ExpectedActiveLeafID: mo.Some("active"), DestinationID: mo.Some("extension"),
			BranchSummary: mo.None[BranchSummaryDraft](),
		}).Return(committed, nil),
		expectObserver(handlers, observer, TreeObserverInvocation{
			SessionID: "session", TargetEntryID: "extension", PrecedingActiveLeafID: mo.Some("active"),
			NavigationDestinationID: mo.Some("extension"), CommittedActiveLeafID: mo.Some("extension"),
			CreatedSummary: mo.None[session.Entry](),
		}, errors.New("save navigation receipt: write receipt.json: disk full")),
	)

	// Act by navigating through the complete request and observer chain.
	result, err := service.NavigateTree(t.Context(), original.Request.Navigation)

	// Assert the replacement target commits and complete causes preserve occurrence order.
	require.NoError(t, err)
	assert.False(t, result.Canceled)
	assert.Equal(t, mo.Some("extension"), result.ActiveLeafID)
	assert.Equal(t, []sessionnavigation.OperationIssue{
		{
			Code: sessionnavigation.OperationIssueHandlerError, ExtensionID: "second-extension",
			HandlerID: "ordinary-error", Message: "load summary rules: open rules.json: permission denied",
		},
		{
			Code: sessionnavigation.OperationIssueObserverError, ExtensionID: "observer-extension",
			HandlerID: "after-commit", Message: "save navigation receipt: write receipt.json: disk full",
		},
	}, result.Issues)
}

// TestNavigatePreservesStateForInvalidHandlerAction verifies invalid action shape cannot change current state.
func TestNavigatePreservesStateForInvalidHandlerAction(t *testing.T) {
	t.Parallel()

	// Arrange one handler that combines preserve with a forbidden replacement payload.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockRuntime(controller)
	service := New(active, models, handlers)
	tree := navigationTree(t, time.Unix(1, 0).UTC())
	preparation, err := tree.NavigationPreparation("user")
	require.NoError(t, err)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	state := handlerState("session", selection, sessionnavigation.Request{
		TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeNoSummary, CustomFocus: mo.None[string](),
	}, preparation)
	handler := Handler{ExtensionID: "extension", HandlerID: "invalid"}
	active.EXPECT().Tree().Return(tree)
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().ActiveSelection().Return(selection)
	registerTestHandlers(service, handlers, HandlerKindRequest, []Handler{handler})
	expectRequestHandler(handlers, handler, RequestHandlerInvocation{
		Original: state, Current: state, CurrentResult: mo.None[HandlerBranchSummaryResult](),
	}, RequestHandlerAction{
		Cancel: false, RequestAction: RequestActionPreserve,
		Request: mo.Some(HandlerNavigationRequest{
			Navigation: sessionnavigation.Request{
				TargetEntryID: "extension", SummaryMode: sessionnavigation.SummaryModeNoSummary,
				CustomFocus: mo.None[string](),
			},
			SummaryModel: selection,
		}),
		ResultAction: ResultActionPreserve, Result: mo.None[HandlerBranchSummaryResult](),
	}, nil)
	committed := tree.Clone()
	require.NoError(t, committed.SetActiveLeaf(mo.Some("root")))
	active.EXPECT().CommitNavigation(gomock.Any(), CommitCommand{
		ExpectedActiveLeafID: mo.Some("active"), DestinationID: mo.Some("root"),
		BranchSummary: mo.None[BranchSummaryDraft](),
	}).Return(committed, nil)

	// Act with the invalid action.
	result, err := service.NavigateTree(t.Context(), state.Request.Navigation)

	// Assert original state commits and one safe invalid-action issue is returned.
	require.NoError(t, err)
	assert.Equal(t, mo.Some("root"), result.ActiveLeafID)
	assert.Equal(t, []sessionnavigation.OperationIssue{{
		Code: sessionnavigation.OperationIssueInvalidHandlerAction, ExtensionID: "extension",
		HandlerID: "invalid", Message: "extension handler returned an invalid action",
	}}, result.Issues)
}

// TestNavigateCancellationReturnsAccumulatedIssuesWithoutCommit verifies cancellation stops all later work.
func TestNavigateCancellationReturnsAccumulatedIssuesWithoutCommit(t *testing.T) {
	t.Parallel()

	// Arrange one ordinary failure followed by one cancel action.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockRuntime(controller)
	service := New(active, models, handlers)
	tree := navigationTree(t, time.Unix(1, 0).UTC())
	preparation, err := tree.NavigationPreparation("user")
	require.NoError(t, err)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	state := handlerState("session", selection, sessionnavigation.Request{
		TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeNoSummary, CustomFocus: mo.None[string](),
	}, preparation)
	failed := Handler{ExtensionID: "extension", HandlerID: "failed"}
	canceling := Handler{ExtensionID: "extension", HandlerID: "cancel"}
	active.EXPECT().Tree().Return(tree)
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().ActiveSelection().Return(selection)
	registerTestHandlers(service, handlers, HandlerKindRequest, []Handler{failed, canceling})
	expectRequestHandler(handlers, failed, gomock.Any(), RequestHandlerAction{}, errors.New("ordinary failure"))
	expectRequestHandler(handlers, canceling, RequestHandlerInvocation{
		Original: state, Current: state, CurrentResult: mo.None[HandlerBranchSummaryResult](),
	}, RequestHandlerAction{
		Cancel: true, RequestAction: RequestAction(0), Request: mo.None[HandlerNavigationRequest](),
		ResultAction: ResultAction(0), Result: mo.None[HandlerBranchSummaryResult](),
	}, nil)

	// Act through the canceling request chain.
	result, err := service.NavigateTree(t.Context(), state.Request.Navigation)

	// Assert cancellation is a state-free result with only preceding issues.
	require.NoError(t, err)
	assert.True(t, result.Canceled)
	assert.Equal(t, []sessionnavigation.OperationIssue{{
		Code: sessionnavigation.OperationIssueHandlerError, ExtensionID: "extension",
		HandlerID: "failed", Message: "ordinary failure",
	}}, result.Issues)
}

// TestNavigateClearedReadyResultRunsBuiltInAndResultHandlers verifies fallback and result replacement happen once.
func TestNavigateClearedReadyResultRunsBuiltInAndResultHandlers(t *testing.T) {
	t.Parallel()

	// Arrange request handlers that set and clear a result, followed by one result replacement.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockRuntime(controller)
	service := New(active, models, handlers)
	tree := navigationTree(t, time.Unix(1, 0).UTC())
	preparation, err := tree.NavigationPreparation("user")
	require.NoError(t, err)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	request := sessionnavigation.Request{
		TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeSummarize, CustomFocus: mo.None[string](),
	}
	state := handlerState("session", selection, request, preparation)
	setHandler := Handler{ExtensionID: "first", HandlerID: "set"}
	clearHandler := Handler{ExtensionID: "second", HandlerID: "clear"}
	failedResultHandler := Handler{ExtensionID: "third", HandlerID: "failed-result"}
	resultHandler := Handler{ExtensionID: "fourth", HandlerID: "replace"}
	source := session.BranchSummarySource{
		ExtensionID: mo.None[string](),
		Model: mo.Some(session.BranchSummaryModelSource{
			Selection: selection, Usage: mo.None[session.TokenUsage](),
		}),
	}
	ready := HandlerBranchSummaryResult{Summary: "ready", Source: source}
	generated := HandlerBranchSummaryResult{Summary: "generated", Source: source}
	final := HandlerBranchSummaryResult{Summary: "refined", Source: source}
	active.EXPECT().Tree().Return(tree)
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().ActiveSelection().Return(selection)
	registerTestHandlers(service, handlers, HandlerKindRequest, []Handler{setHandler, clearHandler})
	expectRequestHandler(handlers, setHandler, gomock.Any(), RequestHandlerAction{
		Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[HandlerNavigationRequest](),
		ResultAction: ResultActionReplace, Result: mo.Some(ready),
	}, nil)
	expectRequestHandler(handlers, clearHandler, gomock.Any(), RequestHandlerAction{
		Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[HandlerNavigationRequest](),
		ResultAction: ResultActionClear, Result: mo.None[HandlerBranchSummaryResult](),
	}, nil)
	models.EXPECT().Request(gomock.Any(), selection, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ model.Selection, _ string, _ []agent.HistoryEntry) (model.Response, error) {
			return summaryResponse(generated.Summary, mo.None[model.Usage]()), nil
		},
	)
	registerTestHandlers(service, handlers, HandlerKindResult, []Handler{failedResultHandler, resultHandler})
	expectResultHandler(handlers, failedResultHandler, ResultHandlerInvocation{
		Original: state, Current: state, OriginalResult: generated, CurrentResult: generated,
	}, ResultHandlerAction{}, errors.New("refine summary: load glossary: invalid JSON"))
	expectResultHandler(handlers, resultHandler, ResultHandlerInvocation{
		Original: state, Current: state, OriginalResult: generated, CurrentResult: generated,
	}, ResultHandlerAction{
		Cancel: false, ResultAction: ResultActionReplace, Result: mo.Some(final),
	}, nil)
	active.EXPECT().CommitNavigation(gomock.Any(), CommitCommand{
		ExpectedActiveLeafID: mo.Some("active"), DestinationID: mo.Some("root"),
		BranchSummary: mo.Some(BranchSummaryDraft{
			Summary: "refined", FirstEntryID: "user", LastEntryID: "active", CommonAncestorID: mo.Some("root"),
			Source: source,
		}),
	}).Return(tree, nil)

	// Act after the ready result is cleared.
	result, err := service.NavigateTree(t.Context(), request)

	// Assert failure preserves the generated result for the later handler and reports its complete cause.
	require.NoError(t, err)
	assert.False(t, result.Canceled)
	assert.Equal(t, []sessionnavigation.OperationIssue{{
		Code: sessionnavigation.OperationIssueHandlerError, ExtensionID: "third", HandlerID: "failed-result",
		Message: "refine summary: load glossary: invalid JSON",
	}}, result.Issues)
}

// TestNavigateResultHandlerCancellationStopsBeforeValidation verifies result-chain cancellation writes nothing.
func TestNavigateResultHandlerCancellationStopsBeforeValidation(t *testing.T) {
	t.Parallel()

	// Arrange a request handler result followed by one canceling result handler.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockRuntime(controller)
	service := New(active, models, handlers)
	tree := navigationTree(t, time.Unix(1, 0).UTC())
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	requestHandler := Handler{ExtensionID: "first", HandlerID: "supply"}
	resultHandler := Handler{ExtensionID: "second", HandlerID: "cancel"}
	active.EXPECT().Tree().Return(tree)
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().ActiveSelection().Return(selection)
	registerTestHandlers(service, handlers, HandlerKindRequest, []Handler{requestHandler})
	expectRequestHandler(handlers, requestHandler, gomock.Any(), RequestHandlerAction{
		Cancel: false, RequestAction: RequestActionPreserve,
		Request: mo.None[HandlerNavigationRequest](), ResultAction: ResultActionReplace,
		Result: mo.Some(HandlerBranchSummaryResult{Summary: "ready", Source: session.BranchSummarySource{
			ExtensionID: mo.Some("first"), Model: mo.None[session.BranchSummaryModelSource](),
		}}),
	}, nil)
	registerTestHandlers(service, handlers, HandlerKindResult, []Handler{resultHandler})
	expectResultHandler(handlers, resultHandler, gomock.Any(), ResultHandlerAction{
		Cancel: true, ResultAction: ResultAction(0), Result: mo.None[HandlerBranchSummaryResult](),
	}, nil)

	// Act through the canceling result chain.
	result, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
		TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeSummarize,
		CustomFocus: mo.None[string](),
	})

	// Assert cancellation returns no committed state and performs no final validation or observer work.
	require.NoError(t, err)
	assert.True(t, result.Canceled)
	assert.Empty(t, result.Issues)
}

// TestNavigateObserversIgnorePostCommitCancellation verifies commit completion owns observer execution.
func TestNavigateObserversIgnorePostCommitCancellation(t *testing.T) {
	t.Parallel()

	// Arrange a commit that cancels the caller before one observer invocation.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockRuntime(controller)
	service := New(active, models, handlers)
	tree := navigationTree(t, time.Unix(1, 0).UTC())
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	observer := Handler{ExtensionID: "extension", HandlerID: "observer"}
	ctx, cancel := context.WithCancel(t.Context())
	active.EXPECT().Tree().Return(tree)
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().ActiveSelection().Return(selection)
	committed := tree.Clone()
	require.NoError(t, committed.SetActiveLeaf(mo.Some("root")))
	active.EXPECT().CommitNavigation(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, CommitCommand) (session.Tree, error) {
			cancel()
			return committed, nil
		},
	)
	registerTestHandlers(service, handlers, HandlerKindObserver, []Handler{observer})
	handlers.EXPECT().HandleHandler(
		gomock.Any(), observer.ExtensionID, observer.HandlerID, gomock.Any(),
	).DoAndReturn(func(
		observerContext context.Context,
		_, _ string,
		_ HandlerRequest,
	) (HandlerResponse, error) {
		assert.NoError(t, observerContext.Err())
		return HandlerResponse{
			Request: mo.None[RequestHandlerAction](), Result: mo.None[ResultHandlerAction](),
			Observer: mo.Some(ObserverAction{}),
		}, nil
	})

	// Act with cancellation occurring only after the commit returns.
	_, err := service.NavigateTree(ctx, sessionnavigation.Request{
		TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeNoSummary,
		CustomFocus: mo.None[string](),
	})

	// Assert the committed outcome and observer call complete despite caller cancellation.
	require.NoError(t, err)
}

// TestNavigateRejectsInconsistentHandlerResultWithoutCommit verifies final validation precedes persistence.
func TestNavigateRejectsInconsistentHandlerResultWithoutCommit(t *testing.T) {
	t.Parallel()

	// Arrange one valid action that supplies a summary for no-summary navigation.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockRuntime(controller)
	service := New(active, models, handlers)
	tree := navigationTree(t, time.Unix(1, 0).UTC())
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	handler := Handler{ExtensionID: "extension", HandlerID: "inconsistent"}
	active.EXPECT().Tree().Return(tree)
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().ActiveSelection().Return(selection)
	registerTestHandlers(service, handlers, HandlerKindRequest, []Handler{handler})
	expectRequestHandler(handlers, handler, gomock.Any(), RequestHandlerAction{
		Cancel: false, RequestAction: RequestActionPreserve,
		Request: mo.None[HandlerNavigationRequest](), ResultAction: ResultActionReplace,
		Result: mo.Some(HandlerBranchSummaryResult{Summary: "unexpected", Source: session.BranchSummarySource{
			ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
		}}),
	}, nil)

	// Act with no-summary mode and an extension-provided result.
	_, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
		TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeNoSummary,
		CustomFocus: mo.None[string](),
	})

	// Assert final validation rejects the state without commit or observer calls.
	require.ErrorIs(t, err, sessionnavigation.ErrExtensionInvalidResult)
}

// TestNavigateEmptyAbandonedPathSkipsModelExecution verifies summarization mode does no model work for an empty path.
func TestNavigateEmptyAbandonedPathSkipsModelExecution(t *testing.T) {
	t.Parallel()

	// Arrange an active non-user entry selected as its own navigation destination.
	controller := gomock.NewController(t)
	active := NewMockActiveSession(controller)
	models := NewMockModelRequester(controller)
	handlers := NewMockRuntime(controller)
	service := New(active, models, handlers)
	entry := session.Entry{
		ID:            "extension",
		ParentID:      mo.None[string](),
		CreatedAt:     time.Unix(1, 0).UTC(),
		Information:   mo.None[session.Information](),
		User:          mo.None[model.Message](),
		Model:         mo.None[model.Response](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult:    mo.None[agent.ToolResult](),
		Extension: mo.Some(
			session.ExtensionEnvelope{ExtensionID: "extension", EntryType: "state", Data: []byte(`{}`)},
		),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	tree, err := session.NewTree([]session.Entry{entry}, mo.Some("extension"), nil)
	require.NoError(t, err)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	active.EXPECT().Tree().Return(tree)
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().ActiveSelection().Return(selection)
	active.EXPECT().CommitNavigation(gomock.Any(), CommitCommand{
		ExpectedActiveLeafID: mo.Some("extension"), DestinationID: mo.Some("extension"),
		BranchSummary: mo.None[BranchSummaryDraft](),
	}).Return(tree, nil)

	// Act with summarization enabled and no abandoned entries.
	result, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
		TargetEntryID: "extension", SummaryMode: sessionnavigation.SummaryModeSummarize,
		CustomFocus: mo.None[string](),
	})

	// Assert navigation commits without an availability check, model request, or summary creation.
	require.NoError(t, err)
	assert.Equal(t, mo.Some("extension"), result.ActiveLeafID)
}

// handlerState creates one complete extension-visible navigation state for tests.
func handlerState(
	sessionID string,
	selection model.Selection,
	request sessionnavigation.Request,
	preparation session.NavigationPreparation,
) HandlerNavigationState {
	return HandlerNavigationState{
		SessionID: sessionID, PrecedingActiveLeafID: mo.Some("active"),
		Request:     HandlerNavigationRequest{Navigation: request, SummaryModel: selection},
		Preparation: projectPreparation(preparation),
	}
}
