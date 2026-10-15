//go:build integration

package extensioncontext

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestConversationObservationClassifiedSessionFailure checks categorized session errors during observation teardown.
func TestConversationObservationClassifiedSessionFailure(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name       string
		canceled   bool
		classified bool
	}{
		{name: "acknowledged failure", canceled: false, classified: true},
		{name: "caller cancellation with acquired failure", canceled: true, classified: true},
		{name: "normal teardown", canceled: false, classified: false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange production capture and logical execution for classified failure and normal teardown.
			controller := gomock.NewController(t)
			runtime := NewMockRuntimeState(controller)
			sessions := NewMockSessionState(controller)
			policies := NewMockPolicyRuntime(controller)
			owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "conversation"}
			identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
			runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true).AnyTimes()
			sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
			policies.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner})
			contexts := New(runtime, sessions)
			compactions := NewMockCompactionSessions(controller)
			compactions.EXPECT().CaptureCompaction().Return(CompactionSnapshot{
				Identity: identity, ActiveLeafID: mo.None[string](), Entries: nil,
				History: nil, Previous: mo.None[session.CompactionEntry](),
			})
			policies.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{})
			policies.EXPECT().
				InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).
				Return(PolicyPreserve, nil)
			contexts.BindPolicy(policies, compactions, 5*time.Minute)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			acquired := &ContextError{code: persistenceUnavailableCode, cause: context.Canceled}
			var lifecycleID string
			var joined chan struct{}
			policies.EXPECT().ObserveConversation(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
				func(parent context.Context, _ PolicyOwner, _ extension.Context, invocation ConversationObservation) error {
					lifecycleID = invocation.LifecycleID
					release, err := contexts.BeginCompactionChild(
						owner.ExtensionID,
						owner.RuntimeID,
						invocation.LifecycleID,
					)
					if err != nil {
						return err
					}
					if scenario.classified {
						release(acquired)
					} else {
						joined = make(chan struct{})
						child, unlink := contexts.LinkCompactionChild(parent, invocation.LifecycleID)
						go func() {
							<-child.Done()
							cause := context.Cause(child)
							unlink()
							close(joined)
							release(cause)
						}()
					}
					if scenario.canceled {
						cancel()
						return context.Canceled
					}
					return nil
				},
			)
			catalog := modelexecution.NewMockCatalogResolver(controller)
			provider := modelexecution.NewMockProviderAttempt(controller)

			descriptor := model.Descriptor{
				Provider:      "provider",
				Model:         "model",
				Input:         []model.InputModality{model.InputModalityText},
				ContextWindow: 100,
				MaxTokens:     10,
				ReasoningCapabilities: model.ReasoningCapabilities{
					Supported: false,
					Choices:   []model.ReasoningChoice{model.ReasoningChoiceOff},
					Default:   model.ReasoningChoiceOff,
				},
				ToolCapabilities: model.ToolCapabilities{},
				Pricing:          mo.None[model.Pricing](),
			}
			selection := model.Selection{
				Provider:        descriptor.Provider,
				Model:           descriptor.Model,
				ReasoningChoice: model.ReasoningChoiceOff,
			}
			catalog.EXPECT().
				ResolveBinding(selection).
				Return(modelexecution.CatalogBinding{
					Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
				}, nil)
			response := model.Response{
				Content:      nil,
				Outcome:      mo.Some(model.OutcomeStop),
				ErrorMessage: mo.None[string](),
				Provider: mo.Some(
					descriptor.Provider,
				),
				Model:         mo.Some(descriptor.Model),
				ResponseModel: mo.None[model.ID](),
				ResponseID:    mo.Some("successful-response"),
				Usage:         mo.Some(model.Usage{}),
				Diagnostics:   nil,
			}
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ modelexecution.ProviderRequest, handle modelexecution.StreamHandler) error {
					return handle(
						modelexecution.StreamEvent{
							Kind:     modelexecution.StreamEventDone,
							Position: mo.None[int](),
							Content:  mo.None[model.Content](),
							Delta:    mo.None[string](),
							Preview:  mo.None[model.ToolCallPreview](),
							ToolCall: mo.None[model.ToolCall](),
							Response: mo.Some(response),
						},
					)
				},
			)
			execution := modelexecution.New(
				catalog,
				modelexecution.RetryPolicy{Enabled: false, MaxRetries: 0, Delays: nil, MaxProviderDelay: 0},
				nil,
				nil,
				contexts,
			)
			var terminal agentrun.StreamEvent
			// Act through logical Stream with the captured owner's production lifetime, without granting compaction authority.
			err := execution.Stream(
				ctx,
				agentrun.ModelRequest{
					Instructions:    "",
					Model:           descriptor,
					ReasoningChoice: selection.ReasoningChoice,
					History:         nil,
					Tools:           nil,
				},
				func(event agentrun.StreamEvent) error { terminal = event; return nil },
			)
			// Assert failure classification and cause identity, response retention, scenario-specific accounting,
			// and closure of the nested-work lifetime.
			if scenario.classified {
				var failure *modelexecution.LogicalFailureError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, modelexecution.FailureExtensionFailed, failure.Category)
				require.ErrorIs(t, err, acquired)
				require.ErrorContains(t, err, persistenceUnavailableCode)
				require.Equal(t, agentrun.StreamEventError, terminal.Kind)

			} else {
				require.NoError(t, err)
				require.Equal(t, agentrun.StreamEventDone, terminal.Kind)

				select {
				case <-joined:
				default:
					require.FailNow(t, "normal observation teardown did not join its child")
				}
			}
			require.Equal(t, response, terminal.Response.MustGet())
			lateRelease, lateErr := contexts.BeginCompactionChild(owner.ExtensionID, owner.RuntimeID, lifecycleID)
			require.ErrorContains(t, lateErr, "lifetime binding is unavailable")
			require.Nil(t, lateRelease)
		})
	}
}
