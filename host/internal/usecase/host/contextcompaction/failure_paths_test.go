//go:build !integration

package contextcompaction

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestCompactPureHandlerCancellationNotifiesFailureWithoutCommit verifies exclusive cancellation semantics.
func TestCompactPureHandlerCancellationNotifiesFailureWithoutCommit(t *testing.T) {
	t.Parallel()
	// Arrange one canceling request handler and one failure observer.
	service, sessions, runtime, contexts, request, _ := newFailurePathHarness(t)
	handler := Handler{ExtensionID: "handler", RuntimeID: "runtime", HandlerID: "cancel"}
	observer := Handler{ExtensionID: "observer", RuntimeID: "runtime", HandlerID: "failure"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
		Requests: []Handler{handler}, Generators: nil, Results: nil, Successes: nil, Failures: []Handler{observer},
	})
	expectCompactionContext(contexts, "handler")
	expectCompactionContext(contexts, "observer")
	runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handler, gomock.Any(), gomock.Any()).Return(
		RequestAction{
			Cancel: true, RequestAction: 0, Request: mo.None[Request](),
			ResultAction: 0, Result: mo.None[Result](),
		}, nil,
	)
	runtime.EXPECT().ObserveCompactionFailure(gomock.Any(), observer, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ Handler, _ extension.Context, invocation OutcomeInvocation) error {
			require.True(t, invocation.Canceled)
			require.True(t, invocation.Committed.IsNone())
			return nil
		},
	)

	// Act through explicit handler cancellation with an active caller context.
	result, err := service.Compact(t.Context(), TriggerManual, mo.None[string](), false, request)

	// Assert cancellation is terminal, observed once, and creates no durable entry.
	require.NoError(t, err)
	require.True(t, result.Canceled)
	require.True(t, result.Committed.IsNone())
	_ = sessions
}

// TestCompactInvalidActionStopsLaterHandlersAndNotifiesFailure verifies action validation ordering.
func TestCompactInvalidActionStopsLaterHandlersAndNotifiesFailure(t *testing.T) {
	t.Parallel()
	// Arrange one malformed replacement before a later handler and failure observer.
	service, _, runtime, contexts, request, _ := newFailurePathHarness(t)
	invalid := Handler{ExtensionID: "invalid", RuntimeID: "runtime", HandlerID: "invalid"}
	later := Handler{ExtensionID: "later", RuntimeID: "runtime", HandlerID: "later"}
	observer := Handler{ExtensionID: "observer", RuntimeID: "runtime", HandlerID: "failure"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
		Requests: []Handler{invalid, later}, Generators: nil, Results: nil,
		Successes: nil, Failures: []Handler{observer},
	})
	expectCompactionContext(contexts, "invalid")
	expectCompactionContext(contexts, "observer")
	runtime.EXPECT().HandleCompactionRequest(gomock.Any(), invalid, gomock.Any(), gomock.Any()).Return(
		RequestAction{
			Cancel: false, RequestAction: RequestActionReplace, Request: mo.None[Request](),
			ResultAction: ResultActionPreserve, Result: mo.None[Result](),
		}, nil,
	)
	callerCause := errors.New("caller canceled during failure observation")
	observerCause := errors.New("failure observer failed independently")
	ctx, cancel := context.WithCancelCause(t.Context())
	runtime.EXPECT().ObserveCompactionFailure(gomock.Any(), observer, gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, Handler, extension.Context, OutcomeInvocation) error {
			cancel(callerCause)
			return observerCause
		},
	)

	// Act through the malformed public action.
	result, err := service.Compact(ctx, TriggerManual, mo.None[string](), false, request)

	// Assert no later request handler or commit runs and extension identity is retained.
	require.True(t, result.Committed.IsNone())
	var failure *FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, FailureExtensionFailed, failure.Category)
	require.ErrorContains(t, err, "invalid action")
	require.ErrorIs(t, err, observerCause)
	require.ErrorIs(t, err, callerCause)
}

// TestCompactMalformedReplacementStopsLaterHandlers verifies complete descriptor validation at composition time.
func TestCompactMalformedReplacementStopsLaterHandlers(t *testing.T) {
	t.Parallel()
	// Arrange one malformed descriptor replacement before a later handler and generator.
	service, _, runtime, contexts, request, _ := newFailurePathHarness(t)
	invalid := Handler{ExtensionID: "invalid", RuntimeID: "runtime", HandlerID: "replace"}
	later := Handler{ExtensionID: "later", RuntimeID: "runtime", HandlerID: "later"}
	generator := Handler{ExtensionID: "generator", RuntimeID: "runtime", HandlerID: "generate"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
		Requests: []Handler{invalid, later}, Generators: []Handler{generator},
		Results: nil, Successes: nil, Failures: nil,
	})
	expectCompactionContext(contexts, "invalid")
	runtime.EXPECT().HandleCompactionRequest(gomock.Any(), invalid, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ Handler, _ extension.Context, invocation RequestInvocation) (RequestAction, error) {
			replacement := invocation.Current
			replacement.Model.Input = nil
			return RequestAction{
				Cancel: false, RequestAction: RequestActionReplace, Request: mo.Some(replacement),
				ResultAction: ResultActionPreserve, Result: mo.None[Result](),
			}, nil
		},
	)

	// Act through the malformed public replacement state.
	result, err := service.Compact(t.Context(), TriggerManual, mo.None[string](), false, request)

	// Assert no later capability or commit runs and the extension category includes the exact validation cause.
	require.True(t, result.Committed.IsNone())
	require.ErrorContains(t, err, "invalid model descriptor")
	var failure *FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, FailureExtensionFailed, failure.Category)
}

// TestCompactMalformedPreviousStopsLaterCapabilities verifies complete preceding-marker validation.
func TestCompactMalformedPreviousStopsLaterCapabilities(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name          string
		mutate        func(*session.CompactionEntry)
		expectedCause string
	}{
		{
			name:          "empty summary",
			mutate:        func(marker *session.CompactionEntry) { marker.Summary = " " },
			expectedCause: "previous compaction summary is empty",
		},
		{
			name:          "missing boundary",
			mutate:        func(marker *session.CompactionEntry) { marker.FirstKeptEntryID = "" },
			expectedCause: "previous compaction boundary is empty",
		},
		{
			name:          "unknown boundary",
			mutate:        func(marker *session.CompactionEntry) { marker.FirstKeptEntryID = "missing" },
			expectedCause: "previous compaction boundary \"missing\" is not on the captured branch",
		},
		{
			name: "invalid accounting",
			mutate: func(marker *session.CompactionEntry) {
				marker.Source = session.CompactionSource{
					ExtensionID: mo.None[string](), Model: mo.None[session.BranchSummaryModelSource](),
				}
			},
			expectedCause: "branch summary requires exactly one source",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a replaced request with a malformed Previous marker before later capabilities.
			service, _, runtime, contexts, request, _ := newFailurePathHarness(t)
			invalid := Handler{ExtensionID: "invalid", RuntimeID: "runtime", HandlerID: "replace-previous"}
			later := Handler{ExtensionID: "later", RuntimeID: "runtime", HandlerID: "later"}
			generator := Handler{ExtensionID: "generator", RuntimeID: "runtime", HandlerID: "generate"}
			runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
				Requests: []Handler{invalid, later}, Generators: []Handler{generator},
				Results: nil, Successes: nil, Failures: nil,
			})
			expectCompactionContext(contexts, "invalid")
			runtime.EXPECT().HandleCompactionRequest(gomock.Any(), invalid, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ Handler, _ extension.Context, invocation RequestInvocation) (RequestAction, error) {
					replacement := invocation.Current
					marker := session.CompactionEntry{
						Summary: "previous summary", FirstKeptEntryID: "kept",
						Source: session.CompactionSource{
							ExtensionID: mo.Some("previous-extension"),
							Model:       mo.None[session.BranchSummaryModelSource](),
						},
						EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
					}
					testCase.mutate(&marker)
					replacement.Previous = mo.Some(marker)
					return RequestAction{
						Cancel: false, RequestAction: RequestActionReplace, Request: mo.Some(replacement),
						ResultAction: ResultActionPreserve, Result: mo.None[Result](),
					}, nil
				},
			)

			// Act through request replacement validation.
			result, err := service.Compact(t.Context(), TriggerManual, mo.None[string](), false, request)

			// Assert no later handler, generator, or commit observes malformed preceding state.
			require.True(t, result.Committed.IsNone())
			require.ErrorContains(t, err, testCase.expectedCause)
			var failure *FailureError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, FailureExtensionFailed, failure.Category)
		})
	}
}

// TestCompactInvalidResultBoundaryStopsBeforeCommit verifies Host authority over the captured branch.
func TestCompactInvalidResultBoundaryStopsBeforeCommit(t *testing.T) {
	t.Parallel()
	// Arrange one supplied result whose kept entry is outside the active branch.
	service, sessions, runtime, contexts, request, _ := newFailurePathHarness(t)
	handler := Handler{ExtensionID: "handler", RuntimeID: "runtime", HandlerID: "supply"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
		Requests: []Handler{handler}, Generators: nil, Results: nil, Successes: nil, Failures: nil,
	})
	expectCompactionContext(contexts, "handler")
	invalid := failurePathResult("handler")
	invalid.FirstKeptEntryID = "missing"
	runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handler, gomock.Any(), gomock.Any()).Return(
		RequestAction{
			Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[Request](),
			ResultAction: ResultActionReplace, Result: mo.Some(invalid),
		}, nil,
	)
	sessions.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(nil)

	// Act through per-handler result validation.
	result, err := service.Compact(t.Context(), TriggerManual, mo.None[string](), false, request)

	// Assert no commit runs and the invalid extension state is classified at its source boundary.
	require.True(t, result.Committed.IsNone())
	require.ErrorContains(t, err, "retained boundary is invalid")
	var failure *FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, FailureExtensionFailed, failure.Category)
}

// TestCompactHandlerTransportCancellationMatrix verifies cancellation ownership before failure typing.
func TestCompactHandlerTransportCancellationMatrix(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name             string
		cancelOwner      bool
		expectedCanceled bool
		expectedTyped    bool
	}{
		{name: "active owner", cancelOwner: false, expectedCanceled: false, expectedTyped: true},
		{name: "canceled owner", cancelOwner: true, expectedCanceled: true, expectedTyped: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange one request handler that terminates through transport cancellation.
			service, _, runtime, contexts, request, _ := newFailurePathHarness(t)
			handler := Handler{ExtensionID: "handler", RuntimeID: "runtime", HandlerID: "cancel-transport"}
			runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
				Requests: []Handler{handler}, Generators: nil, Results: nil, Successes: nil, Failures: nil,
			})
			expectCompactionContext(contexts, "handler")
			ctx, cancel := context.WithCancel(t.Context())
			if !testCase.cancelOwner {
				t.Cleanup(cancel)
			}
			runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handler, gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, Handler, extension.Context, RequestInvocation) (RequestAction, error) {
					if testCase.cancelOwner {
						cancel()
					}
					return RequestAction{}, context.Canceled
				},
			)

			// Act through the compaction orchestration owner.
			result, err := service.Compact(ctx, TriggerManual, mo.None[string](), false, request)

			// Assert only cancellation from the owning context remains an untyped canceled operation.
			require.Equal(t, testCase.expectedCanceled, result.Canceled)
			require.ErrorIs(t, err, context.Canceled)
			var failure *FailureError
			if testCase.expectedTyped {
				require.ErrorAs(t, err, &failure)
				require.Equal(t, FailureExtensionFailed, failure.Category)
			} else {
				require.NotErrorAs(t, err, &failure)
			}
		})
	}
}

// TestCompactMixedHandlerCancellationPreservesEveryCause verifies independent caller and extension failures.
func TestCompactMixedHandlerCancellationPreservesEveryCause(t *testing.T) {
	t.Parallel()
	// Arrange a handler failure acquired while the caller cancels independently.
	service, _, runtime, contexts, request, _ := newFailurePathHarness(t)
	handler := Handler{ExtensionID: "handler", RuntimeID: "runtime", HandlerID: "fail"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
		Requests: []Handler{handler}, Generators: nil, Results: nil, Successes: nil, Failures: nil,
	})
	expectCompactionContext(contexts, "handler")
	callerCause := errors.New("caller canceled compaction")
	handlerCause := errors.New("handler failed independently")
	ctx, cancel := context.WithCancelCause(t.Context())
	runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handler, gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, Handler, extension.Context, RequestInvocation) (RequestAction, error) {
			cancel(callerCause)
			return RequestAction{}, handlerCause
		},
	)

	// Act through the concurrent terminal conditions.
	_, err := service.Compact(ctx, TriggerManual, mo.None[string](), false, request)

	// Assert extension identity and both complete causes remain discoverable.
	require.ErrorIs(t, err, callerCause)
	require.ErrorIs(t, err, handlerCause)
	var failure *FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, FailureExtensionFailed, failure.Category)
}

// TestCompactSuccessfulObserverIgnoresLateOwnerCancellation verifies committed success stays error-free after point of no return.
func TestCompactSuccessfulObserverIgnoresLateOwnerCancellation(t *testing.T) {
	t.Parallel()
	// Arrange a successful commit and observer that cancels the original owner but completes successfully.
	service, sessions, runtime, contexts, request, snapshot := newFailurePathHarness(t)
	handler := Handler{ExtensionID: "handler", RuntimeID: "runtime", HandlerID: "supply"}
	observer := Handler{ExtensionID: "observer", RuntimeID: "runtime", HandlerID: "success"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
		Requests: []Handler{handler}, Generators: nil, Results: nil,
		Successes: []Handler{observer}, Failures: nil,
	})
	expectCompactionContext(contexts, "handler")
	expectCompactionContext(contexts, "observer")
	ready := failurePathResult("handler")
	runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handler, gomock.Any(), gomock.Any()).Return(
		RequestAction{
			Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[Request](),
			ResultAction: ResultActionReplace, Result: mo.Some(ready),
		}, nil,
	)
	sessions.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(request.History).AnyTimes()
	committed := compactionMarkerEntry("compaction", "kept", ready)
	sessions.EXPECT().CommitCompaction(gomock.Any(), snapshot.Identity, snapshot.ActiveLeafID, gomock.Any()).Return(
		committed, nil,
	)
	ownerCause := errors.New("owner canceled during successful observer")
	ctx, cancel := context.WithCancelCause(t.Context())
	runtime.EXPECT().ObserveCompactionSuccess(gomock.Any(), observer, gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, Handler, extension.Context, OutcomeInvocation) error {
			cancel(ownerCause)
			return nil
		},
	)

	// Act through successful detached observation after the durable commit.
	result, err := service.Compact(ctx, TriggerManual, mo.None[string](), false, request)

	// Assert late owner cancellation does not fabricate a post-commit failure or hide committed state.
	require.NoError(t, err)
	require.Equal(t, committed, result.Committed.MustGet())
	require.False(t, result.Canceled)
	require.ErrorIs(t, context.Cause(ctx), ownerCause)
}

// TestCompactObserverFailureReturnsCommittedExtensionCategory verifies commit is retained after observer failure.
func TestCompactObserverFailureReturnsCommittedExtensionCategory(t *testing.T) {
	t.Parallel()
	// Arrange a successful commit followed by one failing success observer.
	service, sessions, runtime, contexts, request, snapshot := newFailurePathHarness(t)
	handler := Handler{ExtensionID: "handler", RuntimeID: "runtime", HandlerID: "supply"}
	observer := Handler{ExtensionID: "observer", RuntimeID: "runtime", HandlerID: "success"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
		Requests: []Handler{handler}, Generators: nil, Results: nil,
		Successes: []Handler{observer}, Failures: nil,
	})
	expectCompactionContext(contexts, "handler")
	expectCompactionContext(contexts, "observer")
	ready := failurePathResult("handler")
	runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handler, gomock.Any(), gomock.Any()).Return(
		RequestAction{
			Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[Request](),
			ResultAction: ResultActionReplace, Result: mo.Some(ready),
		}, nil,
	)
	sessions.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(request.History).AnyTimes()
	committed := compactionMarkerEntry("compaction", "kept", ready)
	sessions.EXPECT().CommitCompaction(gomock.Any(), snapshot.Identity, snapshot.ActiveLeafID, gomock.Any()).Return(
		committed, nil,
	)
	observerCause := errors.New("observer failed after commit")
	runtime.EXPECT().ObserveCompactionSuccess(gomock.Any(), observer, gomock.Any(), gomock.Any()).Return(observerCause)

	// Act through commit and success observation.
	result, err := service.Compact(t.Context(), TriggerManual, mo.None[string](), false, request)

	// Assert the durable marker and complete observer category are returned together.
	require.Equal(t, committed, result.Committed.MustGet())
	require.ErrorIs(t, err, observerCause)
	var failure *FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, FailureExtensionFailed, failure.Category)
}

// TestCompactMixedCommitFailuresPreserveCategoryAndCallerCause verifies pre- and post-commit classification.
func TestCompactMixedCommitFailuresPreserveCategoryAndCallerCause(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name             string
		committed        bool
		commitCause      error
		expectedCategory FailureCategory
	}{
		{name: "persistence", committed: false, commitCause: session.ErrPersistenceUnavailable, expectedCategory: FailurePersistenceUnavailable},
		{name: "publication", committed: true, commitCause: errors.New("publication failed after commit"), expectedCategory: FailureInternal},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a ready result whose commit completes concurrently with caller cancellation.
			service, sessions, runtime, contexts, request, snapshot := newFailurePathHarness(t)
			handler := Handler{ExtensionID: "handler", RuntimeID: "runtime", HandlerID: "supply"}
			runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
				Requests: []Handler{handler}, Generators: nil, Results: nil, Successes: nil, Failures: nil,
			})
			expectCompactionContext(contexts, "handler")
			ready := failurePathResult("handler")
			runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handler, gomock.Any(), gomock.Any()).Return(
				RequestAction{
					Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[Request](),
					ResultAction: ResultActionReplace, Result: mo.Some(ready),
				}, nil,
			)
			sessions.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(request.History).AnyTimes()
			callerCause := errors.New("caller canceled during commit")
			ctx, cancel := context.WithCancelCause(t.Context())
			committed := session.Entry{}
			if testCase.committed {
				committed = compactionMarkerEntry("compaction", "kept", ready)
			}
			sessions.EXPECT().
				CommitCompaction(gomock.Any(), snapshot.Identity, snapshot.ActiveLeafID, gomock.Any()).
				DoAndReturn(
					func(context.Context, session.Identity, mo.Option[string], session.CompactionEntry) (session.Entry, error) {
						cancel(callerCause)
						return committed, testCase.commitCause
					},
				)

			// Act through the competing commit and cancellation outcomes.
			result, err := service.Compact(ctx, TriggerManual, mo.None[string](), false, request)

			// Assert durable state is accurate and every cause retains the selected category.
			require.Equal(t, testCase.committed, result.Committed.IsSome())
			require.ErrorIs(t, err, callerCause)
			require.ErrorIs(t, err, testCase.commitCause)
			var failure *FailureError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, testCase.expectedCategory, failure.Category)
		})
	}
}

// newFailurePathHarness creates one valid single-entry compaction owner with no terminal expectations.
func newFailurePathHarness(
	t *testing.T,
) (*Service, *MockSessionState, *MockRuntime, *MockContextIssuer, modelexecution.ProviderRequest, Snapshot) {
	t.Helper()
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	request := baselineRequest()
	request.Model.ContextWindow = 100_000
	request.Model.MaxTokens = 10_000
	entries := []session.Entry{compactionUserEntry("kept", mo.None[string](), "text")}
	snapshot := Snapshot{
		Identity:     session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
		ActiveLeafID: mo.Some("kept"), Entries: entries, Context: request.History,
		Previous: mo.None[session.CompactionEntry](),
	}
	sessions.EXPECT().CompactionSnapshot().Return(snapshot)
	sessions.EXPECT().ProjectSuffix(gomock.Any(), "kept").Return(request.History, nil).AnyTimes()
	service := New(sessions)
	require.NoError(t, service.BindOrchestration(runtime, contexts, 20_000))
	return service, sessions, runtime, contexts, request, snapshot
}

// failurePathResult creates one valid extension-produced result fixture.
func failurePathResult(extensionID string) Result {
	return Result{
		Summary: "summary", FirstKeptEntryID: "kept",
		Source: session.CompactionSource{
			ExtensionID: mo.Some(extensionID), Model: mo.None[session.BranchSummaryModelSource](),
		},
		Details: mo.None[[]byte](),
	}
}
