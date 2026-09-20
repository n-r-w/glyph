//go:build integration

package contextcompaction_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	contextcompaction "github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestModelExecutionUsesProductionThresholdCompaction verifies the actual preparation owner rebuilds provider history.
func TestModelExecutionUsesProductionThresholdCompaction(t *testing.T) {
	t.Parallel()
	// Arrange actual compaction and model-execution services with mocked external boundaries.
	harness := newPreparationHarness(t, strings.Repeat("oversized ", 5_000), "small compacted request")
	harness.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request modelexecution.ProviderRequest, handle modelexecution.StreamHandler) error {
			require.Equal(t, "small compacted request", request.History[0].User.MustGet().Text(""))
			return emitPreparationTerminal(handle)
		},
	)

	// Act through the production logical model-execution boundary.
	err := harness.execution.Stream(t.Context(), harness.agentRequest, func(agentrun.StreamEvent) error { return nil })

	// Assert threshold preparation committed and dispatched only the rebuilt compacted projection.
	require.NoError(t, err)
}

// TestModelExecutionOverflowRecoveryRejectsPostCommitGrowth verifies recovery never dispatches known oversized context.
func TestModelExecutionOverflowRecoveryRejectsPostCommitGrowth(t *testing.T) {
	t.Parallel()
	// Arrange a provider overflow followed by committed compaction and observer-added context growth.
	harness := newPreparationHarness(t, "short initial context", strings.Repeat("observer growth ", 5_000))
	harness.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, modelexecution.ProviderRequest, modelexecution.StreamHandler) error {
			return &modelexecution.ProviderFailureError{
				Classification: modelexecution.ProviderFailureContextOverflow,
				RetryDelay:     mo.None[time.Duration](), Cause: errors.New("provider context overflow"),
			}
		},
	)

	// Act through the production overflow recovery inside model execution.
	err := harness.execution.Stream(t.Context(), harness.agentRequest, func(agentrun.StreamEvent) error { return nil })

	// Assert the committed marker remains durable while no second provider attempt receives oversized history.
	require.ErrorContains(t, err, "after compaction")
	require.ErrorContains(t, err, "exceeds input budget")
	failure, present := errors.AsType[interface {
		error
		FailureCode() string
	}](err)
	require.True(t, present)
	require.Equal(t, "COMPACTION_FAILED", failure.FailureCode())
}

// TestModelExecutionSecondOverflowAfterRecoveryIsTerminal verifies one successful recovery consumes its allowance.
func TestModelExecutionSecondOverflowAfterRecoveryIsTerminal(t *testing.T) {
	t.Parallel()
	// Arrange one provider that rejects both the initial and compacted requests as oversized.
	harness := newPreparationHarness(t, "short initial context", "small compacted request")
	harness.retryOutput.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(nil)
	calls := 0
	harness.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(_ context.Context, request modelexecution.ProviderRequest, _ modelexecution.StreamHandler) error {
			calls++
			if calls == 2 {
				require.Equal(t, "small compacted request", request.History[0].User.MustGet().Text(""))
			}
			return &modelexecution.ProviderFailureError{
				Classification: modelexecution.ProviderFailureContextOverflow,
				RetryDelay:     mo.None[time.Duration](), Cause: errors.New("provider context overflow"),
			}
		},
	)

	// Act through initial overflow, successful recovery, and one replacement attempt.
	err := harness.execution.Stream(t.Context(), harness.agentRequest, func(agentrun.StreamEvent) error { return nil })

	// Assert no second recovery starts and the terminal provider category is CONTEXT_LIMIT.
	require.Equalf(t, 2, calls, "terminal error: %v", err)
	failure, present := errors.AsType[interface {
		error
		FailureCode() string
	}](err)
	require.True(t, present)
	require.Equal(t, "CONTEXT_LIMIT", failure.FailureCode())
}

// preparationHarness contains production owners and mocked external boundaries for one integration scenario.
type preparationHarness struct {
	// execution is the production logical model-execution owner.
	execution *modelexecution.Service
	// provider is the external provider-attempt boundary.
	provider *modelexecution.MockProviderAttempt
	// retryOutput is the active Host-mode retry progress boundary.
	retryOutput *modelexecution.MockRetryOutput
	// agentRequest is the captured Agent Core request.
	agentRequest agentrun.ModelRequest
}

// newPreparationHarness assembles production compaction and model execution with one supplied extension result.
func newPreparationHarness(t *testing.T, initialText string, rebuiltText string) preparationHarness {
	t.Helper()
	controller := gomock.NewController(t)
	sessions := contextcompaction.NewMockSessionState(controller)
	runtime := contextcompaction.NewMockRuntime(controller)
	contexts := contextcompaction.NewMockContextIssuer(controller)
	catalog := modelexecution.NewMockCatalogResolver(controller)
	provider := modelexecution.NewMockProviderAttempt(controller)
	retryOutput := modelexecution.NewMockRetryOutput(controller)
	descriptor := preparationDescriptor()
	selection := model.Selection{
		Provider: descriptor.Provider, Model: descriptor.Model, ReasoningChoice: model.ReasoningChoiceOff,
	}
	providerRequest := modelexecution.ProviderRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: selection.ReasoningChoice,
		History: []agent.HistoryEntry{preparationHistory(initialText)}, Tools: nil,
	}
	catalog.EXPECT().ResolveBinding(selection).Return(modelexecution.CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	entries := []session.Entry{
		preparationEntry("old", mo.None[string](), "old"),
		preparationEntry("kept", mo.Some("old"), "kept"),
	}
	before := contextcompaction.Snapshot{
		Identity: identity, ActiveLeafID: mo.Some("kept"), Entries: entries,
		Context: providerRequest.History, Previous: mo.None[session.CompactionEntry](),
	}
	after := before
	after.Context = []agent.HistoryEntry{preparationHistory(rebuiltText)}
	sessions.EXPECT().CompactionSnapshot().Return(before)
	sessions.EXPECT().CompactionSnapshot().Return(after)
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	sessions.EXPECT().ProjectSuffix(gomock.Any(), gomock.Any()).Return(providerRequest.History, nil).AnyTimes()
	handler := contextcompaction.Handler{ExtensionID: "extension", RuntimeID: "runtime", HandlerID: "supply"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(contextcompaction.HandlerSet{
		Requests: []contextcompaction.Handler{handler}, Generators: nil,
		Results: nil, Successes: nil, Failures: nil,
	})
	contexts.EXPECT().IssueContext("extension").Return(extension.Context{
		ID: "context", ExtensionID: "extension", RuntimeInstanceID: "runtime",
		SessionID: identity.ID, WorkingDirectory: identity.WorkingDirectory,
	}, nil)
	result := contextcompaction.Result{
		Summary: "summary", FirstKeptEntryID: "kept",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		Details: mo.None[[]byte](),
	}
	runtime.EXPECT().HandleCompactionRequest(gomock.Any(), handler, gomock.Any(), gomock.Any()).Return(
		contextcompaction.RequestAction{
			Cancel: false, RequestAction: contextcompaction.RequestActionPreserve,
			Request:      mo.None[contextcompaction.Request](),
			ResultAction: contextcompaction.ResultActionReplace, Result: mo.Some(result),
		}, nil,
	)
	sessions.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(
		[]agent.HistoryEntry{preparationHistory("small compacted request")},
	).AnyTimes()
	committed := preparationEntry("compaction", mo.Some("kept"), "")
	committed.User = mo.None[session.UserMessage]()
	committed.Compaction = mo.Some(session.CompactionEntry{
		Summary: result.Summary, FirstKeptEntryID: result.FirstKeptEntryID, Source: result.Source,
		EstimatedCost: mo.None[session.EstimatedCost](), Details: result.Details,
	})
	sessions.EXPECT().CommitCompaction(gomock.Any(), identity, mo.Some("kept"), gomock.Any()).Return(committed, nil)
	compactor := contextcompaction.New(sessions)
	require.NoError(t, compactor.BindOrchestration(runtime, contexts, 20_000))
	execution := modelexecution.New(catalog, compactor, modelexecution.RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: time.Second,
	}, nil, retryOutput)
	return preparationHarness{
		execution: execution, provider: provider, retryOutput: retryOutput,
		agentRequest: agentrun.ModelRequest{
			Instructions: providerRequest.Instructions, Model: descriptor,
			ReasoningChoice: selection.ReasoningChoice, History: providerRequest.History, Tools: nil,
		},
	}
}

// preparationDescriptor creates one valid provider-neutral model with a 9,000-token input budget.
func preparationDescriptor() model.Descriptor {
	return model.Descriptor{
		Provider: "provider", Model: "model", Input: []model.InputModality{model.InputModalityText},
		ContextWindow: 10_000, MaxTokens: 1_000,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: false, Choices: []model.ReasoningChoice{model.ReasoningChoiceOff},
			Default: model.ReasoningChoiceOff,
		},
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: false,
			Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
		},
		Pricing: mo.None[model.Pricing](),
	}
}

// preparationHistory creates one provider-visible user history entry.
func preparationHistory(text string) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(text)),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}
}

// preparationEntry creates one persisted user entry fixture.
func preparationEntry(id string, parentID mo.Option[string], text string) session.Entry {
	return session.Entry{
		ID: id, ParentID: parentID, CreatedAt: time.Unix(1, 0),
		Information: mo.None[session.Information](), User: mo.Some(model.TextMessage(text)),
		Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), ExtensionMessage: mo.None[session.ExtensionMessage](),
		EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.None[session.CompactionEntry](),
	}
}

// emitPreparationTerminal emits one complete successful raw provider response.
func emitPreparationTerminal(handle modelexecution.StreamHandler) error {
	return handle(modelexecution.StreamEvent{
		Kind: modelexecution.StreamEventDone, Position: mo.None[int](), Content: mo.None[model.Content](),
		Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.Some(model.Response{
			Content: nil, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
			Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
			ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
			Usage: mo.None[model.Usage](), Diagnostics: nil,
		}),
	})
}
