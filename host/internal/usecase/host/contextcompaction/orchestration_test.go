//go:build !integration

package contextcompaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestClassifyCompactionFailurePrefersPersistenceToMixedCancellation verifies durable failure identity wins.
func TestClassifyCompactionFailurePrefersPersistenceToMixedCancellation(t *testing.T) {
	t.Parallel()
	// Arrange independently acquired persistence and caller-cancellation causes.
	cause := errors.Join(session.ErrPersistenceUnavailable, context.Canceled)

	// Act through the compaction terminal classifier.
	err := classifyCompactionFailure(cause)

	// Assert durable storage identity and every original cause remain available.
	var failure *FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, FailurePersistenceUnavailable, failure.Category)
	require.ErrorIs(t, err, session.ErrPersistenceUnavailable)
	require.ErrorIs(t, err, context.Canceled)
}

// TestValidateRequestBudgetsRejectsMalformedModelState verifies invalid replacement state stops composition.
func TestValidateRequestBudgetsRejectsMalformedModelState(t *testing.T) {
	t.Parallel()
	// Arrange malformed descriptor, reasoning-selection, and estimate-marker variants.
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{name: "missing provider", mutate: func(request *Request) { request.Model.Provider = "" }},
		{name: "missing modalities", mutate: func(request *Request) { request.Model.Input = nil }},
		{name: "unknown modality", mutate: func(request *Request) {
			request.Model.Input = []model.InputModality{model.InputModality("audio")}
		}},
		{name: "invalid reasoning capabilities", mutate: func(request *Request) {
			request.Model.ReasoningCapabilities.Choices = nil
		}},
		{name: "unsupported reasoning choice", mutate: func(request *Request) {
			request.ReasoningChoice = model.ReasoningChoiceMax
		}},
		{name: "unmarked estimate", mutate: func(request *Request) { request.ContextTokensEstimated = false }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			providerRequest := baselineRequest()
			request := Request{
				Trigger: TriggerManual, RetryIntent: false, Instructions: mo.None[string](),
				Model: providerRequest.Model.Clone(), ReasoningChoice: providerRequest.ReasoningChoice,
				Prefix: nil, Suffix: nil, Previous: mo.None[session.CompactionEntry](),
				ContextTokens: 10, ContextTokensEstimated: true,
				ContextWindow:  providerRequest.Model.ContextWindow,
				ResponseBudget: providerRequest.Model.MaxTokens, RetainedBudget: 20,
			}
			testCase.mutate(&request)

			// Act at the per-handler validation boundary.
			err := validateRequestBudgets(request)

			// Assert malformed state is rejected before a later handler sees it.
			require.Error(t, err)
		})
	}
}

// TestValidateReplacementEntryPayloadRejectsMalformedCompaction verifies every nested marker invariant.
func TestValidateReplacementEntryPayloadRejectsMalformedCompaction(t *testing.T) {
	t.Parallel()
	valid := session.CompactionEntry{
		Summary: "summary", FirstKeptEntryID: "kept",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*session.CompactionEntry)
		cause  string
	}{
		{name: "missing summary", mutate: func(value *session.CompactionEntry) { value.Summary = "" }, cause: "summary"},
		{name: "missing boundary", mutate: func(value *session.CompactionEntry) { value.FirstKeptEntryID = "" }, cause: "boundary"},
		{name: "invalid accounting", mutate: func(value *session.CompactionEntry) {
			value.Source.ExtensionID = mo.None[string]()
		}, cause: "source"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			marker := valid.Clone()
			testCase.mutate(&marker)
			entry := compactionMarkerEntry("marker", "kept", Result{
				Summary: marker.Summary, FirstKeptEntryID: marker.FirstKeptEntryID,
				Source: marker.Source, Details: marker.Details,
			})

			// Act at the complete nested replacement payload boundary.
			err := validateReplacementEntryPayload(entry)

			// Assert the exact malformed nested value is rejected.
			require.ErrorContains(t, err, testCase.cause)
		})
	}
}

// TestCompactPreservesPreStartCancellationCause verifies no orchestration work replaces caller identity.
func TestCompactPreservesPreStartCancellationCause(t *testing.T) {
	t.Parallel()
	// Arrange an operation canceled with an independent caller-owned cause.
	cause := errors.New("caller stopped compaction")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)

	// Act before any orchestration dependency can be used.
	_, err := new(Service).Compact(ctx, TriggerManual, mo.None[string](), false, modelexecution.ProviderRequest{})

	// Assert the original cancellation cause remains directly discoverable.
	require.ErrorIs(t, err, cause)
}

// TestCompactComposesReadyResultAndRetainsCommittedObserverFailure verifies immutable chains, generator bypass,
// final validation, one commit, and post-commit failure retention.
func TestCompactComposesReadyResultAndRetainsCommittedObserverFailure(t *testing.T) {
	t.Parallel()
	// Arrange one captured branch and ordered public capabilities.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	request := baselineRequest()
	request.Model.ContextWindow = 100_000
	request.Model.MaxTokens = 10_000
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 4}
	entries := []session.Entry{
		compactionUserEntry("u1", mo.None[string](), "old"),
		compactionUserEntry("u2", mo.Some("u1"), "new"),
	}
	snapshot := Snapshot{
		Identity: identity, ActiveLeafID: mo.Some("u2"), Entries: entries,
		Context: request.History, Previous: mo.None[session.CompactionEntry](),
	}
	ready := Result{
		Summary:          "summary",
		FirstKeptEntryID: "u2",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("first"),
			Model:       mo.None[session.BranchSummaryModelSource](),
		},
		Details: mo.Some([]byte("details")),
	}
	handlers := HandlerSet{
		Requests: []Handler{
			{ExtensionID: "first", RuntimeID: "runtime-1", HandlerID: "supply"},
			{ExtensionID: "second", RuntimeID: "runtime-2", HandlerID: "inspect"},
		},
		Generators: nil,
		Results:    []Handler{{ExtensionID: "third", RuntimeID: "runtime-3", HandlerID: "refine"}},
		Successes:  []Handler{{ExtensionID: "observer", RuntimeID: "runtime-4", HandlerID: "success"}},
		Failures:   nil,
	}
	sessions.EXPECT().CompactionSnapshot().Return(snapshot)
	sessions.EXPECT().ProjectSuffix(gomock.Any(), gomock.Any()).Return(request.History, nil).AnyTimes()
	runtime.EXPECT().SnapshotCompactionHandlers().Return(handlers)
	expectCompactionContext(contexts, "first")
	expectCompactionContext(contexts, "second")
	expectCompactionContext(contexts, "third")
	expectCompactionContext(contexts, "observer")
	var immutableOriginal Request
	gomock.InOrder(
		runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handlers.Requests[0], gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ Handler, _ extension.Context, invocation RequestInvocation) (RequestAction, error) {
				immutableOriginal = invocation.Original
				require.Equal(t, estimateRecord(int64(len("new")), 0), invocation.Original.Suffix[0].EstimatedTokens)
				replacement := invocation.Current
				message := replacement.Suffix[0].User.MustGet()
				message.Content[0].Text = mo.Some("changed by first handler")
				replacement.Suffix[0].User = mo.Some(message)
				replacement.Suffix[0].EstimatedTokens = 999
				replacement.Previous = mo.Some(session.CompactionEntry{
					Summary: "replacement previous summary", FirstKeptEntryID: "u1",
					Source: session.CompactionSource{
						ExtensionID: mo.Some("replacement-owner"), Model: mo.None[session.BranchSummaryModelSource](),
					},
					EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
				})
				return RequestAction{
					Cancel: false, RequestAction: RequestActionReplace, Request: mo.Some(replacement),
					ResultAction: ResultActionReplace, Result: mo.Some(ready),
				}, nil
			}),
		runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handlers.Requests[1], gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ Handler, _ extension.Context, invocation RequestInvocation) (RequestAction, error) {
				require.Equal(t, immutableOriginal, invocation.Original)
				require.Equal(
					t,
					"changed by first handler",
					invocation.Current.Suffix[0].User.MustGet().Content[0].Text.MustGet(),
				)
				require.Equal(t, "replacement previous summary", invocation.Current.Previous.MustGet().Summary)
				require.Equal(
					t, estimateRecord(int64(len("changed by first handler")), 0),
					invocation.Current.Suffix[0].EstimatedTokens,
				)
				require.Equal(t, estimateRecord(int64(len("new")), 0), invocation.Original.Suffix[0].EstimatedTokens)
				require.Equal(t, ready, invocation.CurrentResult.MustGet())
				return RequestAction{
					Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[Request](),
					ResultAction: ResultActionPreserve, Result: mo.None[Result](),
				}, nil
			}),
		runtime.EXPECT().HandleCompactionResult(gomock.Any(), handlers.Results[0], gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ Handler, _ extension.Context, invocation ResultInvocation) (ResultAction, error) {
				require.Equal(t, ready, invocation.OriginalResult)
				require.Equal(t, ready, invocation.CurrentResult)
				return ResultAction{Cancel: false, Preserve: true, Result: mo.None[Result]()}, nil
			}),
	)
	sessions.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(
		[]agent.HistoryEntry{textHistory("fits")},
	).Times(5)
	committed := compactionMarkerEntry("compact", "u2", ready)
	publicationCause := errors.New("publication failed after commit")
	sessions.EXPECT().CommitCompaction(gomock.Any(), identity, mo.Some("u2"), gomock.Any()).Return(
		committed, publicationCause,
	)
	observerCause := errors.New("observer failed after commit")
	callerCause := errors.New("caller canceled during success observation")
	ctx, cancel := context.WithCancelCause(t.Context())
	runtime.EXPECT().ObserveCompactionSuccess(gomock.Any(), handlers.Successes[0], gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, Handler, extension.Context, OutcomeInvocation) error {
			cancel(callerCause)
			return observerCause
		})
	service := New(sessions)
	service.BindOrchestration(runtime, contexts, 20_000)

	// Act through the manual orchestration entry point.
	result, err := service.Compact(ctx, TriggerManual, mo.Some("focus"), false, request)

	// Assert durable state is retained and the complete observer failure is public.
	require.Equal(t, committed, result.Committed.MustGet())
	require.False(t, result.Canceled)
	require.ErrorIs(t, err, publicationCause)
	require.ErrorIs(t, err, observerCause)
	require.ErrorIs(t, err, callerCause)
	var failure *FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, FailureInternal, failure.Category)
	require.Contains(t, err.Error(), "publication failed after commit")
	require.Contains(t, err.Error(), "observer failed after commit")
}

// expectCompactionContext supplies one complete session-bound handler context.
func expectCompactionContext(contexts *MockContextIssuer, extensionID string) {
	contexts.EXPECT().IssueContext(extensionID).Return(extension.Context{
		ID: "context-" + extensionID, ExtensionID: extensionID, RuntimeInstanceID: "runtime-" + extensionID,
		SessionID: "session", WorkingDirectory: "/project",
	}, nil)
}

// compactionUserEntry creates one complete persisted user entry fixture.
func compactionUserEntry(id string, parentID mo.Option[string], text string) session.Entry {
	return session.Entry{
		ID:               id,
		ParentID:         parentID,
		CreatedAt:        time.Unix(1, 0),
		Information:      mo.None[session.Information](),
		User:             mo.Some(model.TextMessage(text)),
		Model:            mo.None[session.ModelResponse](),
		ToolResult:       mo.None[session.ToolResult](),
		Extension:        mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
		BranchSummary:    mo.None[session.BranchSummaryEntry](),
		Compaction:       mo.None[session.CompactionEntry](),
	}
}

// compactionMarkerEntry creates one complete committed marker fixture.
func compactionMarkerEntry(id, parentID string, result Result) session.Entry {
	return session.Entry{
		ID:               id,
		ParentID:         mo.Some(parentID),
		CreatedAt:        time.Unix(2, 0),
		Information:      mo.None[session.Information](),
		User:             mo.None[session.UserMessage](),
		Model:            mo.None[session.ModelResponse](),
		ToolResult:       mo.None[session.ToolResult](),
		Extension:        mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
		BranchSummary:    mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(session.CompactionEntry{
			Summary: result.Summary, FirstKeptEntryID: result.FirstKeptEntryID, Source: result.Source,
			EstimatedCost: mo.None[session.EstimatedCost](), Details: result.Details,
		}),
	}
}
