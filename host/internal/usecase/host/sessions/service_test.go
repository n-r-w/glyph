package sessions

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/hooks/runner"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

type ServiceSuite struct {
	suite.Suite
	repository *MockRepository
	ids        *MockIDGenerator
	clock      *MockClock
}

func TestServiceSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(ServiceSuite))
}

func (s *ServiceSuite) SetupTest() {
	controller := gomock.NewController(s.T())
	s.repository = NewMockRepository(controller)
	s.ids = NewMockIDGenerator(controller)
	s.clock = NewMockClock(controller)
}

func (s *ServiceSuite) TestInitializeCreatesUnpersistedActiveSession() {
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)

	service := New(s.repository, s.ids, s.clock, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))

	s.Equal(session.Info{
		ID:               "session-id",
		Name:             mo.None[string](),
		WorkingDirectory: "/project",
		StoragePath:      mo.None[string](),
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}, service.ActiveInfo())
}

func (s *ServiceSuite) TestCreateReplacesActiveSessionWithIndependentSnapshot() {
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.ids.EXPECT().NewID().Return("created-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	service := New(s.repository, s.ids, s.clock, "/project")

	created, err := service.CreateActive(s.T().Context())
	s.Require().NoError(err)
	s.Equal(session.ID("created-id"), created.Info.ID)
	s.False(created.Info.Name.IsPresent())
	s.False(created.Info.StoragePath.IsPresent())
	s.Equal(createdAt, created.Info.CreatedAt)
	s.Empty(created.Entries)
	created.Info.Name = mo.Some("caller mutation")

	active := service.ActiveInfo()
	s.Equal(session.ID("created-id"), active.ID)
	s.False(active.Name.IsPresent())
	s.False(active.StoragePath.IsPresent())
}

// TestSetNamePersistsNormalizedNameBeforeUpdatingSnapshot verifies set name persists normalized name before updating snapshot.
func (s *ServiceSuite) TestSetNamePersistsNormalizedNameBeforeUpdatingSnapshot() {
	// Arrange test dependencies and scenario inputs.
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	s.ids.EXPECT().NewID().Return("entry-id", nil)
	s.clock.EXPECT().Now().Return(updatedAt)
	s.repository.EXPECT().Append(gomock.Any(), AppendCommand{
		Header: session.Header{
			Version:          1,
			ID:               "session-id",
			CreatedAt:        createdAt,
			WorkingDirectory: "/project",
			// Act by executing the scenario.
		},
		StoragePath: "",
		Entry: session.Entry{
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ToolResult:  mo.None[session.ToolResult](),
			ID:          "entry-id",
			CreatedAt:   updatedAt,
			Information: mo.Some(session.Information{Name: "release notes"}),
			Extension:   mo.None[session.ExtensionEnvelope]()},
	}).Return(AppendResult{StoragePath: "/sessions/file.jsonl"}, nil)

	service := New(s.repository, s.ids, s.clock, "/project")
	// Assert the scenario produces the required observable result.
	s.Require().NoError(service.Initialize(s.T().Context()))
	info, err := service.SetActiveName(s.T().Context(), "  release\r\n\nnotes  ")
	s.Require().NoError(err)
	s.Equal(mo.Some("release notes"), info.Name)
	s.Equal(mo.Some("/sessions/file.jsonl"), info.StoragePath)
	s.Equal(updatedAt, info.UpdatedAt)
}

func (s *ServiceSuite) TestSetNameRejectsWhitespaceWithoutPersistence() {
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	service := New(s.repository, s.ids, s.clock, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))

	_, err := service.SetActiveName(s.T().Context(), " \r\n\t ")
	s.Require().ErrorIs(err, session.ErrInvalidName)
	s.Equal(mo.None[string](), service.ActiveInfo().Name)
}

func (s *ServiceSuite) TestSetNameUsesUniqueEntryIDsAndSuppliedTimestamps() {
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	firstUpdate := createdAt.Add(time.Minute)
	secondUpdate := createdAt.Add(2 * time.Minute)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	gomock.InOrder(
		s.ids.EXPECT().NewID().Return("entry-1", nil),
		s.clock.EXPECT().Now().Return(firstUpdate),
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command AppendCommand) (AppendResult, error) {
				s.Equal("entry-1", command.Entry.ID)
				s.Equal(firstUpdate, command.Entry.CreatedAt)
				return AppendResult{StoragePath: "/sessions/file.jsonl"}, nil
			},
		),
		s.ids.EXPECT().NewID().Return("entry-2", nil),
		s.clock.EXPECT().Now().Return(secondUpdate),
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command AppendCommand) (AppendResult, error) {
				s.Equal("entry-2", command.Entry.ID)
				s.Equal(secondUpdate, command.Entry.CreatedAt)
				s.Equal("/sessions/file.jsonl", command.StoragePath)
				return AppendResult{StoragePath: command.StoragePath}, nil
			},
		),
	)

	service := New(s.repository, s.ids, s.clock, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))
	_, err := service.SetActiveName(s.T().Context(), "first")
	s.Require().NoError(err)
	info, err := service.SetActiveName(s.T().Context(), "second")
	s.Require().NoError(err)
	s.Equal(mo.Some("second"), info.Name)
	s.Equal(secondUpdate, info.UpdatedAt)
}

func (s *ServiceSuite) TestListOrdersUpdatesAndUsesUnnamedIDFallbackData() {
	base := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.repository.EXPECT().List(gomock.Any()).Return([]LoadedSession{
		{Header: session.Header{Version: 1, ID: "older", CreatedAt: base, WorkingDirectory: "/project"}, StoragePath: "/older.jsonl", Entries: nil},
		{Header: session.Header{Version: 1, ID: "z-id", CreatedAt: base.Add(time.Minute), WorkingDirectory: "/project"}, StoragePath: "/z.jsonl", Entries: nil},
		{Header: session.Header{Version: 1, ID: "a-id", CreatedAt: base.Add(time.Minute), WorkingDirectory: "/project"}, StoragePath: "/a.jsonl", Entries: nil},
	}, nil)
	service := New(s.repository, s.ids, s.clock, "/project")

	listed, err := service.ListStored(s.T().Context())
	s.Require().NoError(err)
	s.Require().Len(listed, 3)
	s.Equal(session.ID("a-id"), listed[0].Info.ID)
	s.Equal(session.ID("z-id"), listed[1].Info.ID)
	s.Equal(session.ID("older"), listed[2].Info.ID)
	for _, summary := range listed {
		s.False(summary.Info.Name.IsPresent())
		s.False(summary.FirstUserText.IsPresent())
		s.Zero(summary.TotalMessages)
	}
}

// TestListCountsStoredToolResultsAsTerminalMessages verifies list counts stored tool results as terminal messages.
func (s *ServiceSuite) TestListCountsStoredToolResultsAsTerminalMessages() {
	// Arrange test dependencies and scenario inputs.
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	s.repository.EXPECT().List(gomock.Any()).Return([]LoadedSession{{
		Header:      session.Header{Version: 1, ID: "stored", CreatedAt: createdAt, WorkingDirectory: "/project"},
		StoragePath: "/sessions/stored.jsonl",
		Entries: []session.Entry{
			{
				ID: "user", CreatedAt: createdAt.Add(time.Second), Information: mo.None[session.Information](),
				User: mo.Some(model.TextMessage("question")), Model: mo.None[session.ModelResponse](),
				ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			},
			{
				ID: "model", CreatedAt: createdAt.Add(2 * time.Second), Information: mo.None[session.Information](),
				User: mo.None[session.UserMessage](), Model: mo.Some(model.Response{
					Content: nil, Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
					Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
					ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
					// Act by executing the scenario.
				}),
				ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			},
			{
				ID: "tool", CreatedAt: createdAt.Add(3 * time.Second), Information: mo.None[session.Information](),
				User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "read", Contents: tool.TextContents("result"), IsError: false,
				}),
				Extension: mo.None[session.ExtensionEnvelope](),
			},
		},
	}}, nil)
	service := New(s.repository, s.ids, s.clock, "/project")

	listed, err := service.ListStored(s.T().Context())

	// Assert the scenario produces the required observable result.
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
	s.Equal(3, listed[0].TotalMessages)
}

// TestResumeReturnsIndependentSnapshot verifies resume returns independent snapshot.
func (s *ServiceSuite) TestResumeReturnsIndependentSnapshot() {
	// Arrange test dependencies and scenario inputs.
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	entries := []session.Entry{
		{
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			ID: "entry-id", CreatedAt: createdAt.Add(time.Minute),
			Information: mo.Some(session.Information{Name: "stored name"}),
		}}
	s.repository.EXPECT().Load(gomock.Any(), session.ID("stored-id")).Return(LoadedSession{
		Header:      session.Header{Version: 1, ID: "stored-id", CreatedAt: createdAt, WorkingDirectory: "/project"},
		StoragePath: "/sessions/stored.jsonl",
		// Act by executing the scenario.
		Entries: entries,
	}, nil)
	service := New(s.repository, s.ids, s.clock, "/project")

	replacement, err := service.ResumeActive(s.T().Context(), "stored-id")
	// Assert the scenario produces the required observable result.
	s.Require().NoError(err)
	s.Require().Len(replacement.Entries, 1)
	entries[0].Information = mo.Some(session.Information{Name: "mutated source"})
	replacement.Info.Name = mo.Some("mutated result")
	replacement.Entries[0].Information = mo.Some(session.Information{Name: "mutated replacement"})
	active := service.ActiveInfo()
	s.Equal(session.ID("stored-id"), active.ID)
	s.Equal(mo.Some("stored name"), active.Name)
	s.Equal(mo.Some("/sessions/stored.jsonl"), active.StoragePath)
}

// TestResumeSerializesWithCompletedTextAppend verifies resume serializes with completed text append.
func (s *ServiceSuite) TestResumeSerializesWithCompletedTextAppend() {
	// Arrange test dependencies and scenario inputs.
	createdAt := time.Date(2026, 8, 27, 2, 0, 0, 0, time.UTC)
	s.ids.EXPECT().NewID().Return("old-session", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	service := New(s.repository, s.ids, s.clock, "/project")
	_, err := service.CreateActive(s.T().Context())
	s.Require().NoError(err)

	loadStarted := make(chan struct{})
	releaseLoad := make(chan struct{})
	s.repository.EXPECT().Load(gomock.Any(), session.ID("stored")).DoAndReturn(
		func(context.Context, session.ID) (LoadedSession, error) {
			close(loadStarted)
			<-releaseLoad
			return LoadedSession{
				Header: session.Header{
					Version: 1, ID: "stored", CreatedAt: createdAt, WorkingDirectory: "/project",
				},
				StoragePath: "/sessions/stored.jsonl",
				Entries: []session.Entry{{
					ID: "stored-entry", CreatedAt: createdAt, Information: mo.None[session.Information](),
					User: mo.Some(model.TextMessage("stored text")), Model: mo.None[model.Response](),
					ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
				}},
			}, nil
		},
	)
	resumeDone := make(chan error, 1)
	// Act by executing the scenario.
	go func() {
		_, resumeErr := service.ResumeActive(s.T().Context(), "stored")
		resumeDone <- resumeErr
	}()
	<-loadStarted

	s.ids.EXPECT().NewID().Return("appended-entry", nil)
	s.clock.EXPECT().Now().Return(createdAt.Add(time.Minute))
	var appendCommand AppendCommand
	s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command AppendCommand) (AppendResult, error) {
			appendCommand = command
			return AppendResult{StoragePath: "/sessions/stored.jsonl"}, nil
		},
	)
	appendStarted := make(chan struct{})
	appendDone := make(chan error, 1)
	go func() {
		close(appendStarted)
		appendDone <- service.Append(s.T().Context(), agent.HistoryEntry{
			Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("appended text")),
			Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
		})
	}()
	<-appendStarted
	close(releaseLoad)
	// Assert the scenario produces the required observable result.
	s.Require().NoError(<-resumeDone)
	s.Require().NoError(<-appendDone)
	s.Equal(session.ID("stored"), appendCommand.Header.ID)

	entries := service.ActiveEntries()
	s.Require().Len(entries, 2)
	s.Equal("stored text", entries[0].User.MustGet().Content[0].Text.MustGet())
	s.Equal("appended text", entries[1].User.MustGet().Content[0].Text.MustGet())
}

// TestHistoryAppendPersistsTextBeforePublishingImmutableSnapshot verifies history append persists text before publishing immutable snapshot.
func (s *ServiceSuite) TestHistoryAppendPersistsTextBeforePublishingImmutableSnapshot() {
	// Arrange test dependencies and scenario inputs.
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
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command AppendCommand) (AppendResult, error) {
				s.Equal("hello", command.Entry.User.MustGet().Content[0].Text.MustGet())
				persisted = append(persisted, command.Entry)
				return AppendResult{StoragePath: "/sessions/file.jsonl"}, nil
			},
		),
		s.ids.EXPECT().NewID().Return("model-entry", nil),
		s.clock.EXPECT().Now().Return(modelAt),
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command AppendCommand) (AppendResult, error) {
				stored := command.Entry.Model.MustGet()
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
				persisted = append(persisted, command.Entry)
				return AppendResult{StoragePath: "/sessions/file.jsonl"}, nil
			},
		),
	)
	service := New(s.repository, s.ids, s.clock, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))

	s.Require().NoError(service.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("hello")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}))
	// Act by executing the scenario.
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
	// Assert the scenario produces the required observable result.
	s.Require().NoError(service.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
		Model: mo.Some(response), ToolResult: mo.None[agent.ToolResult](),
	}))

	history := service.Snapshot()
	s.Require().Len(history, 2)
	s.Equal("hello", history[0].User.MustGet().Content[0].Text.MustGet())
	s.Require().Len(history[1].Model.MustGet().Content, 3)
	s.Equal("response-id", history[1].Model.MustGet().ResponseID.MustGet())
	history[0].User.MustGet().Content[0].Text = mo.Some("mutated")
	s.Equal("hello", service.Snapshot()[0].User.MustGet().Content[0].Text.MustGet())

	s.repository.EXPECT().List(gomock.Any()).Return([]LoadedSession{{
		Header:      session.Header{Version: 1, ID: "session-id", CreatedAt: createdAt, WorkingDirectory: "/project"},
		StoragePath: "/sessions/file.jsonl", Entries: persisted,
	}}, nil)
	listed, err := service.ListStored(s.T().Context())
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
	s.Equal(mo.Some("hello"), listed[0].FirstUserText)
	s.Equal(2, listed[0].TotalMessages)
}

func (s *ServiceSuite) TestTerminalModelAndToolResultBecomeDurableBeforeSnapshotPublication() {
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	modelAt := createdAt.Add(time.Second)
	toolAt := createdAt.Add(2 * time.Second)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	active := New(s.repository, s.ids, s.clock, "/project")
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
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](), Provider: mo.Some(model.ProviderID("provider")),
		Model: mo.Some(model.ID("model")), ResponseModel: mo.Some(model.ID("response-model")), ResponseID: mo.Some("response-id"),
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
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command AppendCommand) (AppendResult, error) {
				s.Equal(response, command.Entry.Model.MustGet())
				return AppendResult{StoragePath: "/sessions/history.jsonl"}, nil
			},
		),
		s.ids.EXPECT().NewID().Return("tool-entry", nil),
		s.clock.EXPECT().Now().Return(toolAt),
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command AppendCommand) (AppendResult, error) {
				s.Equal(result, command.Entry.ToolResult.MustGet())
				return AppendResult{StoragePath: "/sessions/history.jsonl"}, nil
			},
		),
	)
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
		ToolResult: mo.None[agent.ToolResult](),
	}))
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(result),
	}))

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

func (s *ServiceSuite) TestTerminalModelProjectionPreservesContentSliceStateAndOrder() {
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	active := New(s.repository, s.ids, s.clock, "/project")
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
			s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, command AppendCommand) (AppendResult, error) {
					actual := command.Entry.Model.MustGet().Content
					s.Equal(test.content == nil, actual == nil)
					s.Equal(test.content, actual)
					return AppendResult{StoragePath: "/sessions/history.jsonl"}, nil
				},
			),
		)
	}

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

		durableContent := active.ActiveEntries()[index].Model.MustGet().Content
		s.Equal(test.content == nil, durableContent == nil)
		s.Equal(test.content, durableContent)
		snapshotContent := active.Snapshot()[index].Model.MustGet().Content
		s.Equal(test.content == nil, snapshotContent == nil)
		s.Equal(test.content, snapshotContent)
	}
}

func (s *ServiceSuite) TestToolResultAppendFailureKeepsDurableAndProviderHistoryUnchanged() {
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	active := New(s.repository, s.ids, s.clock, "/project")
	s.Require().NoError(active.Initialize(s.T().Context()))
	result := agent.ToolResult{
		CallID: "call", ToolName: "read", Contents: tool.TextContents("completed effect"), IsError: false,
	}
	gomock.InOrder(
		s.ids.EXPECT().NewID().Return("tool-entry", nil),
		s.clock.EXPECT().Now().Return(createdAt.Add(time.Second)),
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).Return(AppendResult{}, errors.New("sync failed")),
	)

	err := active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(result),
	})
	s.Require().ErrorContains(err, "sync failed")
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
	active := New(s.repository, s.ids, s.clock, "/project")
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
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command AppendCommand) (AppendResult, error) {
				s.Equal(result, command.Entry.ToolResult.MustGet())
				return AppendResult{StoragePath: "/sessions/history.jsonl"}, nil
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

func (s *ServiceSuite) TestTerminalToolResultProjectionPreservesContentsSliceState() {
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	active := New(s.repository, s.ids, s.clock, "/project")
	s.Require().NoError(active.Initialize(s.T().Context()))

	gomock.InOrder(
		s.ids.EXPECT().NewID().Return("nil-entry", nil),
		s.clock.EXPECT().Now().Return(createdAt.Add(time.Second)),
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command AppendCommand) (AppendResult, error) {
				contents := command.Entry.ToolResult.MustGet().Contents
				s.Nil(contents)
				return AppendResult{StoragePath: "/sessions/history.jsonl"}, nil
			},
		),
		s.ids.EXPECT().NewID().Return("empty-entry", nil),
		s.clock.EXPECT().Now().Return(createdAt.Add(2*time.Second)),
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command AppendCommand) (AppendResult, error) {
				contents := command.Entry.ToolResult.MustGet().Contents
				s.NotNil(contents)
				s.Empty(contents)
				return AppendResult{StoragePath: "/sessions/history.jsonl"}, nil
			},
		),
	)
	for _, contents := range [][]tool.ResultContent{nil, {}} {
		s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
			Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
			ToolResult: mo.Some(agent.ToolResult{
				CallID: "call", ToolName: "tool", Contents: contents, IsError: false,
			}),
		}))
	}
}

// TestNextProviderRequestPreservesCompleteRestartedToolHistory verifies next provider request preserves complete restarted tool history.
func (s *ServiceSuite) TestNextProviderRequestPreservesCompleteRestartedToolHistory() {
	// Arrange test dependencies and scenario inputs.
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
	s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command AppendCommand) (AppendResult, error) {
			persisted = append(persisted, command.Entry)
			return AppendResult{StoragePath: "/sessions/history.jsonl"}, nil
		},
	).AnyTimes()
	active := New(s.repository, s.ids, s.clock, "/project")
	s.Require().NoError(active.Initialize(s.T().Context()))

	call := model.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "input.txt"}}
	providerContext := model.ProviderContext{
		Source: model.ProviderContextSource{
			ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.Some("compatible"),
		},
		Payload: []byte{1, 2, 3},
	}
	// Act by executing the scenario.
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
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](), Provider: mo.Some(model.ProviderID("provider")),
		Model: mo.Some(model.ID("model")), ResponseModel: mo.Some(model.ID("response-model")),
		ResponseID: mo.Some("response-id"), Usage: mo.None[model.Usage](),
		Diagnostics: []model.Diagnostic{{Code: "notice", Message: "safe diagnostic"}},
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
		{Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.Some("image/png"), Data: mo.Some([]byte{4, 5, 6})},
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
	// Assert the scenario produces the required observable result.
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
		StoragePath: "/sessions/history.jsonl", Entries: persisted,
	}, nil)
	restarted := New(s.repository, s.ids, s.clock, "/project")
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
	runtime.EXPECT().Current().Return(agentrun.RuntimeSelection{
		Model: model.Descriptor{
			Provider: "provider", Model: "model", ReasoningCapabilities: model.ReasoningCapabilities{},
			ToolCapabilities: model.ToolCapabilities{},
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

func (s *ServiceSuite) TestSetNameAppendFailurePreservesExistingNameAndStoragePath() {
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	gomock.InOrder(
		s.ids.EXPECT().NewID().Return("entry-1", nil),
		s.clock.EXPECT().Now().Return(createdAt.Add(time.Minute)),
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).Return(AppendResult{StoragePath: "/sessions/file.jsonl"}, nil),
		s.ids.EXPECT().NewID().Return("entry-2", nil),
		s.clock.EXPECT().Now().Return(createdAt.Add(2*time.Minute)),
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).Return(AppendResult{}, errors.New("write failed")),
	)

	service := New(s.repository, s.ids, s.clock, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))
	_, err := service.SetActiveName(s.T().Context(), "stable name")
	s.Require().NoError(err)
	before := service.ActiveInfo()
	_, err = service.SetActiveName(s.T().Context(), "lost name")
	s.Require().Error(err)
	s.Equal(mo.Some("stable name"), before.Name)
	s.Equal(mo.Some("/sessions/file.jsonl"), before.StoragePath)
	s.Equal(before, service.ActiveInfo())
}
