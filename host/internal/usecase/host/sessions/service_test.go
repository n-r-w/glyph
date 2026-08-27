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

func (s *ServiceSuite) TestSetNamePersistsNormalizedNameBeforeUpdatingSnapshot() {
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
		},
		StoragePath: "",
		Entry: session.Entry{
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ID:          "entry-id",
			CreatedAt:   updatedAt,
			Information: mo.Some(session.Information{Name: "release notes"}),
		},
	}).Return(AppendResult{StoragePath: "/sessions/file.jsonl"}, nil)

	service := New(s.repository, s.ids, s.clock, "/project")
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

func (s *ServiceSuite) TestResumeReturnsIndependentSnapshot() {
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	entries := []session.Entry{
		{
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ID: "entry-id", CreatedAt: createdAt.Add(time.Minute),
			Information: mo.Some(session.Information{Name: "stored name"}),
		}}
	s.repository.EXPECT().Load(gomock.Any(), session.ID("stored-id")).Return(LoadedSession{
		Header:      session.Header{Version: 1, ID: "stored-id", CreatedAt: createdAt, WorkingDirectory: "/project"},
		StoragePath: "/sessions/stored.jsonl",
		Entries:     entries,
	}, nil)
	service := New(s.repository, s.ids, s.clock, "/project")

	replacement, err := service.ResumeActive(s.T().Context(), "stored-id")
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

func (s *ServiceSuite) TestResumeSerializesWithCompletedTextAppend() {
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
				}},
			}, nil
		},
	)
	resumeDone := make(chan error, 1)
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
	s.Require().NoError(<-resumeDone)
	s.Require().NoError(<-appendDone)
	s.Equal(session.ID("stored"), appendCommand.Header.ID)

	entries := service.ActiveEntries()
	s.Require().Len(entries, 2)
	s.Equal("stored text", entries[0].User.MustGet().Content[0].Text.MustGet())
	s.Equal("appended text", entries[1].User.MustGet().Content[0].Text.MustGet())
}

func (s *ServiceSuite) TestHistoryAppendPersistsTextBeforePublishingImmutableSnapshot() {
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
				s.Require().Len(stored.Content, 1)
				s.Equal(model.ContentText, stored.Content[0].Kind)
				s.Equal("world", stored.Content[0].Text.MustGet())
				s.Equal(mo.Some(model.OutcomeStop), stored.Outcome)
				s.Equal(mo.None[string](), stored.ErrorMessage)
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
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.Some("not durable"),
		Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
		ResponseModel: mo.Some(model.ID("response-model")), ResponseID: mo.Some("response-id"),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
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

func (s *ServiceSuite) TestNonStopModelAndToolResultRemainProcessLocal() {
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	active := New(s.repository, s.ids, s.clock, "/project")
	s.Require().NoError(active.Initialize(s.T().Context()))

	call := model.ToolCall{ID: "call", Name: "read", Arguments: map[string]any{"path": "input.txt"}}
	response := model.Response{
		Content: []model.Content{{
			Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(call),
		}},
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](),
		Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	result := agent.ToolResult{
		CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents("result"), IsError: false,
	}
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
		ToolResult: mo.None[agent.ToolResult](),
	}))
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(result),
	}))

	s.Require().Empty(active.ActiveEntries())
	s.Require().Len(active.Snapshot(), 2)
	s.Equal(call, active.Snapshot()[0].Model.MustGet().Content[0].ToolCall.MustGet())
	s.Equal(result, active.Snapshot()[1].ToolResult.MustGet())
}

func (s *ServiceSuite) TestNextProviderRequestPreservesCompleteProcessLocalToolHistory() {
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
	s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command AppendCommand) (AppendResult, error) {
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
	response := model.Response{
		Content: []model.Content{
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
		ResponseID: mo.Some("response-id"), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	result := agent.ToolResult{
		CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents("contents"), IsError: false,
	}
	s.Require().NoError(active.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("first")),
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
	escapedModel := escaped[1].Model.MustGet()
	escapedContext := escapedModel.Content[0].ProviderContext.MustGet()
	escapedContext.Payload[0] = 9
	escapedCall := escapedModel.Content[1].ToolCall.MustGet()
	escapedCall.Arguments["path"] = "mutated"

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
			storedModel := request.History[1].Model.MustGet()
			s.Require().Len(storedModel.Content, 2)
			s.Equal([]byte{1, 2, 3}, storedModel.Content[0].ProviderContext.MustGet().Payload)
			s.Equal(call, storedModel.Content[1].ToolCall.MustGet())
			s.Equal("response-id", storedModel.ResponseID.MustGet())
			s.Equal(result, request.History[2].ToolResult.MustGet())
			return providerErr
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	service := agentrun.New("instructions", runtime, runner.New(nil, nil, nil), tools, events, active)

	_, err := service.Run(s.T().Context(), agentrun.Request{RunID: "next", UserText: "second"})
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
