package sessions

import (
	"context"

	"fmt"

	"time"

	"github.com/samber/mo"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

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
	s.clock.EXPECT().Now().Return(updatedAt)
	s.repository.EXPECT().Apply(gomock.Any(), ApplyCommand{
		Header: session.Header{
			Version:          2,
			ID:               "session-id",
			CreatedAt:        createdAt,
			WorkingDirectory: "/project",
		},
		StoragePath: "",
		Mutation: Mutation{
			Entry: mo.None[session.Entry](), Navigation: mo.None[NavigationMutation](),
			Label: mo.None[LabelMutation](), SessionInformation: mo.Some(SessionInformationMutation{
				Name: "release notes", CreatedAt: updatedAt,
			}),
		},
	}).Return(ApplyResult{StoragePath: "/sessions/file.jsonl"}, nil)

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

// TestSetNameUsesSuppliedTimestamps verifies each name update keeps its mutation time.
func (s *ServiceSuite) TestSetNameUsesSuppliedTimestamps() {
	// Arrange two ordered name updates with distinct timestamps.
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	firstUpdate := createdAt.Add(time.Minute)
	secondUpdate := createdAt.Add(2 * time.Minute)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	gomock.InOrder(
		s.clock.EXPECT().Now().Return(firstUpdate),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
				s.Equal(firstUpdate, command.Mutation.SessionInformation.MustGet().CreatedAt)
				return ApplyResult{StoragePath: "/sessions/file.jsonl"}, nil
			},
		),
		s.clock.EXPECT().Now().Return(secondUpdate),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
				s.Equal(secondUpdate, command.Mutation.SessionInformation.MustGet().CreatedAt)
				s.Equal("/sessions/file.jsonl", command.StoragePath)
				return ApplyResult{StoragePath: command.StoragePath}, nil
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
		{Header: session.Header{Version: 1, ID: "older", CreatedAt: base, WorkingDirectory: "/project"}, StoragePath: "/older.jsonl", Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](), Tree: mustSessionTree(nil)},
		{Header: session.Header{Version: 1, ID: "z-id", CreatedAt: base.Add(time.Minute), WorkingDirectory: "/project"}, StoragePath: "/z.jsonl", Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](), Tree: mustSessionTree(nil)},
		{Header: session.Header{Version: 1, ID: "a-id", CreatedAt: base.Add(time.Minute), WorkingDirectory: "/project"}, StoragePath: "/a.jsonl", Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](), Tree: mustSessionTree(nil)},
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

		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](), Tree: mustSessionTree([]session.Entry{
			{ParentID: mo.None[string](), ID: "user", CreatedAt: createdAt.Add(time.Second), Information: mo.None[session.Information](),
				User: mo.Some(model.TextMessage("question")), Model: mo.None[session.ModelResponse](),
				ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
			},
			{ParentID: mo.None[string](), ID: "model", CreatedAt: createdAt.Add(2 * time.Second), Information: mo.None[session.Information](),
				User: mo.None[session.UserMessage](), Model: mo.Some(model.Response{
					Content: nil, Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
					Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
					ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
				}),
				ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
			},
			{ParentID: mo.None[string](), ID: "tool", CreatedAt: createdAt.Add(3 * time.Second), Information: mo.None[session.Information](),
				User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "read", Contents: tool.TextContents("result"), IsError: false,
				}),
				Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
			},
		}),
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
	// Arrange a stored session with caller-owned session information.
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Load(gomock.Any(), session.ID("stored-id")).Return(LoadedSession{
		Header:      session.Header{Version: 2, ID: "stored-id", CreatedAt: createdAt, WorkingDirectory: "/project"},
		StoragePath: "/sessions/stored.jsonl", Tree: mustSessionTree(nil),
		Information:          mo.Some(session.Information{Name: "stored name"}),
		InformationUpdatedAt: mo.Some(createdAt.Add(time.Minute)),
	}, nil)
	service := New(s.repository, s.ids, s.clock, s.pricing, "/project")

	// Act by resuming and mutating source and returned replacement values.
	replacement, err := service.ResumeActive(s.T().Context(), "stored-id")
	s.Require().NoError(err)
	s.Empty(replacement.Entries)
	replacement.Info.Name = mo.Some("mutated result")
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
	entries := []session.Entry{{ParentID: mo.None[string](), ID: "extension-entry", CreatedAt: createdAt, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](),
		Extension: mo.Some(session.ExtensionEnvelope{
			ExtensionID: "example.extension", EntryType: "checkpoint", Data: repositoryBytes,
		}), EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}}
	s.repository.EXPECT().Load(gomock.Any(), session.ID("stored-id")).Return(LoadedSession{
		Header: session.Header{
			Version: 1, ID: "stored-id", CreatedAt: createdAt, WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/stored.jsonl", Tree: mustSessionTree(entries),
		Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
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
				Tree: mustSessionTree([]session.Entry{{
					ParentID: mo.None[string](), ID: "stored-entry", CreatedAt: createdAt,
					Information: mo.None[session.Information](), User: mo.Some(model.TextMessage("stored text")),
					Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
					Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
					BranchSummary: mo.None[session.BranchSummaryEntry](),
				}}),
				Information: mo.None[session.Information](), InformationUpdatedAt: mo.None[time.Time](),
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
	var appendCommand ApplyCommand
	s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command ApplyCommand) (ApplyResult, error) {
			appendCommand = command
			return ApplyResult{StoragePath: "/sessions/stored.jsonl"}, nil
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
