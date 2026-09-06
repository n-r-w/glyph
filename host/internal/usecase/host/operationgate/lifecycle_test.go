//go:build integration

package operationgate_test

import (
	"context"
	"testing"
	"time"

	programmaticoutput "github.com/n-r-w/glyph/host/internal/infra/programmatic/output"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticcontroller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/operationgate"
	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
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
	control := sessioncontrol.New(active, navigator)
	idleInfo := session.Info{
		ID: "idle", Name: mo.None[string](), WorkingDirectory: "/project", StoragePath: mo.None[string](),
		CreatedAt: time.Time{}, UpdatedAt: time.Time{},
	}
	active.EXPECT().CreateActive(gomock.Any()).Return(session.Replacement{Info: idleInfo, Entries: nil}, nil)
	release, acquired := gate.TryAcquire()
	require.True(t, acquired)
	created, err := control.Create(t.Context())
	release()
	require.NoError(t, err)
	require.Equal(t, idleInfo.ID, created.Info.ID)

	started := make(chan struct{})
	settle := make(chan struct{})

	executor := runcontrol.NewMockExecutor(controller)
	client := events.NewMockClientDelivery(controller)
	observer := events.NewMockObserver(controller)
	executor.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, runcontrol.Request) (runcontrol.Result, error) {
			close(started)
			<-settle
			return runcontrol.Result{
				Outcome:            agent.RunOutcomeCompleted,
				SettlementRequired: true,
			}, nil
		})
	gomock.InOrder(
		executor.EXPECT().Settle(gomock.Any()).DoAndReturn(func(string) error {
			_, acquired := gate.TryAcquire()
			require.False(t, acquired, "Core settlement must retain admission")
			return nil
		}),
		client.EXPECT().DeliverSettled(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, string) error {
			_, acquired := gate.TryAcquire()
			require.False(t, acquired, "client settlement must retain admission")
			return nil
		}),
		observer.EXPECT().ObserveSettled(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, string) error {
			_, acquired := gate.TryAcquire()
			require.False(t, acquired, "settlement observers must retain admission")
			return nil
		}),
	)
	coordinator := runcontrol.NewCoordinator(executor, events.NewDispatcher(client, observer), gate)
	runID, err := coordinator.PrepareRun()
	require.NoError(t, err)
	_, acquired = gate.TryAcquire()
	require.False(t, acquired)
	runResult := make(chan error, 1)
	go func() {
		_, runErr := coordinator.RunPrepared(t.Context(), runID, "request")
		runResult <- runErr
	}()
	<-started
	_, acquired = gate.TryAcquire()
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
	release, acquired = gate.TryAcquire()
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
	control := sessioncontrol.New(active, sessioncontrol.NewMockNavigator(controller))
	service := programmatic.New(
		nil, nil, programmatic.NewMockStateQuery(controller),
		func() []agent.HistoryEntry { return nil }, control, gate, programmaticoutput.New(),
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
	outcome := prepared.Run(t.Context(), operation.Reporter[programmaticcontroller.OperationProgress]{})
	prepared.Release()

	// Assert busy is a preparation rejection and admitted work commits only in Run.
	var rejection *programmaticcontroller.RejectionError
	require.ErrorAs(t, busyErr, &rejection)
	assert.Equal(t, programmaticcontroller.RejectionCodeBusy, rejection.Code())
	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
}
