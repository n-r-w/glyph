//go:build !integration

package extensioncontext

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestAutomaticPreserveUsesCapturedSession checks coherent history and one owner/participant capture.
func TestAutomaticPreserveUsesCapturedSession(t *testing.T) {
	t.Parallel()
	// Arrange a session projection distinct from the caller's earlier history.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	policy := NewMockPolicyRuntime(controller)
	compactions := NewMockCompactionSessions(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "prepare"}
	runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true).AnyTimes()
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	handlers := contextcompaction.HandlerSet{}
	policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner}).Times(1)
	policy.EXPECT().SnapshotCompactionHandlers().Return(handlers).Times(1)
	history := []agent.HistoryEntry{
		{
			Kind:       agent.HistoryEntryUser,
			User:       mo.Some(model.TextMessage("captured session")),
			Model:      mo.None[model.Response](),
			ToolResult: mo.None[agent.ToolResult](),
		},
	}
	snapshot := CompactionSnapshot{
		Identity:     identity,
		ActiveLeafID: mo.Some("leaf"),
		Entries:      nil,
		History:      history,
		Previous:     mo.None[session.CompactionEntry](),
	}
	compactions.EXPECT().CaptureCompaction().Return(snapshot).Times(1)
	var actual PolicyInvocation
	policy.EXPECT().
		InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ PolicyOwner, _ extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
			actual = input
			return PolicyPreserve, nil
		})
	policy.EXPECT().ObserveConversation(gomock.Any(), owner, gomock.Any(), gomock.Any()).Return(nil).Times(1)
	service := New(runtime, sessions)
	service.BindPolicy(policy, compactions, 0)
	capture, err := service.CaptureConversation()
	require.NoError(t, err)
	input := modelexecution.ProviderRequest{
		Instructions:    "frozen instructions",
		Model:           model.Descriptor{},
		ReasoningChoice: model.ReasoningChoiceOff,
		History: []agent.HistoryEntry{
			{
				Kind:       agent.HistoryEntryUser,
				User:       mo.Some(model.TextMessage("earlier caller")),
				Model:      mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
			},
		},
		Tools: nil,
	}
	// Act through preparation and observation on the same captured owner.
	prepared, err := capture.MustGet().Prepare(t.Context(), input)
	require.NoError(t, err)
	err = capture.MustGet().Observe(t.Context(), input, model.Response{})
	// Assert the policy and provider preparation use the captured session projection without a replacement outcome.
	require.NoError(t, err)
	require.Equal(t, history, prepared.History)
	require.False(t, prepared.Committed)
	require.Equal(t, PolicyPurposeAutomatic, actual.Purpose)
	require.Equal(t, snapshot, actual.Snapshot)
	require.Equal(t, history, actual.Input.History)
	require.Equal(t, input.Instructions, actual.Input.Instructions)
	require.Equal(t, input.Model, actual.Input.Model)
	require.Empty(t, service.compactionOperations)
}

// TestAutomaticReplacementChecksNotifiedHistory checks final input capture, receipt retention and lifecycle-only validation.
func TestAutomaticReplacementChecksNotifiedHistory(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"accepted", "rejected", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			// Arrange one captured replacement and a success observer that grows outgoing history.
			controller := gomock.NewController(t)
			runtime := NewMockRuntimeState(controller)
			sessions := NewMockSessionState(controller)
			policy := NewMockPolicyRuntime(controller)
			compactions := NewMockCompactionSessions(controller)
			identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
			owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "prepare"}
			observer := contextcompaction.Handler{
				ExtensionID: owner.ExtensionID,
				RuntimeID:   owner.RuntimeID,
				HandlerID:   "success",
			}
			runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true).AnyTimes()
			sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
			policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner}).Times(1)
			policy.EXPECT().
				SnapshotCompactionHandlers().
				Return(contextcompaction.HandlerSet{Requests: nil, Generators: nil, Results: nil, Successes: []contextcompaction.Handler{observer}, Failures: nil}).
				Times(1)
			original := []agent.HistoryEntry{newAutomaticTestHistory("original")}
			snapshot := CompactionSnapshot{
				Identity:     identity,
				ActiveLeafID: mo.Some("leaf"),
				Entries:      nil,
				History:      original,
				Previous:     mo.None[session.CompactionEntry](),
			}
			candidate := session.CompactionEntry{
				Summary:          "summary",
				FirstKeptEntryID: "leaf",
				Source: session.CompactionSource{
					ExtensionID: mo.Some(owner.ExtensionID),
					Model:       mo.None[session.BranchSummaryModelSource](),
				},
				EstimatedCost: mo.None[session.EstimatedCost](),
				Details:       mo.None[[]byte](),
			}
			receipt := newManualTestReceipt(candidate)
			service := New(runtime, sessions)
			service.BindPolicy(policy, compactions, 0)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			compactions.EXPECT().CaptureCompaction().Return(snapshot).Times(1)
			compactions.EXPECT().
				CommitReplacement(gomock.Any(), snapshot, candidate, gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, _ CompactionSnapshot, _ session.CompactionEntry, _ ContextCommitGuard, record func(session.Entry)) (session.Entry, error) {
					record(receipt)
					return receipt, nil
				})
			var commitCause error
			policy.EXPECT().
				InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, _ PolicyOwner, _ extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
					_, commitCause = service.CommitPolicyReplacement(
						ctx,
						owner.ExtensionID,
						owner.RuntimeID,
						input.OperationID,
						candidate,
					)
					return PolicyReplaced, commitCause
				})
			notified := false
			policy.EXPECT().
				ObserveCompactionSuccess(gomock.Any(), observer, gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, _ contextcompaction.Handler, _ extension.Context, _ contextcompaction.OutcomeInvocation) error {
					notified = true
					return nil
				})
			outgoing := CompactionSnapshot{
				Identity:     identity,
				ActiveLeafID: mo.Some("observer-leaf"),
				Entries:      nil,
				History: []agent.HistoryEntry{
					newAutomaticTestHistory("summary"),
					newAutomaticTestHistory("suffix"),
					newAutomaticTestHistory("observer append"),
				},
				Previous: mo.Some(candidate),
			}
			capturedAfterNotification := false
			compactions.EXPECT().
				CaptureCompaction().
				DoAndReturn(func() CompactionSnapshot { capturedAfterNotification = notified; return outgoing }).
				Times(1)
			finalCause := errors.New("policy rejected actual outgoing input: complete budget cause")
			var actual OutgoingInput
			var authorityCause error
			policy.EXPECT().
				ValidateOutgoingInput(gomock.Any(), owner, gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, _ PolicyOwner, _ extension.Context, input OutgoingInput) error {
					actual = input
					_, authorityCause = service.ProjectPolicyReplacement(
						ctx,
						owner.ExtensionID,
						owner.RuntimeID,
						input.LifecycleID,
						candidate,
					)
					// A later append changes the next projection after the outgoing input was captured.
					outgoing.History = append(outgoing.History, newAutomaticTestHistory("later append"))
					if scenario == "rejected" {
						return finalCause
					}
					if scenario == "canceled" {
						cancel()
						return context.Canceled
					}
					return nil
				}).
				Times(1)
			capture, err := service.CaptureConversation()
			require.NoError(t, err)
			input := modelexecution.ProviderRequest{
				Instructions:    "frozen instructions",
				Model:           model.Descriptor{},
				ReasoningChoice: model.ReasoningChoiceOff,
				History:         nil,
				Tools:           nil,
			}
			// Act through automatic preparation and its captured final input check.
			prepared, err := capture.MustGet().Prepare(ctx, input)
			// Assert durable outcome, notified input isolation and absence of policy authority during the final check.
			require.NoError(t, commitCause)
			require.True(t, prepared.Committed)
			require.True(t, capturedAfterNotification)
			require.Error(t, authorityCause)
			require.NotEmpty(t, actual.LifecycleID)
			require.Equal(t, "observer-leaf", actual.Snapshot.ActiveLeafID.MustGet())
			require.Equal(t, input.Instructions, actual.Input.Instructions)
			require.Equal(t, input.Model, actual.Input.Model)
			require.Len(t, actual.Input.History, 3)
			require.Equal(t, "observer append", actual.Input.History[2].User.MustGet().Content[0].Text.MustGet())
			if scenario == "accepted" {
				require.NoError(t, err)
				require.Equal(t, actual.Input.History, prepared.History)
			}
			if scenario == "rejected" {
				require.ErrorIs(t, err, finalCause)
				require.ErrorContains(t, err, finalCause.Error())
			}
			if scenario == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Empty(t, service.compactionOperations)
		})
	}
}

// newAutomaticTestHistory creates provider-neutral text for a captured session projection.
func newAutomaticTestHistory(text string) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind:       agent.HistoryEntryUser,
		User:       mo.Some(model.TextMessage(text)),
		Model:      mo.None[model.Response](),
		ToolResult: mo.None[agent.ToolResult](),
	}
}
