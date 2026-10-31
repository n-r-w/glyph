//go:build integration

package middleware

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// recordingHistory returns a history store mock that keeps appended entries in memory.
func recordingHistory(ctrl *gomock.Controller) (*agentrun.MockHistoryStore, *[]agent.HistoryEntry) {
	history := make([]agent.HistoryEntry, 0)
	store := agentrun.NewMockHistoryStore(ctrl)
	store.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry {
		return agent.CloneHistory(history)
	}).AnyTimes()
	store.EXPECT().
		Append(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, entry agent.HistoryEntry) error {
			history = append(history, entry.Clone())
			return nil
		}).
		AnyTimes()
	return store, &history
}

// TestCoreFinalizationCancellationBoundary verifies with Agent Core, the real owner and a preserve-only handler that
// cancellation before the owner admits the handler records the unchanged response, cancellation while the handler
// runs records none, and cancellation after the handler succeeds records the final value.
func TestCoreFinalizationCancellationBoundary(t *testing.T) {
	t.Parallel()
	response := model.Response{
		Content: []model.Content{{
			Kind: model.ContentText, Text: mo.Some("answer"), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
		}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
		Provider: mo.Some[model.ProviderID]("provider"), Model: mo.Some[model.ID]("configured"),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](),
		Diagnostics: nil,
	}
	for name, test := range map[string]struct {
		// cancelInProvider makes the provider cancel the run before it returns its terminal response.
		cancelInProvider bool
		// handle is the behavior of the preserve-only handler; nil means that it must not run.
		handle func(ctx context.Context, cancel context.CancelFunc) (Transformed[agent.HistoryEntry], error)
		// recorded reports that the model response must be recorded.
		recorded bool
	}{
		"canceled before admission": {cancelInProvider: true, handle: nil, recorded: true},
		"canceled while the handler runs": {
			cancelInProvider: false, recorded: false,
			handle: func(ctx context.Context, cancel context.CancelFunc) (Transformed[agent.HistoryEntry], error) {
				cancel()
				return Transformed[agent.HistoryEntry]{}, ctx.Err()
			},
		},
		"canceled after the handler succeeds": {
			cancelInProvider: false, recorded: true,
			handle: func(_ context.Context, cancel context.CancelFunc) (Transformed[agent.HistoryEntry], error) {
				cancel()
				return Transformed[agent.HistoryEntry]{Kind: Preserve, Replacement: agent.HistoryEntry{}}, nil
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange Agent Core with the real owner, one preserve-only handler, and a provider with one response.
			ctrl := gomock.NewController(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			runtime := NewMockRuntime(ctrl)
			contexts := NewMockContextIssuer(ctrl)
			owner := New(runtime, contexts, nil)
			owner.CommitMiddlewareHandlers([]startup.AcceptedRegistration{{
				ID: "extension", Path: "", Tools: nil,
				Handlers: []startup.AcceptedHandler{{ID: "preserve", Kind: startup.RawHandlerKindFinalizedMessage}},
			}})
			if test.handle != nil {
				contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil)
				runtime.EXPECT().
					HandleFinalizedMessage(gomock.Any(), "extension", "preserve", gomock.Any()).
					DoAndReturn(
						func(
							ctx context.Context, _, _ string, _ Transformation[agent.HistoryEntry],
						) (Transformed[agent.HistoryEntry], error) {
							return test.handle(ctx, cancel)
						},
					)
			}
			provider := agentrun.NewMockModelProvider(ctrl)
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ agentrun.ModelRequest, handle agentrun.StreamHandler) error {
					if test.cancelInProvider {
						defer cancel()
					}
					return handle(agentrun.StreamEvent{
						Kind: agentrun.StreamEventDone, Response: mo.Some(response), Position: mo.None[int](),
						Content: mo.None[model.Content](), Delta: mo.None[string](),
						Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](),
					})
				},
			)
			contextPort := agentrun.NewMockEffectiveContextPort(ctrl)
			contextPort.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, request agentrun.ContextRequest) (agentrun.EffectiveContext, error) {
					return agentrun.EffectiveContext{Request: agentrun.ModelRequest{
						Instructions: request.Instructions, Model: model.Descriptor{
							Provider:              "provider",
							Model:                 "model",
							Input:                 nil,
							ContextWindow:         1,
							MaxTokens:             1,
							ReasoningCapabilities: model.ReasoningCapabilities{},
							ToolCapabilities:      model.ToolCapabilities{},
							Pricing:               mo.None[model.Pricing](),
						},
						ReasoningChoice: model.ReasoningChoiceOff, History: request.History, Tools: request.Tools,
					}, Provider: provider}, nil
				},
			)
			tools := agentrun.NewMockToolRuntime(ctrl)
			tools.EXPECT().Tools().Return(nil)
			events := agentrun.NewMockEventSink(ctrl)
			events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			store, history := recordingHistory(ctrl)
			core := agentrun.New("instructions", contextPort, tools, events, store, owner)

			// Act through one run.
			_, err := core.Run(ctx, runcontrol.Request{RunID: "run", Message: model.TextMessage("hello")})

			// Assert whether the unchanged model response was recorded after the user message.
			if !test.recorded {
				require.ErrorIs(t, err, context.Canceled)
				assert.Len(t, *history, 1)
				return
			}
			require.Len(t, *history, 2)
			assert.Equal(t, mo.Some(response), (*history)[1].Model)
		})
	}
}
