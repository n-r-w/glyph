//go:build !integration

package operationgate_test

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
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
	"github.com/n-r-w/glyph/internal/operation"
)

// TestRunReservationsBlockReplacementUntilSettlement verifies run ownership blocks replacement until settlement.
func TestRunReservationsBlockReplacementUntilSettlement(t *testing.T) {
	t.Parallel()

	// Arrange active-session control and run coordination over one operation gate.
	controller := gomock.NewController(t)
	active := sessioncontrol.NewMockActiveSessions(controller)
	navigator := sessioncontrol.NewMockNavigator(controller)
	gate := operationgate.New()
	control := sessioncontrol.New(active, navigator, gate.TryAcquire)
	idleInfo := session.Info{
		ID: "idle", Name: mo.None[string](), WorkingDirectory: "/project", StoragePath: mo.None[string](),
		CreatedAt: time.Time{}, UpdatedAt: time.Time{},
	}
	active.EXPECT().CreateActive(gomock.Any()).Return(session.Replacement{Info: idleInfo, Entries: nil}, nil)
	release, acquired := control.TryAcquire()
	require.True(t, acquired)
	created, err := control.Create(t.Context())
	release()
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
	}, nil, nil, gate.TryAcquire)
	runID, err := coordinator.PrepareRun()
	require.NoError(t, err)
	_, acquired = control.TryAcquire()
	require.False(t, acquired)
	runResult := make(chan error, 1)
	go func() {
		_, runErr := coordinator.RunPrepared(t.Context(), runID, "request")
		runResult <- runErr
	}()
	<-started
	_, acquired = control.TryAcquire()
	require.False(t, acquired)
	close(settle)
	require.NoError(t, <-runResult)

	resumedInfo := session.Info{
		ID: "stored", Name: mo.Some("stored"), WorkingDirectory: "/project",
		StoragePath: mo.Some("/sessions/stored.jsonl"), CreatedAt: time.Time{}, UpdatedAt: time.Time{},
	}
	active.EXPECT().ResumeActive(gomock.Any(), session.ID("stored")).Return(
		session.Replacement{Info: resumedInfo, Entries: nil}, nil,
	)
	release, acquired = control.TryAcquire()
	require.True(t, acquired)
	resumed, err := control.Resume(t.Context(), "stored")
	release()
	require.NoError(t, err)
	require.Equal(t, resumedInfo.ID, resumed.Info.ID)
}

// TestProgrammaticPreparationReservesSessionMutationBeforeStorage verifies bounded gate admission.
func TestProgrammaticPreparationReservesSessionMutationBeforeStorage(t *testing.T) {
	t.Parallel()

	// Arrange a Programmatic service over an occupied shared session gate.
	controller := gomock.NewController(t)
	active := sessioncontrol.NewMockActiveSessions(controller)
	gate := operationgate.New()
	control := sessioncontrol.New(active, sessioncontrol.NewMockNavigator(controller), gate.TryAcquire)
	service := programmatic.New(
		nil, nil, func() agentrun.State { return agentrun.State{} },
		func() []agent.HistoryEntry { return nil }, control, programmatic.NewDelivery(),
	)
	release, acquired := gate.TryAcquire()
	require.True(t, acquired)
	command := programmaticcontroller.Command{
		OperationID: "resume", Kind: programmaticcontroller.CommandResumeSession,
		UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](), SessionID: mo.Some(session.ID("stored")),
		SessionName: mo.None[string](), TargetEntryID: mo.None[string](),
		SummaryMode: programmaticcontroller.SummaryModeNoSummary, CustomFocus: mo.None[string](),
		EntryLabel: mo.None[string](),
	}

	// Act by preparing before and after releasing the shared gate.
	_, busyErr := service.Prepare(t.Context(), command)
	release()
	info := session.Info{
		ID: "stored", Name: mo.None[string](), WorkingDirectory: "/project", StoragePath: mo.Some("stored"),
		CreatedAt: time.Time{}, UpdatedAt: time.Time{},
	}
	active.EXPECT().ResumeActive(gomock.Any(), session.ID("stored")).Return(
		session.Replacement{Info: info, Entries: nil}, nil,
	)
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	outcome := prepared.Run(t.Context(), operation.Reporter[programmaticcontroller.AgentEvent]{})
	prepared.Release()

	// Assert busy is a preparation rejection and admitted work commits only in Run.
	var rejection *programmaticcontroller.RejectionError
	require.ErrorAs(t, busyErr, &rejection)
	assert.Equal(t, programmaticcontroller.RejectionCodeBusy, rejection.Code())
	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
}
