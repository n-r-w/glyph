package ui

import (
	"context"

	"io"

	"sync"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestSessionImmediateQuitOwnsTerminalSendEOF verifies result-first termination.
func (s *SessionSuite) TestSessionImmediateQuitOwnsTerminalSendEOF() {
	t := s.T()

	channel := s.channel
	deliveryAttempted := make(chan struct{})
	gomock.InOrder(
		channel.EXPECT().Send(gomock.Any()).Return(nil),
		channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(domainui.Frame) error {
			close(deliveryAttempted)
			return io.EOF
		}),
	)
	channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
		<-deliveryAttempted
		return domainui.Command{
			Kind:            domainui.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	})
	s.authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)

	err := NewSession(channel, s.runner, s.authenticator, s.modelCatalog, nil, func(context.Context) {}).Run(
		t.Context(),
		domainui.Initialization{
			SelectedUIID:   "ui",
			StartupContent: nil,
			Extensions:     nil,
			Availability:   domainui.AvailabilityCheckingAuthentication,
			Models:         nil,
			ModelSelection: mo.Some(domainui.ModelSelection{}),
			SessionInfo:    session.Info{},
		},
	)

	require.NoError(t, err)
}

// TestSessionImmediateQuitDoesNotMaskUnexpectedDeliveryFailure verifies non-terminal send failures remain errors.
func (s *SessionSuite) TestSessionImmediateQuitDoesNotMaskUnexpectedDeliveryFailure() {
	t := s.T()

	channel := s.channel
	deliveryAttempted := make(chan struct{})
	gomock.InOrder(
		channel.EXPECT().Send(gomock.Any()).Return(nil),
		channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(domainui.Frame) error {
			close(deliveryAttempted)
			return io.ErrUnexpectedEOF
		}),
	)
	// Receive is optional because a non-EOF delivery failure may finish the session before the receiver starts.
	channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
		<-deliveryAttempted
		return domainui.Command{
			Kind:            domainui.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	}).AnyTimes()
	s.authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)

	err := NewSession(channel, s.runner, s.authenticator, s.modelCatalog, nil, func(context.Context) {}).Run(
		t.Context(),
		domainui.Initialization{
			SelectedUIID:   "ui",
			StartupContent: nil,
			Extensions:     nil,
			Availability:   domainui.AvailabilityCheckingAuthentication,
			Models:         nil,
			ModelSelection: mo.Some(domainui.ModelSelection{}),
			SessionInfo:    session.Info{},
		},
	)

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

// TestSessionStreamFailureCancelsAndAwaitsActiveRun verifies authoritative termination ordering.
func (s *SessionSuite) TestSessionStreamFailureCancelsAndAwaitsActiveRun() {
	t := s.T()

	channel := s.channel
	runner := s.runner
	authenticator := s.authenticator
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)
	ready := make(chan struct{})
	runStarted := make(chan struct{})
	runStopped := make(chan struct{})
	var readyOnce sync.Once
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		if frame.Kind == domainui.FrameLifecycle && frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
			readyOnce.Do(func() { close(ready) })
		}
		return nil
	}).AnyTimes()
	runner.EXPECT().Run(gomock.Any(), "request").DoAndReturn(
		func(ctx context.Context, _ string) (agent.RunOutcome, error) {
			close(runStarted)
			<-ctx.Done()
			close(runStopped)
			return agent.RunOutcomeAborted, ctx.Err()
		},
	)
	commandCall := 0
	channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
		commandCall++
		if commandCall == 1 {
			<-ready
			return domainui.Command{
				Kind:            domainui.CommandSubmit,
				Text:            mo.Some("request"),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		}
		<-runStarted
		return domainui.Command{}, io.ErrUnexpectedEOF
	}).Times(2)

	err := NewSession(channel, runner, authenticator, s.modelCatalog, nil, func(context.Context) {}).Run(t.Context(), domainui.Initialization{
		SelectedUIID:   "ui",
		StartupContent: nil,
		Extensions:     nil,
		Availability:   domainui.AvailabilityCheckingAuthentication,
		Models:         nil,
		ModelSelection: mo.Some(domainui.ModelSelection{}),
		SessionInfo:    session.Info{},
	})

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	select {
	case <-runStopped:
	default:
		assert.Fail(t, "active run was not awaited before stream failure returned")
	}
}
