//go:build !integration

package ui

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestSubmitPreparationReservesRunnerBeforeAcceptance verifies submit admission and execution ownership.
func TestSubmitPreparationReservesRunnerBeforeAcceptance(t *testing.T) {
	t.Parallel()
	// Arrange controller, channel, and runner for service.Prepare to verify submit admission and execution ownership.

	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	runner := NewMockAgentRunner(controller)
	authenticator := NewMockAuthenticator(controller)
	runner.EXPECT().PrepareRun().Return("run", nil)
	channel.EXPECT().BindProgress(gomock.Any()).Return(func() {})
	channel.EXPECT().Send(gomock.Any()).Times(2).Return(nil)
	runner.EXPECT().RunPrepared(gomock.Any(), "run", "hello").Return(agent.RunOutcomeCompleted, nil)
	runner.EXPECT().CancelPrepared("run")
	service := NewSession(
		channel, runner, authenticator, NewMockModelCatalog(controller), nil, func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityIdle)
	command := newCommandForPreparedTest(domainui.CommandSubmit)
	command.OperationID = "operation"
	command.Text = mo.Some("hello")

	// Act by invoking service.Prepare to exercise submit admission and execution ownership.
	prepared, err := service.Prepare(t.Context(), command)
	// Assert submit admission and execution ownership.
	require.NoError(t, err)
	outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
	prepared.Release()

	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
	frame, ok := outcome.Result()
	require.True(t, ok)
	assert.Equal(t, domainui.FrameSubmitCompleted, frame.Kind)
	assert.Equal(t, domainui.AvailabilityIdle, service.operationAvailabilitySnapshot())
}

// TestSubmitAvailabilityDeliveryFailureStopsRun verifies connection delivery failure propagation.
func TestSubmitAvailabilityDeliveryFailureStopsRun(t *testing.T) {
	t.Parallel()

	// Arrange an admitted submit whose first connection event cannot be queued.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	runner := NewMockAgentRunner(controller)
	source := errors.New("deliver running availability failed")
	runner.EXPECT().PrepareRun().Return("run", nil)
	channel.EXPECT().BindProgress(gomock.Any()).Return(func() {})
	channel.EXPECT().Send(gomock.Any()).Return(source)
	channel.EXPECT().Send(gomock.Any()).Return(nil)
	runner.EXPECT().CancelPrepared("run")
	service := NewSession(
		channel, runner, NewMockAuthenticator(controller), NewMockModelCatalog(controller), nil,
		func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityIdle)
	command := newCommandForPreparedTest(domainui.CommandSubmit)
	command.Text = mo.Some("hello")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)

	// Act through prepared execution and release.
	outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
	prepared.Release()

	// Assert the source cause stops agent execution and reaches the failed outcome.
	assert.Equal(t, operation.TerminalStateFailed, outcome.State())
	assert.ErrorIs(t, outcome.Err(), source)
}

// TestSubmitPreparationRejectsBusyRunner verifies busy is decided before acceptance.
func TestSubmitPreparationRejectsBusyRunner(t *testing.T) {
	t.Parallel()
	// Arrange controller, runner, and service for service.Prepare to verify busy is decided before acceptance.

	controller := gomock.NewController(t)
	runner := NewMockAgentRunner(controller)
	runner.EXPECT().PrepareRun().Return("", session.ErrBusy)
	service := NewSession(
		NewMockChannel(controller), runner, NewMockAuthenticator(controller), NewMockModelCatalog(controller), nil,
		func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityIdle)
	command := newCommandForPreparedTest(domainui.CommandSubmit)
	command.Text = mo.Some("hello")

	// Act by invoking service.Prepare to exercise busy is decided before acceptance.
	_, err := service.Prepare(t.Context(), command)

	var rejection *PreparationError
	// Assert busy is decided before acceptance.
	require.ErrorAs(t, err, &rejection)
	assert.Equal(t, rejectionCodeBusy, rejection.Code())
	assert.ErrorIs(t, err, session.ErrBusy)
}

// TestSubmitFailurePreservesCauseAndAuthenticationAvailability verifies failed run terminal semantics.
func TestSubmitFailurePreservesCauseAndAuthenticationAvailability(t *testing.T) {
	t.Parallel()
	// Arrange controller, channel, and runner for Prepared.Run to verify failed run terminal semantics.

	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	runner := NewMockAgentRunner(controller)
	authenticator := NewMockAuthenticator(controller)
	source := errors.New("credentials expired")
	runner.EXPECT().PrepareRun().Return("run", nil)
	channel.EXPECT().BindProgress(gomock.Any()).Return(func() {})
	channel.EXPECT().Send(gomock.Any()).Times(2).Return(nil)
	runner.EXPECT().RunPrepared(gomock.Any(), "run", "hello").Return(agent.RunOutcomeCompleted, source)
	runner.EXPECT().CancelPrepared("run")
	authenticator.EXPECT().IsSignInRequired(source).Return(true)
	service := NewSession(
		channel, runner, authenticator, NewMockModelCatalog(controller), nil, func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityIdle)
	command := newCommandForPreparedTest(domainui.CommandSubmit)
	command.OperationID = "operation"
	command.Text = mo.Some("hello")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)

	// Act by invoking Prepared.Run to exercise failed run terminal semantics.
	outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
	prepared.Release()

	// Assert failed run terminal semantics.
	assert.Equal(t, operation.TerminalStateFailed, outcome.State())
	assert.ErrorIs(t, outcome.Err(), source)
	assert.Equal(t, domainui.AvailabilityAuthenticationFailed, service.operationAvailabilitySnapshot())
}
