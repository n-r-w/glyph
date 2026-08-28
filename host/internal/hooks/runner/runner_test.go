package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/hooks"
)

// TestRunnerAppliesSequentialCopiedValues verifies ordered transformations and copy isolation.
func TestRunnerAppliesSequentialCopiedValues(t *testing.T) {
	t.Parallel()

	contextSeen := ""
	requestSeen := ""
	responseSeen := ""
	runner := New(
		[]hooks.ContextHandler{
			func(_ context.Context, value hooks.Context) (hooks.Context, error) {
				message := value.History[0].User.OrEmpty()
				message.Content[0].Text = mo.Some("first-context")
				value.History[0].User = mo.Some(message)
				return value, nil
			},
			func(_ context.Context, value hooks.Context) (hooks.Context, error) {
				message := value.History[0].User.OrEmpty()
				contextSeen = message.Content[0].Text.OrEmpty()
				message.Content[0].Text = mo.Some("final-context")
				value.History[0].User = mo.Some(message)
				return value, nil
			},
		},
		[]hooks.RequestHandler{
			func(_ context.Context, value hooks.Request) (hooks.Request, error) {
				value.Payload[0] = '1'
				value.Headers["X-Test"][0] = "first-request"
				return value, nil
			},
			func(_ context.Context, value hooks.Request) (hooks.Request, error) {
				requestSeen = string(value.Payload) + ":" + value.Headers["X-Test"][0]
				value.Payload[0] = '2'
				value.Headers["X-Test"][0] = "final-request"
				return value, nil
			},
		},
		[]hooks.ResponseHandler{
			func(_ context.Context, value hooks.Response) error {
				value.Headers["X-Test"][0] = "mutated-copy"
				return nil
			},
			func(_ context.Context, value hooks.Response) error {
				responseSeen = value.Headers["X-Test"][0]
				return nil
			},
		},
	)
	originalContext := hooks.Context{History: []agent.HistoryEntry{{
		Kind:  agent.HistoryEntryUser,
		User:  mo.Some(model.Message{Content: []model.InputContent{{Kind: model.InputContentText, Text: mo.Some("original-context"), MediaType: mo.None[string](), Data: mo.None[[]byte]()}}}),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}}}
	originalRequest := hooks.Request{
		Provider: "provider", Model: "model", Payload: []byte("abc"),
		Headers: hooks.Header{"X-Test": {"original-request"}},
	}
	originalResponse := hooks.Response{
		Provider: "provider", Model: "model", Status: 200,
		Headers: hooks.Header{"X-Test": {"original-response"}},
	}

	transformedContext, err := runner.TransformContext(t.Context(), originalContext)
	require.NoError(t, err)
	transformedRequest, err := runner.TransformRequest(t.Context(), originalRequest)
	require.NoError(t, err)
	require.NoError(t, runner.ObserveResponse(t.Context(), originalResponse))

	assert.Equal(t, "first-context", contextSeen)
	assert.Equal(t, "final-context", transformedContext.History[0].User.OrEmpty().Content[0].Text.OrEmpty())
	assert.Equal(t, "original-context", originalContext.History[0].User.OrEmpty().Content[0].Text.OrEmpty())
	assert.Equal(t, "1bc:first-request", requestSeen)
	assert.Equal(t, "2bc", string(transformedRequest.Payload))
	assert.Equal(t, "final-request", transformedRequest.Headers["X-Test"][0])
	assert.Equal(t, "abc", string(originalRequest.Payload))
	assert.Equal(t, "original-request", originalRequest.Headers["X-Test"][0])
	assert.Equal(t, "original-response", responseSeen)
	assert.Equal(t, "original-response", originalResponse.Headers["X-Test"][0])
}

// TestRunnerStopsAtFirstError verifies stage failures retain handler causes and stop later handlers.
func TestRunnerStopsAtFirstError(t *testing.T) {
	t.Parallel()

	rawErr := errors.New("secret hook failure")
	tests := []struct {
		name  string
		stage hooks.Stage
		run   func(*Runner) error
	}{
		{
			name: "context", stage: hooks.StageContext,
			run: func(runner *Runner) error {
				_, err := runner.TransformContext(t.Context(), hooks.Context{})
				return err
			},
		},
		{
			name: "request", stage: hooks.StageRequest,
			run: func(runner *Runner) error {
				_, err := runner.TransformRequest(t.Context(), hooks.Request{})
				return err
			},
		},
		{
			name: "response", stage: hooks.StageResponse,
			run: func(runner *Runner) error {
				return runner.ObserveResponse(t.Context(), hooks.Response{})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			laterCalls := 0
			runner := New(
				[]hooks.ContextHandler{
					func(context.Context, hooks.Context) (hooks.Context, error) { return hooks.Context{}, rawErr },
					func(_ context.Context, value hooks.Context) (hooks.Context, error) { laterCalls++; return value, nil },
				},
				[]hooks.RequestHandler{
					func(context.Context, hooks.Request) (hooks.Request, error) { return hooks.Request{}, rawErr },
					func(_ context.Context, value hooks.Request) (hooks.Request, error) { laterCalls++; return value, nil },
				},
				[]hooks.ResponseHandler{
					func(context.Context, hooks.Response) error { return rawErr },
					func(context.Context, hooks.Response) error { laterCalls++; return nil },
				},
			)

			err := test.run(runner)

			var failure hooks.HookError
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, test.stage, failure.Stage)
			require.ErrorIs(t, err, rawErr)
			assert.Contains(t, err.Error(), rawErr.Error())
			assert.Zero(t, laterCalls)
		})
	}
}

// TestRunnerClonesHistoryOptions verifies hook context copies isolate mutable history payloads.
func TestRunnerClonesHistoryOptions(t *testing.T) {
	t.Parallel()

	original := hooks.Context{History: []agent.HistoryEntry{{
		Kind: agent.HistoryEntryModel,
		Model: mo.Some(model.Response{Content: []model.Content{
			{
				Kind: model.ContentReasoning, Text: mo.Some("reason"),
				ProviderContext: mo.Some(model.ProviderContext{Payload: []byte{1, 2, 3}, Source: model.ProviderContextSource{}}), Final: false, ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind:     model.ContentToolCall,
				ToolCall: mo.Some(model.ToolCall{Arguments: map[string]any{"items": []any{"first"}}, ID: "", Name: ""}), Text: mo.None[string](), Final: false, ProviderContext: mo.None[model.ProviderContext](),
			},
		}, Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
		}), User: mo.None[model.Message](), ToolResult: mo.None[agent.ToolResult](),
	}, {
		Kind: agent.HistoryEntryToolResult,
		User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(agent.ToolResult{
			CallID: "call", ToolName: "tool",
			Contents: []tool.ResultContent{{
				Kind: tool.ResultContentImage, Text: mo.None[string](),
				Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{4, 5, 6}}),
			}}, IsError: false,
		}),
	}}}
	runner := New([]hooks.ContextHandler{
		func(_ context.Context, value hooks.Context) (hooks.Context, error) {
			providerContext := value.History[0].Model.OrEmpty().Content[0].ProviderContext.OrEmpty()
			providerContext.Payload[0] = 9
			call := value.History[0].Model.OrEmpty().Content[1].ToolCall.OrEmpty()
			call.Arguments["items"].([]any)[0] = "changed"
			image := value.History[1].ToolResult.OrEmpty().Contents[0].Image.OrEmpty()
			image.Data[0] = 9
			return value, nil
		},
	}, nil, nil)

	_, err := runner.TransformContext(t.Context(), original)
	require.NoError(t, err)
	assert.Equal(t, byte(1), original.History[0].Model.OrEmpty().Content[0].ProviderContext.OrEmpty().Payload[0])
	assert.Equal(t, "first", original.History[0].Model.OrEmpty().Content[1].ToolCall.OrEmpty().Arguments["items"].([]any)[0])
	assert.Equal(t, byte(4), original.History[1].ToolResult.OrEmpty().Contents[0].Image.OrEmpty().Data[0])
}
