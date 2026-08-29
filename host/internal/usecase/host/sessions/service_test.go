package sessions

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	pricing    *MockPricingCatalog
}

// TestServiceSuite runs active-session persistence, projection, replacement, and failure scenarios.
func TestServiceSuite(t *testing.T) {
	t.Parallel()

	// Arrange a fresh active-session service suite.

	// Act by running every suite scenario.

	// Assert through the scenario assertions and mock expectations owned by the suite.
	suite.Run(t, new(ServiceSuite))
}

func (s *ServiceSuite) SetupTest() {
	controller := gomock.NewController(s.T())
	s.repository = NewMockRepository(controller)
	s.ids = NewMockIDGenerator(controller)
	s.clock = NewMockClock(controller)
	s.pricing = NewMockPricingCatalog(controller)
	s.pricing.EXPECT().Pricing(gomock.Any(), gomock.Any()).Return(mo.None[model.Pricing]()).AnyTimes()
}

// TestInitializeCreatesUnpersistedActiveSession verifies initialization creates only an in-memory active session.
func (s *ServiceSuite) TestInitializeCreatesUnpersistedActiveSession() {
	// Arrange deterministic session identity and creation time.
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)

	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")

	// Act by initializing the active-session service.
	s.Require().NoError(service.Initialize(s.T().Context()))

	// Assert the active session has no durable name or storage path.
	s.Equal(session.Info{
		ID:               "session-id",
		Name:             mo.None[string](),
		WorkingDirectory: "/project",
		StoragePath:      mo.None[string](),
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}, service.ActiveInfo())
}

// TestResumeFailurePreservesPreviousActiveSession verifies rejected storage cannot replace active identity.
func (s *ServiceSuite) TestResumeFailurePreservesPreviousActiveSession() {
	// Arrange one initialized active session and a failed target load.
	createdAt := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("active-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))
	beforeInfo := service.ActiveInfo()
	beforeHistory := service.Snapshot()
	s.repository.EXPECT().Load(gomock.Any(), session.ID("broken-id")).Return(
		LoadedSession{}, fmt.Errorf("%w: invalid completed record", session.ErrUnavailable),
	)

	// Act by trying to resume the unavailable target session.
	_, err := service.ResumeActive(s.T().Context(), session.ID("broken-id"))

	// Assert the error propagates and the previous active snapshot remains exact.
	s.Require().ErrorIs(err, session.ErrUnavailable)
	s.Equal(beforeInfo, service.ActiveInfo())
	s.Equal(beforeHistory, service.Snapshot())
}

// TestCreateReplacesActiveSessionWithIndependentSnapshot verifies caller mutation cannot alter the new active session.
func (s *ServiceSuite) TestCreateReplacesActiveSessionWithIndependentSnapshot() {
	// Arrange deterministic identity and time for a new active session.
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.ids.EXPECT().NewID().Return("created-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")

	// Act by creating the active session and mutating the returned replacement.
	created, err := service.CreateActive(s.T().Context())
	s.Require().NoError(err)
	s.Equal(session.ID("created-id"), created.Info.ID)
	s.False(created.Info.Name.IsPresent())
	s.False(created.Info.StoragePath.IsPresent())
	s.Equal(createdAt, created.Info.CreatedAt)
	s.Empty(created.Entries)
	created.Info.Name = mo.Some("caller mutation")

	// Assert the service retains independent active-session metadata.
	active := service.ActiveInfo()
	s.Equal(session.ID("created-id"), active.ID)
	s.False(active.Name.IsPresent())
	s.False(active.StoragePath.IsPresent())
}

// TestSetNamePersistsNormalizedNameBeforeUpdatingSnapshot verifies a whitespace-heavy name becomes durable before publication.
func (s *ServiceSuite) TestSetNamePersistsNormalizedNameBeforeUpdatingSnapshot() {
	// Arrange an initialized session and an append expectation for the normalized name.
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
			ToolResult:  mo.None[session.ToolResult](),
			ID:          "entry-id",
			CreatedAt:   updatedAt,
			Information: mo.Some(session.Information{Name: "release notes"}),
			Extension:   mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost]()},
	}).Return(AppendResult{StoragePath: "/sessions/file.jsonl"}, nil)

	// Act by initializing the service and setting a whitespace-heavy name.
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))
	info, err := service.SetActiveName(s.T().Context(), "  release\r\n\nnotes  ")
	s.Require().NoError(err)
	// Assert the normalized name is persisted before the active snapshot changes.
	s.Equal(mo.Some("release notes"), info.Name)
	s.Equal(mo.Some("/sessions/file.jsonl"), info.StoragePath)
	s.Equal(updatedAt, info.UpdatedAt)
}

// TestSetNameRejectsWhitespaceWithoutPersistence verifies blank normalized names do not reach persistence.
func (s *ServiceSuite) TestSetNameRejectsWhitespaceWithoutPersistence() {
	// Arrange an initialized unnamed active session.
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))

	// Act by assigning a whitespace-only name.
	_, err := service.SetActiveName(s.T().Context(), " \r\n\t ")

	// Assert validation rejects the name and preserves the unnamed state.
	s.Require().ErrorIs(err, session.ErrInvalidName)
	s.Equal(mo.None[string](), service.ActiveInfo().Name)
}

// TestSetNameUsesUniqueEntryIDsAndSuppliedTimestamps verifies each name update owns a new entry identity and time.
func (s *ServiceSuite) TestSetNameUsesUniqueEntryIDsAndSuppliedTimestamps() {
	// Arrange two ordered name updates with distinct IDs and timestamps.
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

	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))

	// Act by assigning two consecutive durable names.
	_, err := service.SetActiveName(s.T().Context(), "first")
	s.Require().NoError(err)
	info, err := service.SetActiveName(s.T().Context(), "second")

	// Assert the second unique entry and supplied time become active.
	s.Require().NoError(err)
	s.Equal(mo.Some("second"), info.Name)
	s.Equal(secondUpdate, info.UpdatedAt)
}

// TestListOrdersUpdatesAndUsesUnnamedIDFallbackData verifies unnamed summaries are ordered without invented display data.
func (s *ServiceSuite) TestListOrdersUpdatesAndUsesUnnamedIDFallbackData() {
	// Arrange stored unnamed sessions with tied and distinct update times.
	base := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.repository.EXPECT().List(gomock.Any()).Return([]LoadedSession{
		{Header: session.Header{Version: 1, ID: "older", CreatedAt: base, WorkingDirectory: "/project"}, StoragePath: "/older.jsonl", Entries: nil},
		{Header: session.Header{Version: 1, ID: "z-id", CreatedAt: base.Add(time.Minute), WorkingDirectory: "/project"}, StoragePath: "/z.jsonl", Entries: nil},
		{Header: session.Header{Version: 1, ID: "a-id", CreatedAt: base.Add(time.Minute), WorkingDirectory: "/project"}, StoragePath: "/a.jsonl", Entries: nil},
	}, nil)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")

	// Act by listing reconstructed session summaries.
	listed, err := service.ListStored(s.T().Context())
	// Assert summaries use deterministic update order and absent fallback fields.
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

// TestListCountsStoredToolResultsAsTerminalMessages verifies model and tool-result entries both contribute to the summary count.
func (s *ServiceSuite) TestListCountsStoredToolResultsAsTerminalMessages() {
	// Arrange a stored session containing model and tool-result terminal entries.
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	s.repository.EXPECT().List(gomock.Any()).Return([]LoadedSession{{
		Header:      session.Header{Version: 1, ID: "stored", CreatedAt: createdAt, WorkingDirectory: "/project"},
		StoragePath: "/sessions/stored.jsonl",
		Entries: []session.Entry{
			{
				ID: "user", CreatedAt: createdAt.Add(time.Second), Information: mo.None[session.Information](),
				User: mo.Some(model.TextMessage("question")), Model: mo.None[session.ModelResponse](),
				ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
			},
			{
				ID: "model", CreatedAt: createdAt.Add(2 * time.Second), Information: mo.None[session.Information](),
				User: mo.None[session.UserMessage](), Model: mo.Some(model.Response{
					Content: nil, Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
					Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
					ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
				}),
				ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
			},
			{
				ID: "tool", CreatedAt: createdAt.Add(3 * time.Second), Information: mo.None[session.Information](),
				User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "read", Contents: tool.TextContents("result"), IsError: false,
				}),
				Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
			},
		},
	}}, nil)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")

	// Act by listing the stored session.
	listed, err := service.ListStored(s.T().Context())

	// Assert the summary counts every terminal message including tool results.
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
	s.Equal(3, listed[0].TotalMessages)
}

// TestResumeReturnsIndependentSnapshot verifies source and replacement mutations cannot alter active metadata.
func (s *ServiceSuite) TestResumeReturnsIndependentSnapshot() {
	// Arrange a stored session with caller-owned entry metadata.
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	entries := []session.Entry{
		{
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			ID: "entry-id", CreatedAt: createdAt.Add(time.Minute),
			Information: mo.Some(session.Information{Name: "stored name"}), EstimatedCost: mo.None[session.EstimatedCost](),
		}}
	s.repository.EXPECT().Load(gomock.Any(), session.ID("stored-id")).Return(LoadedSession{
		Header:      session.Header{Version: 1, ID: "stored-id", CreatedAt: createdAt, WorkingDirectory: "/project"},
		StoragePath: "/sessions/stored.jsonl",
		Entries:     entries,
	}, nil)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")

	// Act by resuming and mutating source and returned replacement values.
	replacement, err := service.ResumeActive(s.T().Context(), "stored-id")
	s.Require().NoError(err)
	s.Require().Len(replacement.Entries, 1)
	entries[0].Information = mo.Some(session.Information{Name: "mutated source"})
	replacement.Info.Name = mo.Some("mutated result")
	replacement.Entries[0].Information = mo.Some(session.Information{Name: "mutated replacement"})
	// Assert the active session retains independently owned metadata.
	active := service.ActiveInfo()
	s.Equal(session.ID("stored-id"), active.ID)
	s.Equal(mo.Some("stored name"), active.Name)
	s.Equal(mo.Some("/sessions/stored.jsonl"), active.StoragePath)
}

// TestResumeOwnsExtensionEnvelopeBytesAcrossSnapshots verifies every active-session boundary owns extension bytes.
func (s *ServiceSuite) TestResumeOwnsExtensionEnvelopeBytesAcrossSnapshots() {
	// Arrange a loaded session with one present extension envelope and caller-owned JSON bytes.
	createdAt := time.Date(2026, 8, 27, 4, 0, 0, 0, time.UTC)
	want := []byte(`{"checkpoint":true}`)
	repositoryBytes := append([]byte(nil), want...)
	entries := []session.Entry{{
		ID: "extension-entry", CreatedAt: createdAt, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](),
		Extension: mo.Some(session.ExtensionEnvelope{
			ExtensionID: "example.extension", EntryType: "checkpoint", Data: repositoryBytes,
		}), EstimatedCost: mo.None[session.EstimatedCost](),
	}}
	s.repository.EXPECT().Load(gomock.Any(), session.ID("stored-id")).Return(LoadedSession{
		Header: session.Header{
			Version: 1, ID: "stored-id", CreatedAt: createdAt, WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/stored.jsonl", Entries: entries,
	}, nil)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")

	// Act by resuming, then mutating repository input, returned replacement, and one active snapshot.
	replacement, err := service.ResumeActive(s.T().Context(), "stored-id")
	s.Require().NoError(err)
	repositoryBytes[0] = 'X'
	replacement.Entries[0].Extension.MustGet().Data[1] = 'Y'
	firstSnapshot := service.ActiveEntries()
	firstSnapshot[0].Extension.MustGet().Data[2] = 'Z'
	laterSnapshot := service.ActiveEntries()

	// Assert the later snapshot retains extension presence and the original independently owned bytes.
	s.Require().Len(laterSnapshot, 1)
	s.Require().True(laterSnapshot[0].Extension.IsPresent())
	s.Equal(want, laterSnapshot[0].Extension.MustGet().Data)
}

// TestResumeSerializesWithCompletedTextAppend verifies resume cannot redirect an append that already owns the service lock.
func (s *ServiceSuite) TestResumeSerializesWithCompletedTextAppend() {
	// Arrange a blocked repository load and a concurrent completed text append.
	createdAt := time.Date(2026, 8, 27, 2, 0, 0, 0, time.UTC)
	s.ids.EXPECT().NewID().Return("old-session", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")
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
					ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
				}},
			}, nil
		},
	)
	resumeDone := make(chan error, 1)
	// Act by starting resume and append operations while controlling load release.
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
	// Assert serialization keeps the completed append on its original session.
	s.Require().NoError(<-resumeDone)
	s.Require().NoError(<-appendDone)
	s.Equal(session.ID("stored"), appendCommand.Header.ID)

	entries := service.ActiveEntries()
	s.Require().Len(entries, 2)
	s.Equal("stored text", entries[0].User.MustGet().Content[0].Text.MustGet())
	s.Equal("appended text", entries[1].User.MustGet().Content[0].Text.MustGet())
}

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
		s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).Return(AppendResult{}, errors.New("sync failed")),
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

// TestNextProviderRequestPreservesCompleteRestartedToolHistory verifies restart retains complete tool history and independently owned bytes.
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
	s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command AppendCommand) (AppendResult, error) {
			persisted = append(persisted, command.Entry)
			return AppendResult{StoragePath: "/sessions/history.jsonl"}, nil
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
		StoragePath: "/sessions/history.jsonl", Entries: persisted,
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
	runtime.EXPECT().Current().Return(agentrun.RuntimeSelection{
		Model: model.Descriptor{
			Provider: "provider", Model: "model", Input: nil, ContextWindow: 0, MaxTokens: 0, ReasoningCapabilities: model.ReasoningCapabilities{},
			ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
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

// TestSetNameAppendFailureMakesOnlyActiveSessionWriteUnavailable verifies snapshot preservation and local write blocking.
func (s *ServiceSuite) TestSetNameAppendFailureMakesOnlyActiveSessionWriteUnavailable() {
	// Arrange one successful name append followed by one failed append.
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

	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	s.Require().NoError(service.Initialize(s.T().Context()))
	_, err := service.SetActiveName(s.T().Context(), "stable name")
	s.Require().NoError(err)
	before := service.ActiveInfo()

	// Act by attempting the failing second name update.
	_, err = service.SetActiveName(s.T().Context(), "lost name")

	// Assert the prior snapshot stays readable and later mutations make no storage call.
	s.Require().ErrorIs(err, session.ErrPersistenceUnavailable)
	s.Equal(mo.Some("stable name"), before.Name)
	s.Equal(mo.Some("/sessions/file.jsonl"), before.StoragePath)
	s.Equal(before, service.ActiveInfo())
	s.Require().Len(service.ActiveEntries(), 1)
	s.Equal("stable name", service.ActiveEntries()[0].Information.MustGet().Name)
	s.Zero(service.ActiveStatistics().TotalMessages)
	s.Equal(before, service.ActiveInformation().Info)

	_, err = service.SetActiveName(s.T().Context(), "blocked name")
	s.Require().ErrorIs(err, session.ErrPersistenceUnavailable)
	err = service.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("blocked content")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	})
	s.Require().ErrorIs(err, agentrun.ErrPersistenceUnavailable)
}

// TestCreateAndSuccessfulResumeRestoreWrites verifies only active replacement clears local persistence failure.
func (s *ServiceSuite) TestCreateAndSuccessfulResumeRestoreWrites() {
	// Arrange a failed active mutation, a failed resume, a successful resume, and a later successful append.
	createdAt := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	s.ids.EXPECT().NewID().Return("active", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")
	_, err := service.CreateActive(s.T().Context())
	s.Require().NoError(err)
	s.ids.EXPECT().NewID().Return("failed-entry", nil)
	s.clock.EXPECT().Now().Return(createdAt.Add(time.Second))
	s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).Return(AppendResult{}, errors.New("disk failed"))
	err = service.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("first")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	})
	s.Require().ErrorIs(err, agentrun.ErrPersistenceUnavailable)
	before := service.ActiveInfo()
	s.repository.EXPECT().Load(gomock.Any(), session.ID("broken")).Return(LoadedSession{}, session.ErrUnavailable)

	// Act by failing resume, then resuming valid storage and appending to the replacement.
	_, err = service.ResumeActive(s.T().Context(), "broken")
	s.Require().ErrorIs(err, session.ErrUnavailable)
	s.Equal(before, service.ActiveInfo())
	err = service.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("still blocked")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	})
	s.Require().ErrorIs(err, agentrun.ErrPersistenceUnavailable)

	resumedAt := createdAt.Add(time.Minute)
	s.repository.EXPECT().Load(gomock.Any(), session.ID("stored")).Return(LoadedSession{
		Header:      session.Header{Version: 1, ID: "stored", CreatedAt: resumedAt, WorkingDirectory: "/project"},
		StoragePath: "/sessions/stored.jsonl", Entries: nil,
	}, nil)
	_, err = service.ResumeActive(s.T().Context(), "stored")
	s.Require().NoError(err)
	s.ids.EXPECT().NewID().Return("resumed-entry", nil)
	s.clock.EXPECT().Now().Return(resumedAt.Add(time.Second))
	s.repository.EXPECT().Append(gomock.Any(), gomock.Any()).Return(AppendResult{StoragePath: "/sessions/stored.jsonl"}, nil)
	err = service.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("resumed write")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	})

	// Assert successful resume restores mutation access and advances only the resumed snapshot.
	s.Require().NoError(err)
	s.Equal(session.ID("stored"), service.ActiveInfo().ID)
	s.Require().Len(service.ActiveEntries(), 1)
}
