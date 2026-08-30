package sessions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"time"

	"github.com/samber/mo"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"

	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestHistoryAppendPersistsTextBeforePublishingImmutableSnapshot verifies complete user and model entries become durable before publication.
func (s *ServiceSuite) TestHistoryAppendPersistsTextBeforePublishingImmutableSnapshot() {
	// Arrange persistence expectations for complete user and model history entries.
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	userAt := createdAt.Add(time.Minute)
	modelAt := createdAt.Add(2 * time.Minute)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	persisted := make([]session.Entry, 0, 2)
	gomock.InOrder(
		s.ids.EXPECT().NewID().Return("user-entry", nil),
		s.clock.EXPECT().Now().Return(userAt),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
				s.Equal("hello", command.Mutation.Entry.MustGet().User.MustGet().Content[0].Text.MustGet())
				persisted = append(persisted, command.Mutation.Entry.MustGet())
				return ApplyResult{StoragePath: "/sessions/file.jsonl"}, nil
			},
		),
		s.ids.EXPECT().NewID().Return("model-entry", nil),
		s.clock.EXPECT().Now().Return(modelAt),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
				stored := command.Mutation.Entry.MustGet().Model.MustGet()
				s.Require().Len(stored.Content, 3)
				s.Equal(model.ContentText, stored.Content[0].Kind)
				s.Equal("world", stored.Content[0].Text.MustGet())
				s.Equal(model.ContentRefusal, stored.Content[1].Kind)
				s.Equal("refusal", stored.Content[1].Text.MustGet())
				s.Equal(model.ContentReasoning, stored.Content[2].Kind)
				s.Equal("reasoning", stored.Content[2].Text.MustGet())
				s.Equal([]byte{1, 2, 3}, stored.Content[2].ProviderContext.MustGet().Payload)
				s.Equal(mo.Some(model.OutcomeStop), stored.Outcome)
				s.Equal(mo.Some("safe terminal message"), stored.ErrorMessage)
				s.Equal(mo.Some("response-id"), stored.ResponseID)
				persisted = append(persisted, command.Mutation.Entry.MustGet())
				return ApplyResult{StoragePath: "/sessions/file.jsonl"}, nil
			},
		),
	)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))

	// Act by appending user and model entries before reading snapshots.
	s.Require().NoError(service.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("hello")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}))
	response := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentText, Text: mo.Some("world"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentRefusal, Text: mo.Some("refusal"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentReasoning, Text: mo.Some("reasoning"), Final: true,
				ProviderContext: mo.Some(model.ProviderContext{
					Source: model.ProviderContextSource{
						ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.Some("key"),
					},
					Payload: []byte{1, 2, 3},
				}),
				ToolCall: mo.None[model.ToolCall](),
			},
		},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.Some("safe terminal message"),
		Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
		ResponseModel: mo.Some(model.ID("response-model")), ResponseID: mo.Some("response-id"),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	s.Require().NoError(service.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
		Model: mo.Some(response), ToolResult: mo.None[agent.ToolResult](),
	}))

	// Assert durable entries publish complete, independently owned provider history.
	history := service.Snapshot()
	s.Require().Len(history, 2)
	s.Equal("hello", history[0].User.MustGet().Content[0].Text.MustGet())
	s.Require().Len(history[1].Model.MustGet().Content, 3)
	s.Equal("response-id", history[1].Model.MustGet().ResponseID.MustGet())
	history[0].User.MustGet().Content[0].Text = mo.Some("mutated")
	s.Equal("hello", service.Snapshot()[0].User.MustGet().Content[0].Text.MustGet())

	s.repository.EXPECT().List(gomock.Any()).Return([]LoadedSession{
		{
			Header: session.Header{
				Version:          1,
				ID:               "session-id",
				CreatedAt:        createdAt,
				WorkingDirectory: "/project",
			},
			StoragePath:          "/sessions/file.jsonl",
			Information:          mo.None[session.Information](),
			InformationUpdatedAt: mo.None[time.Time](),
			Tree:                 mustSessionTree(persisted),
		},
	}, nil)
	listed, err := service.ListStored(s.T().Context())
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
	s.Equal(mo.Some("hello"), listed[0].FirstUserText)
	s.Equal(2, listed[0].TotalMessages)
}

// TestTerminalModelAndToolResultBecomeDurableBeforeSnapshotPublication verifies terminal history is durable and independently owned.
func (s *ServiceSuite) TestTerminalModelAndToolResultBecomeDurableBeforeSnapshotPublication() {
	// Arrange terminal model and tool-result entries with ordered persistence expectations.
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	modelAt := createdAt.Add(time.Second)
	toolAt := createdAt.Add(2 * time.Second)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	active := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(active.Initialize(s.T().Context()))

	call := model.ToolCall{ID: "call", Name: "read", Arguments: map[string]any{"path": "input.txt"}}
	providerContext := model.ProviderContext{
		Source: model.ProviderContextSource{
			ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.Some("key"),
		},
		Payload: []byte{1, 2, 3},
	}
	response := model.Response{
		Content: []model.Content{{
			Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
			ProviderContext: mo.Some(providerContext), ToolCall: mo.Some(call),
		}},
		Outcome: mo.Some(
			model.OutcomeToolUse,
		),
		ErrorMessage: mo.None[string](),
		Provider:     mo.Some(model.ProviderID("provider")),
		Model: mo.Some(
			model.ID("model"),
		),
		ResponseModel: mo.Some(model.ID("response-model")),
		ResponseID:    mo.Some("response-id"),
		Usage: mo.Some(model.Usage{
			InputTokens: 1, OutputTokens: 2, CachedInputTokens: 3,
			CacheWriteTokens: 4, ReasoningTokens: 1, TotalTokens: 10,
		}),
		Diagnostics: nil,
	}
	result := agent.ToolResult{
		CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents("result"), IsError: false,
	}
	gomock.InOrder(
		s.ids.EXPECT().NewID().Return("model-entry", nil),
		s.clock.EXPECT().Now().Return(modelAt),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
				s.Equal(response, command.Mutation.Entry.MustGet().Model.MustGet())
				return ApplyResult{StoragePath: "/sessions/history.jsonl"}, nil
			},
		),
		s.ids.EXPECT().NewID().Return("tool-entry", nil),
		s.clock.EXPECT().Now().Return(toolAt),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
				s.Equal(result, command.Mutation.Entry.MustGet().ToolResult.MustGet())
				return ApplyResult{StoragePath: "/sessions/history.jsonl"}, nil
			},
		),
	)

	// Act by appending the terminal model response and its tool result.
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
		ToolResult: mo.None[agent.ToolResult](),
	}))
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(result),
	}))

	// Assert durable entries precede publication and escaped snapshots cannot mutate active state.
	entries := active.ActiveEntries()
	s.Require().Len(entries, 2)
	s.Equal(response, entries[0].Model.MustGet())
	s.Equal(result, entries[1].ToolResult.MustGet())
	history := active.Snapshot()
	s.Require().Len(history, 2)
	escapedResponse := history[0].Model.MustGet()
	escapedResponse.Content[0].ProviderContext.MustGet().Payload[0] = 9
	escapedResponse.Content[0].ToolCall.MustGet().Arguments["path"] = "mutated"
	escapedResult := history[1].ToolResult.MustGet()
	escapedResult.Contents[0].Text = mo.Some("mutated")
	s.Equal([]byte{1, 2, 3}, active.Snapshot()[0].Model.MustGet().Content[0].ProviderContext.MustGet().Payload)
	s.Equal("input.txt", active.Snapshot()[0].Model.MustGet().Content[0].ToolCall.MustGet().Arguments["path"])
	s.Equal("result", active.Snapshot()[1].ToolResult.MustGet().Contents[0].Text.MustGet())
}

// TestTerminalModelProjectionPreservesContentSliceStateAndOrder verifies nil, empty, and ordered content remain distinct.
func (s *ServiceSuite) TestTerminalModelProjectionPreservesContentSliceStateAndOrder() {
	// Arrange terminal responses for each supported content slice state.
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	active := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(active.Initialize(s.T().Context()))

	ordered := []model.Content{
		{
			Kind: model.ContentText, Text: mo.Some("before tool"), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
		},
		{
			Kind: model.ContentReasoning, Text: mo.None[string](), Final: true,
			ProviderContext: mo.Some(model.ProviderContext{
				Source: model.ProviderContextSource{
					ProviderID: "provider", API: "responses", Model: "model",
					CompatibilityKey: mo.Some("key"),
				},
				Payload: []byte{1, 2, 3},
			}),
			ToolCall: mo.None[model.ToolCall](),
		},
		{
			Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(model.ToolCall{
				ID: "call", Name: "read", Arguments: map[string]any{"path": "input.txt"},
			}),
		},
	}
	cases := []struct {
		name    string
		content []model.Content
		outcome model.Outcome
	}{
		{name: "nil", content: nil, outcome: model.OutcomeFailed},
		{name: "non-nil empty", content: []model.Content{}, outcome: model.OutcomeFailed},
		{name: "ordered supported continuation", content: ordered, outcome: model.OutcomeToolUse},
	}
	for index := range cases {
		test := cases[index]
		entryID := fmt.Sprintf("model-entry-%d", index)
		entryTime := createdAt.Add(time.Duration(index+1) * time.Second)
		gomock.InOrder(
			s.ids.EXPECT().NewID().Return(entryID, nil),
			s.clock.EXPECT().Now().Return(entryTime),
			s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
					actual := command.Mutation.Entry.MustGet().Model.MustGet().Content
					s.Equal(test.content == nil, actual == nil)
					s.Equal(test.content, actual)
					return ApplyResult{StoragePath: "/sessions/history.jsonl"}, nil
				},
			),
		)
	}

	// Act by appending each terminal model response.
	for index := range cases {
		test := cases[index]
		response := model.Response{
			Content: test.content, Outcome: mo.Some(test.outcome), ErrorMessage: mo.Some("safe terminal result"),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
			ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
		}
		s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
			Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
			ToolResult: mo.None[agent.ToolResult](),
		}))

		// Assert durable and provider-history projections preserve state and order.
		durableContent := active.ActiveEntries()[index].Model.MustGet().Content
		s.Equal(test.content == nil, durableContent == nil)
		s.Equal(test.content, durableContent)
		snapshotContent := active.Snapshot()[index].Model.MustGet().Content
		s.Equal(test.content == nil, snapshotContent == nil)
		s.Equal(test.content, snapshotContent)
	}
}

// TestToolResultAppendFailureKeepsDurableAndProviderHistoryUnchanged verifies failed durability prevents tool-result publication.
func (s *ServiceSuite) TestToolResultAppendFailureKeepsDurableAndProviderHistoryUnchanged() {
	// Arrange an initialized session and a tool-result append that fails during sync.
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	active := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(active.Initialize(s.T().Context()))
	result := agent.ToolResult{
		CallID: "call", ToolName: "read", Contents: tool.TextContents("completed effect"), IsError: false,
	}
	gomock.InOrder(
		s.ids.EXPECT().NewID().Return("tool-entry", nil),
		s.clock.EXPECT().Now().Return(createdAt.Add(time.Second)),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(ApplyResult{}, errors.New("sync failed")),
	)

	// Act by appending the terminal tool result.
	err := active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(result),
	})
	// Assert the Agent Core boundary keeps one persistence classification and the storage cause.
	s.Require().ErrorIs(err, agentrun.ErrPersistenceUnavailable)
	s.Require().ErrorContains(err, "sync failed")
	s.Equal(1, strings.Count(err.Error(), agentrun.ErrPersistenceUnavailable.Error()))
	s.Empty(active.ActiveEntries())
	s.Empty(active.Snapshot())
}

// TestImageOnlyToolResultBecomesDurable verifies an image-only result is persisted and retained in snapshots.
func (s *ServiceSuite) TestImageOnlyToolResultBecomesDurable() {
	// Arrange an active session and an image-only terminal tool result.
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	active := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(active.Initialize(s.T().Context()))
	result := agent.ToolResult{
		CallID: "call", ToolName: "read",
		Contents: []tool.ResultContent{{
			Kind: tool.ResultContentImage, Text: mo.None[string](),
			Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2, 3}}),
		}},
		IsError: false,
	}

	gomock.InOrder(
		s.ids.EXPECT().NewID().Return("tool-entry", nil),
		s.clock.EXPECT().Now().Return(createdAt.Add(time.Second)),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
				s.Equal(result, command.Mutation.Entry.MustGet().ToolResult.MustGet())
				return ApplyResult{StoragePath: "/sessions/history.jsonl"}, nil
			},
		),
	)

	// Act by appending the terminal result to active history.
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(result),
	}))

	// Assert the durable entry and provider snapshot retain the complete image result.
	s.Require().Len(active.ActiveEntries(), 1)
	s.Equal(result, active.ActiveEntries()[0].ToolResult.MustGet())
	s.Require().Len(active.Snapshot(), 1)
	s.Equal(result, active.Snapshot()[0].ToolResult.MustGet())
}

// TestTerminalToolResultProjectionPreservesContentsSliceState verifies nil and empty result slices remain distinct.
func (s *ServiceSuite) TestTerminalToolResultProjectionPreservesContentsSliceState() {
	// Arrange repository callbacks for nil and present-empty tool-result content slices.
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	active := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(active.Initialize(s.T().Context()))

	gomock.InOrder(
		s.ids.EXPECT().NewID().Return("nil-entry", nil),
		s.clock.EXPECT().Now().Return(createdAt.Add(time.Second)),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
				contents := command.Mutation.Entry.MustGet().ToolResult.MustGet().Contents
				s.Nil(contents)
				return ApplyResult{StoragePath: "/sessions/history.jsonl"}, nil
			},
		),
		s.ids.EXPECT().NewID().Return("empty-entry", nil),
		s.clock.EXPECT().Now().Return(createdAt.Add(2*time.Second)),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
				contents := command.Mutation.Entry.MustGet().ToolResult.MustGet().Contents
				s.NotNil(contents)
				s.Empty(contents)
				return ApplyResult{StoragePath: "/sessions/history.jsonl"}, nil
			},
		),
	)
	// Act by appending terminal tool results with both slice states.
	for _, contents := range [][]tool.ResultContent{nil, {}} {
		s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
			Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
			ToolResult: mo.Some(agent.ToolResult{
				CallID: "call", ToolName: "tool", Contents: contents, IsError: false,
			}),
		}))
	}

	// Assert both durable projections completed and became active entries.
	s.Len(active.ActiveEntries(), 2)
}

// TestHistoryAppendRejectsInvalidTreeMutationBeforePersistence verifies validation precedes durable publication.
func (s *ServiceSuite) TestHistoryAppendRejectsInvalidTreeMutationBeforePersistence() {
	// Arrange an active tree and an ID generator collision with its existing root.
	createdAt := time.Date(2026, 8, 27, 5, 0, 0, 0, time.UTC)
	root := session.Entry{
		ParentID: mo.None[string](), ID: "duplicate", CreatedAt: createdAt,
		Information: mo.None[session.Information](), User: mo.Some(model.TextMessage("root")),
		Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	service.active = LoadedSession{
		Header: session.Header{
			Version:          formatVersion,
			ID:               "active",
			CreatedAt:        createdAt,
			WorkingDirectory: "/project",
		},
		StoragePath:          "/sessions/active.jsonl",
		Tree:                 mustSessionTree([]session.Entry{root}),
		Information:          mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}
	s.ids.EXPECT().NewID().Return("duplicate", nil)
	s.clock.EXPECT().Now().Return(createdAt.Add(time.Second))

	// Act by appending a complete user message with the duplicate ID.
	err := service.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("new")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	})

	// Assert validation rejects the mutation without repository access or active-state change.
	s.Require().ErrorContains(err, "duplicate entry ID")
	entries := service.ActiveEntries()
	s.Require().Len(entries, 1)
	s.Equal("duplicate", entries[0].ID)
}
