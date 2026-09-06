//go:build integration

package sessions

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"

	"github.com/samber/mo"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestNextProviderRequestPreservesCompleteRestartedToolHistory verifies restart retains complete tool history and
// independently owned bytes.
func TestNextProviderRequestPreservesCompleteRestartedToolHistory(t *testing.T) {
	t.Parallel()
	// Arrange session dependencies without a filesystem adapter.
	mockController := gomock.NewController(t)
	repository := NewMockRepository(mockController)
	ids := NewMockIDGenerator(mockController)
	clock := NewMockClock(mockController)
	pricing := NewMockPricingCatalog(mockController)
	pricing.EXPECT().Pricing(gomock.Any(), gomock.Any()).Return(mo.None[model.Pricing]()).AnyTimes()

	// Arrange a resumed history containing images, refusal, reasoning, tool calls, and tool results.
	base := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	idIndex := 0
	repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	ids.EXPECT().NewID().DoAndReturn(func() (string, error) {
		idIndex++
		return fmt.Sprintf("id-%d", idIndex), nil
	}).AnyTimes()
	timeIndex := 0
	clock.EXPECT().Now().DoAndReturn(func() time.Time {
		timeIndex++
		return base.Add(time.Duration(timeIndex) * time.Second)
	}).AnyTimes()
	persisted := make([]session.Entry, 0, 4)
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			persisted = append(persisted, command.Mutation.Entry.MustGet())
			return ApplyResult{StoragePath: "/sessions/history.jsonl"}, nil
		},
	).AnyTimes()
	// Act by reconstructing the session, mutating escaped snapshots, and taking the next provider snapshot.
	active := New(repository, ids, clock, pricing, "/project")
	require.NoError(t, active.Initialize(t.Context()))

	call := model.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "input.txt"}}
	providerContext := model.ProviderContext{
		Source: model.ProviderContextSource{
			ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.Some("compatible"),
		},
		Payload: []byte{1, 2, 3},
	}
	response := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentRefusal, Text: mo.Some("refusal"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentReasoning, Text: mo.Some("reasoning"), Final: true,
				ProviderContext: mo.Some(providerContext), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(call),
			},
		},
		Outcome: mo.Some(
			model.OutcomeToolUse,
		),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.Some(model.ProviderID("provider")),
		Model:         mo.Some(model.ID("model")),
		ResponseModel: mo.Some(model.ID("response-model")),
		ResponseID:    mo.Some("response-id"),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   []model.Diagnostic{{Code: "notice", Message: "safe diagnostic"}},
	}
	result := agent.ToolResult{
		CallID: call.ID, ToolName: call.Name, IsError: false,
		Contents: []tool.ResultContent{
			{Kind: tool.ResultContentText, Text: mo.Some("contents"), Image: mo.None[tool.ResultImage]()},
			{Kind: tool.ResultContentImage, Text: mo.None[string](), Image: mo.Some(tool.ResultImage{
				MediaType: "image/png", Data: []byte{9, 8, 7},
			})},
		},
	}
	user := model.Message{Content: []model.InputContent{
		{Kind: model.InputContentText, Text: mo.Some("first"), MediaType: mo.None[string](), Data: mo.None[[]byte]()},
		{
			Kind:      model.InputContentImage,
			Text:      mo.None[string](),
			MediaType: mo.Some("image/png"),
			Data:      mo.Some([]byte{4, 5, 6}),
		},
	}}
	require.NoError(t, active.Append(t.Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(user),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}))
	require.NoError(t, active.Append(t.Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
		ToolResult: mo.None[agent.ToolResult](),
	}))
	require.NoError(t, active.Append(t.Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(result),
	}))
	escaped := active.Snapshot()
	escapedUser := escaped[0].User.MustGet()
	// Assert the next provider request retains complete ordered history with independent bytes.
	require.Len(t, escapedUser.Content, 2)
	escapedUser.Content[1].Data.MustGet()[0] = 0
	escapedModel := escaped[1].Model.MustGet()
	require.Len(t, escapedModel.Content, 3)
	escapedContext := escapedModel.Content[1].ProviderContext.MustGet()
	escapedContext.Payload[0] = 9
	escapedCall := escapedModel.Content[2].ToolCall.MustGet()
	escapedCall.Arguments["path"] = "mutated"
	escapedToolResult := escaped[2].ToolResult.MustGet()
	require.Len(t, escapedToolResult.Contents, 2)
	escapedToolResult.Contents[1].Image.MustGet().Data[0] = 0

	// restoredTree is the durable branch returned to the restarted session owner.
	restoredTree, treeErr := session.NewTree(persisted, mo.Some(persisted[len(persisted)-1].ID), nil)
	require.NoError(t, treeErr)
	repository.EXPECT().Load(gomock.Any(), session.ID("session-id")).Return(LoadedSession{
		Header: session.Header{
			Version: 1, ID: "session-id", CreatedAt: base.Add(time.Second), WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/history.jsonl", Tree: restoredTree,
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}, nil)
	restarted := New(repository, ids, clock, pricing, "/project")
	_, err := restarted.ResumeActive(t.Context(), "session-id")
	require.NoError(t, err)
	persistedUser := persisted[0].User.MustGet()
	require.Len(t, persistedUser.Content, 2)
	persistedUser.Content[1].Data.MustGet()[0] = 1
	persistedResponse := persisted[1].Model.MustGet()
	require.Len(t, persistedResponse.Content, 3)
	persistedResponse.Content[1].ProviderContext.MustGet().Payload[0] = 8
	persistedResponse.Content[2].ToolCall.MustGet().Arguments["path"] = "changed after resume"
	persistedToolResult := persisted[2].ToolResult.MustGet()
	require.Len(t, persistedToolResult.Contents, 2)
	persistedToolResult.Contents[1].Image.MustGet().Data[0] = 1

	controller := gomock.NewController(t)
	provider := agentrun.NewMockModelProvider(controller)
	runtime := agentrun.NewMockModelRuntime(controller)
	tools := agentrun.NewMockToolRuntime(controller)
	events := agentrun.NewMockEventSink(controller)
	runtime.EXPECT().Snapshot().Return(agentrun.RequestSnapshot{
		Model: model.Descriptor{
			Provider:              "provider",
			Model:                 "model",
			Input:                 nil,
			ContextWindow:         0,
			MaxTokens:             0,
			ReasoningCapabilities: model.ReasoningCapabilities{},
			ToolCapabilities:      model.ToolCapabilities{},
			Pricing:               mo.None[model.Pricing](),
		},
		ReasoningChoice: model.ReasoningChoiceOff,
		Provider:        provider,
	})
	tools.EXPECT().Tools().Return(nil)
	providerErr := errors.New("provider stopped")
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request agentrun.ModelRequest, _ agentrun.StreamHandler) error {
			require.Len(t, request.History, 4)
			storedUser := request.History[0].User.MustGet()
			require.Len(t, storedUser.Content, 2)
			assert.Equal(t, []byte{4, 5, 6}, storedUser.Content[1].Data.MustGet())
			storedModel := request.History[1].Model.MustGet()
			require.Len(t, storedModel.Content, 3)
			assert.Equal(t, "refusal", storedModel.Content[0].Text.MustGet())
			assert.Equal(t, "reasoning", storedModel.Content[1].Text.MustGet())
			assert.Equal(t, []byte{1, 2, 3}, storedModel.Content[1].ProviderContext.MustGet().Payload)
			assert.Equal(t, call, storedModel.Content[2].ToolCall.MustGet())
			assert.Equal(t, "response-id", storedModel.ResponseID.MustGet())
			assert.Equal(t, []model.Diagnostic{{Code: "notice", Message: "safe diagnostic"}}, storedModel.Diagnostics)
			assert.Equal(t, result, request.History[2].ToolResult.MustGet())
			return providerErr
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := agentrun.New("instructions", runtime, tools, events, restarted)

	_, err = service.Run(t.Context(), runcontrol.Request{RunID: "next", UserText: "second"})
	require.ErrorIs(t, err, providerErr)
}
