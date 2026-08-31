//go:build !integration

package sessions

import (
	"errors"
	"time"

	"github.com/samber/mo"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"

	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestSetNameMutationFailureMakesOnlyActiveSessionWriteUnavailable verifies snapshot preservation and local write
// blocking.
func (s *ServiceSuite) TestSetNameMutationFailureMakesOnlyActiveSessionWriteUnavailable() {
	// Arrange one successful name mutation followed by one failed mutation.
	createdAt := time.Date(2026, 8, 26, 20, 0, 0, 0, time.UTC)
	s.repository.EXPECT().Initialize(gomock.Any()).Return(nil)
	s.ids.EXPECT().NewID().Return("session-id", nil)
	s.clock.EXPECT().Now().Return(createdAt)
	gomock.InOrder(
		s.clock.EXPECT().Now().Return(createdAt.Add(time.Minute)),
		s.repository.EXPECT().
			Apply(gomock.Any(), gomock.Any()).
			Return(ApplyResult{StoragePath: "/sessions/file.jsonl"}, nil),
		s.clock.EXPECT().Now().Return(createdAt.Add(2*time.Minute)),
		s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(ApplyResult{}, errors.New("write failed")),
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
	s.Empty(service.ActiveEntries())
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
	s.repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(ApplyResult{}, errors.New("disk failed"))
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
		Header: session.Header{
			Version:          1,
			ID:               "stored",
			CreatedAt:        resumedAt,
			WorkingDirectory: "/project",
		},
		StoragePath:          "/sessions/stored.jsonl",
		Tree:                 session.Tree{},
		Information:          mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}, nil)
	_, err = service.ResumeActive(s.T().Context(), "stored")
	s.Require().NoError(err)
	s.ids.EXPECT().NewID().Return("resumed-entry", nil)
	s.clock.EXPECT().Now().Return(resumedAt.Add(time.Second))
	s.repository.EXPECT().
		Apply(gomock.Any(), gomock.Any()).
		Return(ApplyResult{StoragePath: "/sessions/stored.jsonl"}, nil)
	err = service.Append(s.T().Context(), agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("resumed write")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	})

	// Assert successful resume restores mutation access and advances only the resumed snapshot.
	s.Require().NoError(err)
	s.Equal(session.ID("stored"), service.ActiveInfo().ID)
	s.Require().Len(service.ActiveEntries(), 1)
}
