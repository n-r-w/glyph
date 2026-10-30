//go:build integration

package extensioncontext

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
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestModelExecutionOverflowRecoveryRejectsPostCommitGrowth checks suppressed retry after final-input rejection.
func TestModelExecutionOverflowRecoveryRejectsPostCommitGrowth(t *testing.T) {
	t.Parallel()
	// Arrange a provider overflow and an owner rejecting notified outgoing history after its durable replacement.
	finalCause := errors.New("after compaction: outgoing input exceeds input budget\nsource β")
	harness := newOverflowExecutionHarness(t, "initial context", strings.Repeat("observer growth ", 5_000), finalCause)
	providerCause := errors.New("provider context overflow\nsource α")
	harness.provider.EXPECT().
		Stream(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&modelexecution.ProviderFailureError{
			Classification: modelexecution.ProviderFailureContextOverflow,
			RetryDelay:     mo.None[time.Duration](), Cause: providerCause,
		})

	// Act through one logical model execution and captured recovery.
	err := streamLogical(
		t.Context(),
		harness.execution,
		harness.agentRequest,
		func(agentrun.StreamEvent) error { return nil },
	)

	// Assert the independent receipt and every cause survive without another provider dispatch.
	require.ErrorIs(t, err, providerCause)
	require.ErrorIs(t, err, finalCause)
	require.ErrorContains(t, err, providerCause.Error())
	require.ErrorContains(t, err, finalCause.Error())
	failure, present := errors.AsType[*modelexecution.LogicalFailureError](err)
	require.True(t, present)
	require.Equal(t, modelexecution.FailureCompactionFailed, failure.Category)
	require.Equal(t, 1, *harness.commits)
	require.Empty(t, harness.contexts.compactionOperations)
}

// TestModelExecutionOverflowAtAttemptExhaustionIsTerminal checks exhausted overflow without mutation.
func TestModelExecutionOverflowAtAttemptExhaustionIsTerminal(t *testing.T) {
	t.Parallel()
	// Arrange one admitted recovery followed by overflow at the generic attempt limit.
	harness := newOverflowExecutionHarness(t, "initial context", "rebuilt context", nil)
	harness.retryOutput.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(nil)
	causes := []error{errors.New("first overflow\nsource α"), errors.New("exhausted overflow\nsource β")}
	calls := 0
	harness.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(_ context.Context, request modelexecution.ProviderRequest, _ modelexecution.StreamHandler) error {
			if calls == 1 {
				require.Equal(t, "rebuilt context", request.History[0].User.MustGet().Text(""))
			}
			cause := causes[calls]
			calls++
			return &modelexecution.ProviderFailureError{
				Classification: modelexecution.ProviderFailureContextOverflow,
				RetryDelay:     mo.None[time.Duration](), Cause: cause,
			}
		})

	// Act through the admitted retry and its exhausted overflow.
	err := streamLogical(
		t.Context(),
		harness.execution,
		harness.agentRequest,
		func(agentrun.StreamEvent) error { return nil },
	)

	// Assert the captured owner receives only the admitted recovery and all provider causes remain terminal.
	require.Equal(t, 2, calls)
	require.Equal(t, 1, *harness.commits)
	for _, cause := range causes {
		require.ErrorIs(t, err, cause)
		require.ErrorContains(t, err, cause.Error())
	}
	failure, present := errors.AsType[*modelexecution.LogicalFailureError](err)
	require.True(t, present)
	require.Equal(t, modelexecution.FailureContextLimit, failure.Category)
	require.Empty(t, harness.contexts.compactionOperations)
}

// overflowExecutionHarness contains components for captured policy integration and logical model execution.
type overflowExecutionHarness struct {
	// execution owns the logical provider attempts.
	execution *modelexecution.Service
	// contexts owns captured policy lifetimes and receipts.
	contexts *Service
	// provider represents the external provider boundary.
	provider *modelexecution.MockProviderAttempt
	// retryOutput represents Host progress delivery.
	retryOutput *modelexecution.MockRetryOutput
	// agentRequest contains the frozen conversation binding.
	agentRequest agentrun.ContextRequest
	// commits is the number of independently acquired replacement receipts.
	commits *int
}

// newOverflowExecutionHarness assembles the captured recovery path with isolated external ports.
func newOverflowExecutionHarness(t *testing.T, initial, rebuilt string, finalCause error) overflowExecutionHarness {
	t.Helper()
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	state := NewMockSessionState(controller)
	policy := NewMockPolicyRuntime(controller)
	compactions := NewMockCompactionSessions(controller)
	catalog := modelexecution.NewMockCatalogResolver(controller)
	provider := modelexecution.NewMockProviderAttempt(controller)

	output := modelexecution.NewMockRetryOutput(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "prepare"}
	runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true).AnyTimes()
	state.EXPECT().ContextSession().Return(identity).AnyTimes()
	policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner}).Times(1)
	policy.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{}).Times(1)
	before := CompactionSnapshot{
		Identity:     identity,
		ActiveLeafID: mo.Some("kept"),
		Entries:      nil,
		History: session.PreparedHistory{
			{
				History:         newOverflowExecutionHistory(initial),
				SourceID:        "kept",
				SourcePosition:  0,
				SourceKind:      session.HistorySourceUser,
				PreviousSummary: false,
			},
		},
		Previous: mo.None[session.CompactionEntry](),
	}
	candidate := session.CompactionEntry{
		Summary:          "summary",
		FirstKeptEntryID: "kept",
		Source: session.CompactionSource{
			ExtensionID: mo.Some(owner.ExtensionID),
			Requests:    nil,
		},

		Details: mo.None[[]byte](),
	}
	receipt := session.Entry{
		ID: "receipt", ParentID: mo.Some("kept"), CreatedAt: time.Unix(1, 0),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), ExtensionMessage: mo.None[session.ExtensionMessage](),
		EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(candidate),
	}
	after := CompactionSnapshot{
		Identity:     identity,
		ActiveLeafID: mo.Some("notified"),
		Entries:      nil,
		History: session.PreparedHistory{
			{
				History:         newOverflowExecutionHistory(rebuilt),
				SourceID:        "notified",
				SourcePosition:  0,
				SourceKind:      session.HistorySourceUser,
				PreviousSummary: false,
			},
		},
		Previous: mo.Some(candidate),
	}
	originalCapture := compactions.EXPECT().CaptureCompaction().Return(before).Times(2)
	compactions.EXPECT().CaptureCompaction().Return(after).After(originalCapture).Times(1)
	service := New(runtime, state)
	service.BindPolicy(policy, compactions, 0)
	commits := 0
	runtime.EXPECT().BeginContextCommit(owner.ExtensionID, owner.RuntimeID).Return(func() {}, nil).Times(1)
	compactions.EXPECT().CommitReplacement(gomock.Any(), before, candidate, gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			_ CompactionSnapshot,
			_ session.CompactionEntry,
			guard ContextCommitGuard,
			record func(session.Entry),
		) (session.Entry, error) {
			release, err := guard()
			require.NoError(t, err)
			defer release()
			commits++
			record(receipt)
			return receipt, nil
		})
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(
			ctx context.Context,
			_ PolicyOwner,
			_ extension.Context,
			invocation PolicyInvocation,
		) (PolicyDisposition, error) {
			if invocation.Purpose == PolicyPurposeAutomatic {
				return PolicyPreserve, nil
			}
			require.Equal(t, PolicyPurposeOverflow, invocation.Purpose)
			require.True(t, invocation.OverflowError.IsSome())
			_, err := service.CommitPolicyReplacement(
				ctx,
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.OperationID,
				candidate,
			)
			return PolicyReplaced, err
		})
	policy.EXPECT().ValidateOutgoingInput(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ PolicyOwner, _ extension.Context, outgoing OutgoingInput) error {
			require.Equal(t, after, outgoing.Snapshot)
			require.Equal(t, after.History.Content(), outgoing.Input.History)
			require.Equal(t, "instructions", outgoing.Input.Instructions)
			return NewPolicyOperationError(ctx, modelexecution.FailureCompactionFailed, finalCause)
		})
	descriptor := model.Descriptor{
		Provider:      "provider",
		Model:         "model",
		Input:         []model.InputModality{model.InputModalityText},
		ContextWindow: 10_000,
		MaxTokens:     1_000,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: false,
			Choices:   []model.ReasoningChoice{model.ReasoningChoiceOff},
			Default:   model.ReasoningChoiceOff,
		},
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: false,
			Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
		},
		Pricing: mo.None[model.Pricing](),
	}
	selection := model.Selection{
		Provider:        descriptor.Provider,
		Model:           descriptor.Model,
		ReasoningChoice: model.ReasoningChoiceOff,
	}
	catalog.EXPECT().
		ActiveBinding().
		Return(modelexecution.CatalogBinding{
			Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
		})
	execution := modelexecution.New(catalog, modelexecution.RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: time.Second,
	}, nil, output, service)
	return overflowExecutionHarness{
		execution:   execution,
		contexts:    service,
		provider:    provider,
		retryOutput: output,
		agentRequest: agentrun.ContextRequest{
			Instructions: "instructions",
			History:      before.History.Content(),
			Tools:        nil,
		},
		commits: &commits,
	}
}

// newOverflowExecutionHistory creates a provider-neutral user entry.
func newOverflowExecutionHistory(text string) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(text)),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}
}

// streamLogical executes one Agent Core request through the effective-context port and its prepared provider.
func streamLogical(
	ctx context.Context,
	execution *modelexecution.Service,
	request agentrun.ContextRequest,
	handle agentrun.StreamHandler,
) error {
	effective, err := execution.PrepareContext(ctx, request)
	if err != nil {
		return err
	}
	return effective.Provider.Stream(ctx, effective.Request, handle)
}
