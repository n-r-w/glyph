//go:build !integration

package operationgate_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticcontroller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/operationgate"
	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

// TestRunReservationsBlockReplacementUntilSettlement verifies run ownership blocks replacement until settlement.
func TestRunReservationsBlockReplacementUntilSettlement(t *testing.T) {
	t.Parallel()

	// Arrange active-session control and run coordination over one operation gate.
	controller := gomock.NewController(t)
	active := sessioncontrol.NewMockActiveSessions(controller)
	navigator := sessioncontrol.NewMockNavigator(controller)
	gate := operationgate.New()
	control := sessioncontrol.New(active, navigator, gate)
	idleInfo := session.Info{
		ID: "idle", Name: mo.None[string](), WorkingDirectory: "/project", StoragePath: mo.None[string](),
		CreatedAt: time.Time{}, UpdatedAt: time.Time{},
	}
	active.EXPECT().CreateActive(gomock.Any()).Return(session.Replacement{Info: idleInfo, Entries: nil}, nil)

	// Act by creating the initial idle session.
	created, err := control.Create(t.Context())

	// Assert replacement succeeds while no run owns the gate.
	require.NoError(t, err)
	require.Equal(t, idleInfo.ID, created.Info.ID)

	started := make(chan struct{})
	settle := make(chan struct{})
	coordinator := events.NewCoordinator(func(context.Context, agentrun.Request) (agentrun.Result, error) {
		close(started)
		<-settle
		return agentrun.Result{
			Outcome:      agent.RunOutcomeCompleted,
			AddedHistory: nil,
			ErrorMessage: mo.None[string](),
		}, nil
	}, nil, nil, gate)
	// Act by reserving a run and attempting replacement before the run starts.
	runID, err := coordinator.PrepareRun()
	require.NoError(t, err)
	_, err = control.Create(t.Context())

	// Assert the prepared reservation blocks creation.
	require.ErrorIs(t, err, session.ErrBusy)

	runResult := make(chan error, 1)
	go func() {
		_, runErr := coordinator.RunPrepared(t.Context(), runID, "request")
		runResult <- runErr
	}()
	<-started

	// Act by attempting resume during execution and then settling the run.
	_, err = control.Resume(t.Context(), "stored")

	// Assert the active run blocks resume until settlement completes.
	require.ErrorIs(t, err, session.ErrBusy)
	close(settle)
	require.NoError(t, <-runResult)

	resumedInfo := session.Info{
		ID:               "stored",
		Name:             mo.Some("stored"),
		WorkingDirectory: "/project",
		StoragePath:      mo.Some("/sessions/stored.jsonl"),
		CreatedAt:        time.Time{},
		UpdatedAt:        time.Time{},
	}
	active.EXPECT().ResumeActive(gomock.Any(), session.ID("stored")).Return(
		session.Replacement{Info: resumedInfo, Entries: nil}, nil,
	)

	// Act by resuming after settlement releases run ownership.
	resumed, err := control.Resume(t.Context(), "stored")

	// Assert resume succeeds with the stored session identity.
	require.NoError(t, err)
	require.Equal(t, resumedInfo.ID, resumed.Info.ID)
}

// TestProgrammaticSessionCommandsRespectGateBeforeStorage verifies gate checks precede session persistence.
func TestProgrammaticSessionCommandsRespectGateBeforeStorage(t *testing.T) {
	t.Parallel()

	// Arrange programmatic session control with a shared operation gate.
	controller := gomock.NewController(t)
	active := sessioncontrol.NewMockActiveSessions(controller)
	navigator := sessioncontrol.NewMockNavigator(controller)
	gate := operationgate.New()
	control := sessioncontrol.New(active, navigator, gate)
	service := programmatic.New(
		nil, nil, func() agentrun.State { return agentrun.State{} },
		func() []agent.HistoryEntry { return nil }, control, programmatic.NewDelivery(),
	)

	// Act by issuing invalid, busy, query, and post-settlement session commands.
	response, operation, err := service.Handle(t.Context(), programmaticSessionCommand(
		"invalid-resume", programmaticcontroller.CommandResumeSession, mo.None[session.ID](), mo.None[string](),
	))

	// Assert each command observes gate precedence and permitted queries avoid storage replacement.
	require.NoError(t, err)
	require.Nil(t, operation)
	require.Equal(t, programmaticcontroller.RejectionInvalidArgument, response.Rejection.MustGet().Code)
	release, acquired := gate.TryAcquire()
	require.True(t, acquired, "invalid arguments must not acquire the operation gate")
	release()

	started := make(chan struct{})
	settle := make(chan struct{})
	coordinator := events.NewCoordinator(func(context.Context, agentrun.Request) (agentrun.Result, error) {
		close(started)
		<-settle
		return agentrun.Result{
			Outcome: agent.RunOutcomeCompleted, AddedHistory: nil, ErrorMessage: mo.None[string](),
		}, nil
	}, nil, nil, gate)
	runID, err := coordinator.PrepareRun()
	require.NoError(t, err)
	runResult := make(chan error, 1)
	go func() {
		_, runErr := coordinator.RunPrepared(t.Context(), runID, "request")
		runResult <- runErr
	}()
	<-started

	response, operation, err = service.Handle(t.Context(), programmaticSessionCommand(
		"busy-resume", programmaticcontroller.CommandResumeSession, mo.Some(session.ID("stored")), mo.None[string](),
	))
	require.NoError(t, err)
	require.Nil(t, operation)
	require.Equal(t, programmaticcontroller.RejectionBusy, response.Rejection.MustGet().Code)
	response, operation, err = service.Handle(t.Context(), programmaticSessionCommand(
		"busy-create", programmaticcontroller.CommandCreateSession, mo.None[session.ID](), mo.None[string](),
	))
	require.NoError(t, err)
	require.Nil(t, operation)
	require.Equal(t, programmaticcontroller.RejectionBusy, response.Rejection.MustGet().Code)

	info := session.Info{
		ID: "active", Name: mo.Some("renamed"), WorkingDirectory: "/project", StoragePath: mo.None[string](),
		CreatedAt: time.Time{}, UpdatedAt: time.Time{},
	}
	active.EXPECT().SetActiveName(gomock.Any(), "renamed").Return(info, nil)
	active.EXPECT().ListStored(gomock.Any()).Return([]session.Summary{}, nil)
	active.EXPECT().ActiveInfo().Return(info)
	for _, command := range []programmaticcontroller.Command{
		programmaticSessionCommand(
			"name", programmaticcontroller.CommandSetSessionName, mo.None[session.ID](), mo.Some("renamed"),
		),
		programmaticSessionCommand(
			"list", programmaticcontroller.CommandListSessions, mo.None[session.ID](), mo.None[string](),
		),
		programmaticSessionCommand(
			"information", programmaticcontroller.CommandGetSessionInfo, mo.None[session.ID](), mo.None[string](),
		),
	} {
		response, operation, err = service.Handle(t.Context(), command)
		require.NoError(t, err)
		require.Nil(t, operation)
		require.False(t, response.Rejection.IsPresent())
	}

	close(settle)
	require.NoError(t, <-runResult)
	resumedInfo := session.Info{
		ID: "stored", Name: mo.None[string](), WorkingDirectory: "/project", StoragePath: mo.Some("/stored"),
		CreatedAt: time.Time{}, UpdatedAt: time.Time{},
	}
	active.EXPECT().ResumeActive(gomock.Any(), session.ID("stored")).Return(
		session.Replacement{Info: resumedInfo, Entries: nil}, nil,
	)
	response, operation, err = service.Handle(t.Context(), programmaticSessionCommand(
		"resumed", programmaticcontroller.CommandResumeSession, mo.Some(session.ID("stored")), mo.None[string](),
	))
	require.NoError(t, err)
	require.Nil(t, operation)
	require.False(t, response.Rejection.IsPresent())
	require.Equal(t, resumedInfo, response.SessionInfo.MustGet())
}

// TestProgrammaticBusyResumeSkipsRepositoryUntilGateRelease verifies programmatic busy resume skips repository until
// gate release.
func TestProgrammaticBusyResumeSkipsRepositoryUntilGateRelease(t *testing.T) {
	t.Parallel()

	// Arrange a real active-session service behind an acquired operation gate.
	controller := gomock.NewController(t)
	repository := hostsessions.NewMockRepository(controller)
	active := hostsessions.New(
		repository,
		hostsessions.NewMockIDGenerator(controller),
		hostsessions.NewMockClock(controller),
		nil,
		"/project",
	)
	navigator := sessioncontrol.NewMockNavigator(controller)
	gate := operationgate.New()
	service := programmatic.New(
		nil, nil, func() agentrun.State { return agentrun.State{} }, func() []agent.HistoryEntry { return nil },
		sessioncontrol.New(active, navigator, gate), programmatic.NewDelivery(),
	)
	release, acquired := gate.TryAcquire()
	require.True(t, acquired)

	// Act by requesting resume before and after releasing the gate.
	response, operation, err := service.Handle(t.Context(), programmaticSessionCommand(
		"busy", programmaticcontroller.CommandResumeSession, mo.Some(session.ID("stored")), mo.None[string](),
	))

	// Assert the busy request is rejected before repository access.
	require.NoError(t, err)
	require.Nil(t, operation)
	require.Equal(t, programmaticcontroller.RejectionBusy, response.Rejection.MustGet().Code)
	release()

	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	repository.EXPECT().Load(gomock.Any(), session.ID("stored")).Return(hostsessions.LoadedSession{
		Header: session.Header{
			Version: 2, ID: "stored", CreatedAt: createdAt, WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/stored.jsonl", Tree: session.Tree{},
		Information:          mo.Some(session.Information{Name: "stored"}),
		InformationUpdatedAt: mo.Some(createdAt.Add(time.Second)),
	}, nil)
	response, operation, err = service.Handle(t.Context(), programmaticSessionCommand(
		"resumed", programmaticcontroller.CommandResumeSession, mo.Some(session.ID("stored")), mo.None[string](),
	))
	require.NoError(t, err)
	require.Nil(t, operation)
	require.False(t, response.Rejection.IsPresent())
	require.Equal(t, session.ID("stored"), response.SessionInfo.MustGet().ID)
}

func programmaticSessionCommand(
	correlationID string,
	kind programmaticcontroller.CommandKind,
	id mo.Option[session.ID],
	name mo.Option[string],
) programmaticcontroller.Command {
	return programmaticcontroller.Command{
		CorrelationID:   correlationID,
		Kind:            kind,
		UserText:        mo.None[string](),
		ProviderID:      mo.None[model.ProviderID](),
		ModelID:         mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:       id,
		SessionName:     name,
		TargetEntryID:   mo.None[string](),
		SummaryMode:     programmaticcontroller.SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	}
}

// TestAcceptedDeliveryFailureReleasesPreparedRunExactlyOnce verifies cancellation frees the gate once without starting
// the run.
func TestAcceptedDeliveryFailureReleasesPreparedRunExactlyOnce(t *testing.T) {
	t.Parallel()

	// Arrange real gate ownership behind a mock coordinator and an Agent Core start counter.
	controller := gomock.NewController(t)
	gate := operationgate.New()
	var agentStarts atomic.Int32
	realCoordinator := events.NewCoordinator(func(context.Context, agentrun.Request) (agentrun.Result, error) {
		agentStarts.Add(1)
		return agentrun.Result{
			Outcome:      agent.RunOutcomeCompleted,
			AddedHistory: nil,
			ErrorMessage: mo.None[string](),
		}, nil
	}, nil, nil, gate)
	coordinator := programmatic.NewMockCoordinator(controller)
	coordinator.EXPECT().PrepareRun().DoAndReturn(realCoordinator.PrepareRun)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).Do(realCoordinator.CancelPrepared).Times(1)
	coordinator.EXPECT().RunPrepared(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	service := programmatic.New(
		coordinator,
		nil,
		func() agentrun.State { return agentrun.State{} },
		func() []agent.HistoryEntry { return nil },
		nil,
		programmatic.NewDelivery(),
	)

	// Act by accepting a request and canceling its delivery twice.
	response, operation, err := service.Handle(t.Context(), programmaticcontroller.Command{
		CorrelationID:   "accepted",
		Kind:            programmaticcontroller.CommandUserRequest,
		UserText:        mo.Some("request"),
		ProviderID:      mo.None[model.ProviderID](),
		ModelID:         mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:       mo.None[session.ID](),
		SessionName:     mo.None[string](),
		TargetEntryID:   mo.None[string](),
		SummaryMode:     programmaticcontroller.SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	})
	require.NoError(t, err)
	require.Equal(t, programmaticcontroller.ResponseUserRequestAccepted, response.Kind)
	require.NotNil(t, operation)
	require.NoError(t, service.CancelAndWait(t.Context()))
	require.NoError(t, service.CancelAndWait(t.Context()))

	// Assert cancellation is idempotent and Agent Core never starts.
	require.Zero(t, agentStarts.Load())

	active := sessioncontrol.NewMockActiveSessions(controller)
	active.EXPECT().CreateActive(gomock.Any()).Return(session.Replacement{Info: session.Info{
		ID: "next", Name: mo.None[string](), WorkingDirectory: "/project", StoragePath: mo.None[string](),
		CreatedAt: time.Time{}, UpdatedAt: time.Time{},
	}, Entries: nil}, nil)

	// Act by creating a session after cancellation releases the reservation.
	navigator := sessioncontrol.NewMockNavigator(controller)
	created, err := sessioncontrol.New(active, navigator, gate).Create(t.Context())

	// Assert the next replacement acquires the gate and succeeds.
	require.NoError(t, err)
	require.Equal(t, session.ID("next"), created.Info.ID)
}
