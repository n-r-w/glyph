//go:build !integration

package run

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/hooks"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestServiceRunTransformsRequestLocalContext verifies sequential context replacement without history mutation.
func TestServiceRunTransformsRequestLocalContext(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	tools.EXPECT().Tools().Return(nil)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	contextSeen := ""
	hookRunner := hookrunner.New([]hooks.ContextHandler{
		func(_ context.Context, value hooks.Context) (hooks.Context, error) {
			value.History[0].User.OrEmpty().Content[0].Text = mo.Some("first transformation")
			return value, nil
		},
		func(_ context.Context, value hooks.Context) (hooks.Context, error) {
			contextSeen = value.History[0].User.OrEmpty().Content[0].Text.OrEmpty()
			value.History[0].User.OrEmpty().Content[0].Text = mo.Some("final transformation")
			return value, nil
		},
	}, nil, nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, handle StreamHandler) error {
			assert.Equal(t, "final transformation", request.History[0].User.OrEmpty().Content[0].Text.OrEmpty())
			return handle(
				StreamEvent{
					Kind: StreamEventDone,
					Response: mo.Some(
						model.Response{
							Outcome:       mo.Some(model.OutcomeStop),
							Content:       nil,
							ErrorMessage:  mo.None[string](),
							Provider:      mo.None[model.ProviderID](),
							Model:         mo.None[model.ID](),
							ResponseModel: mo.None[model.ID](),
							ResponseID:    mo.None[string](),
							Usage:         mo.None[model.Usage](),
							Diagnostics:   nil,
						},
					),
					Position: mo.None[int](),
					Content:  mo.None[model.Content](),
					Delta:    mo.None[string](),
					Preview:  mo.None[model.ToolCallPreview](),
					ToolCall: mo.None[model.ToolCall](),
				},
			)
		},
	)
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		hookRunner,
		tools,
		events,
	)

	result, err := service.Run(t.Context(), Request{RunID: "context-success", UserText: "persisted input"})

	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	assert.Equal(t, "first transformation", contextSeen)
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, "persisted input", history[0].User.OrEmpty().Content[0].Text.OrEmpty())
}

// TestServiceRunStopsOnContextHookFailure verifies context hook causes reach terminal results and history.
func TestServiceRunStopsOnContextHookFailure(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	laterCalls := 0
	hookErr := errors.New("unique context hook error")
	hookRunner := hookrunner.New([]hooks.ContextHandler{
		func(_ context.Context, value hooks.Context) (hooks.Context, error) {
			value.History[0].User.OrEmpty().Content[0].Text = mo.Some("secret transformed context")
			return hooks.Context{}, hookErr
		},
		func(_ context.Context, value hooks.Context) (hooks.Context, error) {
			laterCalls++
			return value, nil
		},
	}, nil, nil)
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		hookRunner,
		tools,
		events,
	)

	result, err := service.Run(t.Context(), Request{RunID: "context-failure", UserText: "persisted input"})

	require.Error(t, err)
	require.ErrorIs(t, err, hookErr)
	assert.Contains(t, err.Error(), hookErr.Error())
	assert.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	assert.Contains(t, result.ErrorMessage.OrEmpty(), hookErr.Error())
	assert.Zero(t, laterCalls)
	history := service.History()
	require.Len(t, history, 2)
	assert.Equal(t, "persisted input", history[0].User.OrEmpty().Content[0].Text.OrEmpty())
	assert.Equal(t, model.OutcomeFailed, history[1].Model.OrEmpty().Outcome.OrEmpty())
	assert.Contains(t, history[1].Model.OrEmpty().ErrorMessage.OrEmpty(), hookErr.Error())
	assert.Equal(
		t,
		[]model.Diagnostic{{Code: "internal_hook_failed", Message: "context"}},
		history[1].Model.OrEmpty().Diagnostics,
	)
	assert.Equal(t, []agent.HistoryEntry{history[0]}, service.ProjectHistory())
}

// TestCloneToolResultClonesImageBytesInsideOption verifies history snapshots do not share mutable image data.
func TestCloneToolResultClonesImageBytesInsideOption(t *testing.T) {
	t.Parallel()

	original := agent.ToolResult{
		Contents: []tool.ResultContent{{
			Kind:  tool.ResultContentImage,
			Text:  mo.None[string](),
			Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2, 3}}),
		}}, CallID: "", ToolName: "", IsError: false,
	}
	cloned := original.Clone()
	image, ok := cloned.Contents[0].Image.Get()
	require.True(t, ok)
	image.Data[0] = 9

	assert.Equal(t, byte(1), original.Contents[0].Image.OrEmpty().Data[0])
}
