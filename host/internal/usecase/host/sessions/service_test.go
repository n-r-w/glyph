package sessions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
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
	s.Equal(session.ID("created-id"), created.ID)
	s.False(created.Name.IsPresent())
	s.False(created.StoragePath.IsPresent())
	s.Equal(createdAt, created.CreatedAt)
	created.Name = mo.Some("caller mutation")

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
	entries := []session.Entry{{
		ID: "entry-id", CreatedAt: createdAt.Add(time.Minute),
		Information: mo.Some(session.Information{Name: "stored name"}),
	}}
	s.repository.EXPECT().Load(gomock.Any(), session.ID("stored-id")).Return(LoadedSession{
		Header:      session.Header{Version: 1, ID: "stored-id", CreatedAt: createdAt, WorkingDirectory: "/project"},
		StoragePath: "/sessions/stored.jsonl",
		Entries:     entries,
	}, nil)
	service := New(s.repository, s.ids, s.clock, "/project")

	info, err := service.ResumeActive(s.T().Context(), "stored-id")
	s.Require().NoError(err)
	entries[0].Information = mo.Some(session.Information{Name: "mutated source"})
	info.Name = mo.Some("mutated result")
	active := service.ActiveInfo()
	s.Equal(session.ID("stored-id"), active.ID)
	s.Equal(mo.Some("stored name"), active.Name)
	s.Equal(mo.Some("/sessions/stored.jsonl"), active.StoragePath)
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
