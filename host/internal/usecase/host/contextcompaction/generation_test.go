//go:build !integration

package contextcompaction

import (
	"context"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestPrepareContextRejectsPostCommitGrowthAboveCapturedBudget verifies actual outbound history is re-estimated.
func TestPrepareContextRejectsPostCommitGrowthAboveCapturedBudget(t *testing.T) {
	t.Parallel()
	// Arrange threshold compaction that commits before a success observer adds model-visible context.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	request := baselineRequest()
	request.Model.ContextWindow = 10_000
	request.Model.MaxTokens = 1_000
	request.History = []agent.HistoryEntry{textHistory(strings.Repeat("oversized ", 5_000))}
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	entries := []session.Entry{
		compactionUserEntry("u1", mo.None[string](), "old"),
		compactionUserEntry("u2", mo.Some("u1"), "kept"),
	}
	before := Snapshot{
		Identity: identity, ActiveLeafID: mo.Some("u2"), Entries: entries,
		Context: request.History, Previous: mo.None[session.CompactionEntry](),
	}
	after := cloneSnapshot(before)
	after.Context = []agent.HistoryEntry{textHistory(strings.Repeat("observer growth ", 5_000))}
	sessions.EXPECT().CompactionSnapshot().Return(before)
	sessions.EXPECT().CompactionSnapshot().Return(after)
	sessions.EXPECT().ContextSession().Return(identity)
	sessions.EXPECT().ProjectSuffix(gomock.Any(), gomock.Any()).Return(request.History, nil).AnyTimes()
	handler := Handler{ExtensionID: "extension", RuntimeID: "runtime", HandlerID: "supply"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
		Requests: []Handler{handler}, Generators: nil, Results: nil, Successes: nil, Failures: nil,
	})
	expectCompactionContext(contexts, "extension")
	ready := Result{
		Summary: "summary", FirstKeptEntryID: "u2",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		Details: mo.None[[]byte](),
	}
	runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handler, gomock.Any(), gomock.Any()).Return(
		RequestAction{
			Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[Request](),
			ResultAction: ResultActionReplace, Result: mo.Some(ready),
		}, nil,
	)
	sessions.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(
		[]agent.HistoryEntry{textHistory("small compacted request")},
	).AnyTimes()
	committed := compactionMarkerEntry("compaction", "u2", ready)
	sessions.EXPECT().CommitCompaction(gomock.Any(), identity, mo.Some("u2"), gomock.Any()).Return(committed, nil)
	service := New(sessions)
	require.NoError(t, service.BindOrchestration(runtime, contexts, 20_000))

	// Act through the production threshold preparation entry point.
	_, err := service.PrepareContext(t.Context(), request)

	// Assert the durable marker remains committed but the known oversized projection is not returned for dispatch.
	require.ErrorContains(t, err, "after compaction")
	require.ErrorContains(t, err, "exceeds input budget")
}

// TestCompactResultClearingInvokesGenerator verifies clear composition removes a supplied ready result.
func TestCompactResultClearingInvokesGenerator(t *testing.T) {
	t.Parallel()
	// Arrange one supplied result followed by an explicit clear and one registered generator.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	request := baselineRequest()
	request.Model.ContextWindow = 100_000
	request.Model.MaxTokens = 10_000
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	entries := []session.Entry{compactionUserEntry("kept", mo.None[string](), "text")}
	sessions.EXPECT().CompactionSnapshot().Return(Snapshot{
		Identity: identity, ActiveLeafID: mo.Some("kept"), Entries: entries,
		Context: request.History, Previous: mo.None[session.CompactionEntry](),
	})
	sessions.EXPECT().ProjectSuffix(gomock.Any(), "kept").Return(request.History, nil).AnyTimes()
	supplier := Handler{ExtensionID: "supplier", RuntimeID: "runtime", HandlerID: "supply"}
	clearer := Handler{ExtensionID: "clearer", RuntimeID: "runtime", HandlerID: "clear"}
	generator := Handler{ExtensionID: "generator", RuntimeID: "runtime", HandlerID: "generate"}
	handlers := HandlerSet{
		Requests: []Handler{supplier, clearer}, Generators: []Handler{generator},
		Results: nil, Successes: nil, Failures: nil,
	}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(handlers)
	expectCompactionContext(contexts, "supplier")
	expectCompactionContext(contexts, "clearer")
	expectCompactionContext(contexts, "generator")
	supplied := failurePathResult("supplier")
	generated := failurePathResult("generator")
	gomock.InOrder(
		runtime.EXPECT().HandleCompactionRequest(gomock.Any(), supplier, gomock.Any(), gomock.Any()).Return(
			RequestAction{
				Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[Request](),
				ResultAction: ResultActionReplace, Result: mo.Some(supplied),
			}, nil,
		),
		runtime.EXPECT().HandleCompactionRequest(gomock.Any(), clearer, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ Handler, _ extension.Context, invocation RequestInvocation) (RequestAction, error) {
				require.Equal(t, supplied, invocation.CurrentResult.MustGet())
				return RequestAction{
					Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[Request](),
					ResultAction: ResultActionClear, Result: mo.None[Result](),
				}, nil
			},
		),
		runtime.EXPECT().GenerateCompaction(gomock.Any(), generator, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ Handler, _ extension.Context, invocation RequestInvocation) (Result, error) {
				require.True(t, invocation.CurrentResult.IsNone())
				return generated, nil
			},
		),
	)
	sessions.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(request.History).AnyTimes()
	committed := compactionMarkerEntry("compaction", "kept", generated)
	sessions.EXPECT().CommitCompaction(gomock.Any(), identity, mo.Some("kept"), gomock.Any()).Return(committed, nil)
	service := New(sessions)
	require.NoError(t, service.BindOrchestration(runtime, contexts, 20_000))

	// Act through request-state composition.
	result, err := service.Compact(t.Context(), TriggerManual, mo.None[string](), false, request)

	// Assert the generated replacement, not the cleared result, is committed.
	require.NoError(t, err)
	require.Equal(t, committed, result.Committed.MustGet())
}

// TestCompactRequiresExactlyOneGenerator verifies zero and multiple registrations fail without fallback.
func TestCompactRequiresExactlyOneGenerator(t *testing.T) {
	t.Parallel()
	for name, generators := range map[string][]Handler{
		"missing": nil,
		"multiple": {
			{ExtensionID: "one", RuntimeID: "r1", HandlerID: "g1"},
			{ExtensionID: "two", RuntimeID: "r2", HandlerID: "g2"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a valid proposal with no ready result.
			controller := gomock.NewController(t)
			sessions := NewMockSessionState(controller)
			runtime := NewMockRuntime(controller)
			contexts := NewMockContextIssuer(controller)
			request := baselineRequest()
			entries := []session.Entry{compactionUserEntry("kept", mo.None[string](), "text")}
			sessions.EXPECT().CompactionSnapshot().Return(Snapshot{
				Identity:     session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
				ActiveLeafID: mo.Some("kept"), Entries: entries, Context: request.History,
				Previous: mo.None[session.CompactionEntry](),
			})
			sessions.EXPECT().ProjectSuffix(gomock.Any(), "kept").Return(request.History, nil).AnyTimes()
			runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
				Requests: nil, Generators: generators, Results: nil, Successes: nil, Failures: nil,
			})
			service := New(sessions)
			require.NoError(t, service.BindOrchestration(runtime, contexts, 20_000))

			// Act without a bundled or fallback generator.
			result, err := service.Compact(t.Context(), TriggerManual, mo.None[string](), false, request)

			// Assert the explicit registration failure has extension identity and no commit.
			require.Error(t, err)
			var failure *FailureError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, FailureExtensionFailed, failure.Category)
			require.True(t, result.Committed.IsNone())
		})
	}
}

// TestCompactRejectsInvalidGeneratorResultBeforeResultHandlers verifies generation is one extension boundary.
func TestCompactRejectsInvalidGeneratorResultBeforeResultHandlers(t *testing.T) {
	t.Parallel()
	// Arrange one generator that returns an empty summary before a registered result handler.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	request := baselineRequest()
	request.Model.ContextWindow = 100_000
	request.Model.MaxTokens = 10_000
	entries := []session.Entry{compactionUserEntry("kept", mo.None[string](), "text")}
	sessions.EXPECT().CompactionSnapshot().Return(Snapshot{
		Identity:     session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
		ActiveLeafID: mo.Some("kept"), Entries: entries, Context: request.History,
		Previous: mo.None[session.CompactionEntry](),
	})
	sessions.EXPECT().ProjectSuffix(gomock.Any(), "kept").Return(request.History, nil).AnyTimes()
	generator := Handler{ExtensionID: "generator", RuntimeID: "runtime", HandlerID: "generate"}
	resultHandler := Handler{ExtensionID: "result", RuntimeID: "runtime", HandlerID: "repair"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
		Requests: nil, Generators: []Handler{generator}, Results: []Handler{resultHandler},
		Successes: nil, Failures: nil,
	})
	expectCompactionContext(contexts, "generator")
	runtime.EXPECT().GenerateCompaction(gomock.Any(), generator, gomock.Any(), gomock.Any()).Return(Result{
		Summary: "", FirstKeptEntryID: "kept",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("generator"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		Details: mo.None[[]byte](),
	}, nil)
	service := New(sessions)
	require.NoError(t, service.BindOrchestration(runtime, contexts, 20_000))

	// Act through custom generation.
	result, err := service.Compact(t.Context(), TriggerManual, mo.None[string](), false, request)

	// Assert the invalid generator result is terminal before any result handler or commit.
	require.True(t, result.Committed.IsNone())
	require.ErrorContains(t, err, "generator \"generate\"")
	require.ErrorContains(t, err, "summary is empty")
	var failure *FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, FailureExtensionFailed, failure.Category)
}

// TestCompactGeneratorAndConcurrentCancellation verifies generation commits once and cancellation retains user abort.
func TestCompactGeneratorAndConcurrentCancellation(t *testing.T) {
	t.Parallel()
	t.Run("generator", func(t *testing.T) {
		t.Parallel()
		// Arrange one exact registered generator.
		controller := gomock.NewController(t)
		sessions := NewMockSessionState(controller)
		runtime := NewMockRuntime(controller)
		contexts := NewMockContextIssuer(controller)
		request := baselineRequest()
		identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
		entries := []session.Entry{compactionUserEntry("kept", mo.None[string](), "text")}
		sessions.EXPECT().CompactionSnapshot().Return(Snapshot{
			Identity: identity, ActiveLeafID: mo.Some("kept"), Entries: entries,
			Context: request.History, Previous: mo.None[session.CompactionEntry](),
		})
		sessions.EXPECT().ProjectSuffix(gomock.Any(), "kept").Return(request.History, nil).AnyTimes()
		generator := Handler{ExtensionID: "generator", RuntimeID: "runtime", HandlerID: "generate"}
		runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
			Requests: nil, Generators: []Handler{generator}, Results: nil, Successes: nil, Failures: nil,
		})
		expectCompactionContext(contexts, "generator")
		generated := Result{
			Summary: "summary", FirstKeptEntryID: "kept",
			Source: session.CompactionSource{
				ExtensionID: mo.Some("generator"), Model: mo.None[session.BranchSummaryModelSource](),
			}, Details: mo.None[[]byte](),
		}
		runtime.EXPECT().GenerateCompaction(gomock.Any(), generator, gomock.Any(), gomock.Any()).Return(generated, nil)
		sessions.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(request.History).AnyTimes()
		committed := compactionMarkerEntry("compact", "kept", generated)
		sessions.EXPECT().CommitCompaction(gomock.Any(), identity, mo.Some("kept"), gomock.Any()).Return(committed, nil)
		service := New(sessions)
		require.NoError(t, service.BindOrchestration(runtime, contexts, 20_000))

		// Act through generation.
		result, err := service.Compact(t.Context(), TriggerManual, mo.None[string](), false, request)

		// Assert one durable commit.
		require.NoError(t, err)
		require.Equal(t, "compact", result.Committed.OrEmpty().ID)
	})

	t.Run("concurrent cancellation", func(t *testing.T) {
		t.Parallel()
		// Arrange a handler that explicitly cancels as the caller aborts.
		controller := gomock.NewController(t)
		sessions := NewMockSessionState(controller)
		runtime := NewMockRuntime(controller)
		contexts := NewMockContextIssuer(controller)
		request := baselineRequest()
		entries := []session.Entry{compactionUserEntry("kept", mo.None[string](), "text")}
		sessions.EXPECT().CompactionSnapshot().Return(Snapshot{
			Identity:     session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
			ActiveLeafID: mo.Some("kept"), Entries: entries, Context: request.History,
			Previous: mo.None[session.CompactionEntry](),
		})
		sessions.EXPECT().ProjectSuffix(gomock.Any(), "kept").Return(request.History, nil).AnyTimes()
		handler := Handler{ExtensionID: "extension", RuntimeID: "runtime", HandlerID: "cancel"}
		runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
			Requests: []Handler{handler}, Generators: nil, Results: nil, Successes: nil, Failures: nil,
		})
		expectCompactionContext(contexts, "extension")
		ctx, cancel := context.WithCancel(t.Context())
		runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handler, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ Handler, _ extension.Context, _ RequestInvocation) (RequestAction, error) {
				cancel()
				return RequestAction{
					Cancel: true, RequestAction: 0, Request: mo.None[Request](),
					ResultAction: 0, Result: mo.None[Result](),
				}, nil
			},
		)
		service := New(sessions)
		require.NoError(t, service.BindOrchestration(runtime, contexts, 20_000))

		// Act through simultaneous explicit and caller cancellation.
		result, err := service.Compact(ctx, TriggerManual, mo.None[string](), false, request)

		// Assert cancellation has no commit and preserves user abort.
		require.True(t, result.Canceled)
		require.True(t, result.Committed.IsNone())
		require.ErrorIs(t, err, context.Canceled)
	})
}
