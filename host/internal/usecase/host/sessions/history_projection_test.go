//go:build !integration

package sessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samber/mo"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/hooks/runner"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestNextProviderRequestPreservesCompleteRestartedToolHistory verifies restart retains complete tool history and
// independently owned bytes.
func (s *ServiceSuite) TestNextProviderRequestPreservesCompleteRestartedToolHistory() {
	// Arrange a resumed history containing images, refusal, reasoning, tool calls, and tool results.
	base := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	idIndex := 0
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().DoAndReturn(func() (string, error) {
		idIndex++
		return fmt.Sprintf("id-%d", idIndex), nil
	}).AnyTimes()
	timeIndex := 0
	s.clock.EXPECT().Now().DoAndReturn(func() time.Time {
		timeIndex++
		return base.Add(time.Duration(timeIndex) * time.Second)
	}).AnyTimes()
	persisted := make([]session.Entry, 0, 4)
	s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			persisted = append(persisted, command.Mutation.Entry.MustGet())
			return ApplyResult{StoragePath: "/sessions/history.jsonl"}, nil
		},
	).AnyTimes()
	// Act by reconstructing the session, mutating escaped snapshots, and taking the next provider snapshot.
	active := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(active.Initialize(s.T().Context()))

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
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(user),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}))
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
		ToolResult: mo.None[agent.ToolResult](),
	}))
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(result),
	}))
	escaped := active.Snapshot()
	escapedUser := escaped[0].User.MustGet()
	// Assert the next provider request retains complete ordered history with independent bytes.
	s.Require().Len(escapedUser.Content, 2)
	escapedUser.Content[1].Data.MustGet()[0] = 0
	escapedModel := escaped[1].Model.MustGet()
	s.Require().Len(escapedModel.Content, 3)
	escapedContext := escapedModel.Content[1].ProviderContext.MustGet()
	escapedContext.Payload[0] = 9
	escapedCall := escapedModel.Content[2].ToolCall.MustGet()
	escapedCall.Arguments["path"] = "mutated"
	escapedToolResult := escaped[2].ToolResult.MustGet()
	s.Require().Len(escapedToolResult.Contents, 2)
	escapedToolResult.Contents[1].Image.MustGet().Data[0] = 0

	s.repository.EXPECT().Load(gomock.Any(), session.ID("session-id")).Return(LoadedSession{
		Header: session.Header{
			Version: 1, ID: "session-id", CreatedAt: base.Add(time.Second), WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/history.jsonl", Tree: mustSessionTree(persisted),
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
	}, nil)
	restarted := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	_, err := restarted.ResumeActive(s.T().Context(), "session-id")
	s.Require().NoError(err)
	persistedUser := persisted[0].User.MustGet()
	s.Require().Len(persistedUser.Content, 2)
	persistedUser.Content[1].Data.MustGet()[0] = 1
	persistedResponse := persisted[1].Model.MustGet()
	s.Require().Len(persistedResponse.Content, 3)
	persistedResponse.Content[1].ProviderContext.MustGet().Payload[0] = 8
	persistedResponse.Content[2].ToolCall.MustGet().Arguments["path"] = "changed after resume"
	persistedToolResult := persisted[2].ToolResult.MustGet()
	s.Require().Len(persistedToolResult.Contents, 2)
	persistedToolResult.Contents[1].Image.MustGet().Data[0] = 1

	controller := gomock.NewController(s.T())
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
			s.Require().Len(request.History, 4)
			storedUser := request.History[0].User.MustGet()
			s.Require().Len(storedUser.Content, 2)
			s.Equal([]byte{4, 5, 6}, storedUser.Content[1].Data.MustGet())
			storedModel := request.History[1].Model.MustGet()
			s.Require().Len(storedModel.Content, 3)
			s.Equal("refusal", storedModel.Content[0].Text.MustGet())
			s.Equal("reasoning", storedModel.Content[1].Text.MustGet())
			s.Equal([]byte{1, 2, 3}, storedModel.Content[1].ProviderContext.MustGet().Payload)
			s.Equal(call, storedModel.Content[2].ToolCall.MustGet())
			s.Equal("response-id", storedModel.ResponseID.MustGet())
			s.Equal([]model.Diagnostic{{Code: "notice", Message: "safe diagnostic"}}, storedModel.Diagnostics)
			s.Equal(result, request.History[2].ToolResult.MustGet())
			return providerErr
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := agentrun.New("instructions", runtime, runner.New(nil, nil, nil), tools, events, restarted)

	_, err = service.Run(s.T().Context(), agentrun.Request{RunID: "next", UserText: "second"})
	s.Require().ErrorIs(err, providerErr)
}
