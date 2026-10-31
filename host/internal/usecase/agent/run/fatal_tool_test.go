//go:build !integration

package run

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestConsumedChildFailureStopsLaterTools verifies fatal precedence and stops later tool and provider calls.
func TestConsumedChildFailureStopsLaterTools(t *testing.T) {
	t.Parallel()
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "failure-with-cancellation"}[canceled], func(t *testing.T) {
			t.Parallel()
			// Arrange a provider response containing two calls and one independent fatal failure.
			mocks := gomock.NewController(t)
			provider := NewMockModelProvider(mocks)
			tools := NewMockToolRuntime(mocks)
			events := NewMockEventSink(mocks)
			first := model.ToolCall{ID: "first", Name: "first", Arguments: testToolCallArguments(`{}`)}
			second := model.ToolCall{ID: "second", Name: "second", Arguments: testToolCallArguments(`{}`)}
			response := model.Response{
				Content: []model.Content{testCallItem(first), testCallItem(second)},
				Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
				Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
				Usage: mo.None[model.Usage](), Diagnostics: nil,
			}
			cause := NewMockFatalOperationFailure(mocks)
			cause.EXPECT().Error().Return("nested handler: original independent cause").AnyTimes()
			tools.EXPECT().Tools().Return(nil)
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, nil))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			tools.EXPECT().Execute(gomock.Any(), first, gomock.Any()).DoAndReturn(
				func(context.Context, model.ToolCall, tool.ProgressHandler) (agent.ToolResult, error) {
					if canceled {
						cancel()
						return agent.ToolResult{}, errors.Join(cause, context.Canceled)
					}
					return agent.ToolResult{}, cause
				},
			)
			events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			service := newTestService(
				t,
				testInstructions,
				testModelDescriptor,
				model.ReasoningChoiceHigh,
				provider,
				tools,
				events,
			)
			// Act through Core's public run entry point.
			result, err := service.Run(ctx, runcontrol.Request{RunID: "fatal-tool", Message: model.TextMessage("go")})
			// Assert failure preserves the original cause and never starts another tool or provider request.
			require.ErrorIs(t, err, cause)
			require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
		})
	}
}
