package ui

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// sessionContextKey isolates context-propagation evidence within this test package.
type sessionContextKey struct{}

// SessionSuite shares channel, runner, and authenticator mocks across lifecycle scenarios.
type SessionSuite struct {
	suite.Suite
	channel       *MockChannel
	runner        *MockAgentRunner
	authenticator *MockAuthenticator
}

// SetupTest creates isolated session boundaries for each lifecycle scenario.
func (s *SessionSuite) SetupTest() {
	controller := gomock.NewController(s.T())
	s.channel = NewMockChannel(controller)
	s.runner = NewMockAgentRunner(controller)
	s.authenticator = NewMockAuthenticator(controller)
}

// TestSessionInitializationDeliveryFailureSkipsActivation starts no Host work before initialization succeeds.
func (s *SessionSuite) TestSessionInitializationDeliveryFailureSkipsActivation() {
	t := s.T()

	s.channel.EXPECT().Send(gomock.Any()).Return(io.ErrClosedPipe)
	activated := false

	err := NewSession(s.channel, s.runner, s.authenticator, func(context.Context) {
		activated = true
	}).Run(t.Context(), domainui.Initialization{
		SelectedUIID: "test-ui", StartupContent: nil, Extensions: nil,
		Availability: domainui.AvailabilityCheckingAuthentication,
	})

	require.ErrorIs(t, err, io.ErrClosedPipe)
	assert.False(t, activated)
}

// TestSessionReadyRunAndQuit verifies initialization, ready submission, completion, and termination order.
func (s *SessionSuite) TestSessionReadyRunAndQuit() {
	t := s.T()

	// Arrange: return one submit command and withhold quit until the run completes.
	channel := s.channel
	runner := s.runner
	authenticator := s.authenticator
	initialization := domainui.Initialization{
		SelectedUIID: "test-ui", StartupContent: nil, Extensions: nil,
		Availability: domainui.AvailabilityCheckingAuthentication,
	}
	var mutex sync.Mutex
	var readyOnce sync.Once
	var idleOnce sync.Once
	ready := make(chan struct{})
	idleAfterRun := make(chan struct{})
	frames := make([]domainui.Frame, 0, 5)
	activationOrder := make([]string, 0, 2)
	contextKey := sessionContextKey{}
	runContext := context.WithValue(t.Context(), contextKey, "session-value")
	var activationValue any
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		mutex.Lock()
		frames = append(frames, frame)
		if frame.Kind == domainui.FrameInitialization {
			activationOrder = append(activationOrder, "initialization")
		}
		frameCount := len(frames)
		mutex.Unlock()
		if frame.Lifecycle.Availability == domainui.AvailabilityIdle {
			if frameCount == 2 {
				readyOnce.Do(func() { close(ready) })
			} else if frameCount > 2 {
				idleOnce.Do(func() { close(idleAfterRun) })
			}
		}
		return nil
	}).AnyTimes()
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)
	runner.EXPECT().Run(gomock.Any(), "first request").DoAndReturn(
		func(_ context.Context, _ string) (agent.RunOutcome, error) {
			return agent.RunOutcomeCompleted, nil
		},
	)
	commandCall := 0
	channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
		commandCall++
		if commandCall == 1 {
			<-ready
			return domainui.Command{Kind: domainui.CommandSubmit, Text: "first request"}, nil
		}
		<-idleAfterRun
		return domainui.Command{Kind: domainui.CommandQuit, Text: ""}, nil
	}).Times(2)

	// Act: run the complete UI session.
	err := NewSession(channel, runner, authenticator, func(ctx context.Context) {
		mutex.Lock()
		activationOrder = append(activationOrder, "activated")
		activationValue = ctx.Value(contextKey)
		mutex.Unlock()
	}).Run(runContext, initialization)

	// Assert: initialization is first and state reaches idle, running, then idle before quit.
	require.NoError(t, err)
	mutex.Lock()
	defer mutex.Unlock()
	require.Len(t, frames, 4)
	assert.Equal(t, []string{"initialization", "activated"}, activationOrder)
	assert.Equal(t, "session-value", activationValue)
	assert.Equal(t, domainui.FrameInitialization, frames[0].Kind)
	assert.Equal(t, domainui.AvailabilityIdle, frames[1].Lifecycle.Availability)
	assert.Equal(t, domainui.AvailabilityRunning, frames[2].Lifecycle.Availability)
	assert.Equal(t, domainui.AvailabilityIdle, frames[3].Lifecycle.Availability)
}

// TestSessionOAuthFailureRequiresExplicitRetry verifies failed authentication never retries automatically.
func (s *SessionSuite) TestSessionOAuthFailureRequiresExplicitRetry() {
	t := s.T()

	channel := s.channel
	runner := s.runner
	authenticator := s.authenticator
	needsSignIn := errors.New("sign-in required")
	oauthFailure := errors.New("OAuth failed")
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(needsSignIn)
	authenticator.EXPECT().IsSignInRequired(needsSignIn).Return(true)
	gomock.InOrder(
		authenticator.EXPECT().SignIn(gomock.Any()).Return(oauthFailure),
		authenticator.EXPECT().SignIn(gomock.Any()).Return(nil),
	)
	var mutex sync.Mutex
	frames := make([]domainui.Frame, 0, 10)
	authFailed := make(chan struct{})
	ready := make(chan struct{})
	var authFailedOnce sync.Once
	var readyOnce sync.Once
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		mutex.Lock()
		frames = append(frames, frame)
		mutex.Unlock()
		if frame.Lifecycle.Availability == domainui.AvailabilityAuthenticationFailed {
			authFailedOnce.Do(func() { close(authFailed) })
		}
		if frame.Lifecycle.Availability == domainui.AvailabilityIdle {
			readyOnce.Do(func() { close(ready) })
		}
		return nil
	}).AnyTimes()
	commandCall := 0
	channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
		commandCall++
		switch commandCall {
		case 1:
			<-authFailed
			return domainui.Command{Kind: domainui.CommandSubmit, Text: "blocked"}, nil
		case 2:
			return domainui.Command{Kind: domainui.CommandRetryAuthentication, Text: ""}, nil
		default:
			<-ready
			return domainui.Command{Kind: domainui.CommandQuit, Text: ""}, nil
		}
	}).Times(3)

	err := NewSession(channel, runner, authenticator, func(context.Context) {}).Run(t.Context(), domainui.Initialization{
		SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
		Availability: domainui.AvailabilityCheckingAuthentication,
	})

	require.NoError(t, err)
	mutex.Lock()
	defer mutex.Unlock()
	assert.True(t, containsRetryableError(frames, oauthFailure.Error()))
	assert.True(t, containsInformation(frames, "not ready"))
	assert.True(t, containsAvailability(frames, domainui.AvailabilityIdle))
}

// TestSessionRejectsBusySubmissionAndStopsActiveRun verifies no queue or parallel run exists.
func (s *SessionSuite) TestSessionRejectsBusySubmissionAndStopsActiveRun() {
	t := s.T()

	channel := s.channel
	runner := s.runner
	authenticator := s.authenticator
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)
	ready := make(chan struct{})
	runStarted := make(chan struct{})
	idleAfterStop := make(chan struct{})
	var readyOnce sync.Once
	var stoppedOnce sync.Once
	var mutex sync.Mutex
	frames := make([]domainui.Frame, 0, 10)
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		mutex.Lock()
		frames = append(frames, frame)
		frameCount := len(frames)
		mutex.Unlock()
		if frame.Lifecycle.Availability == domainui.AvailabilityIdle {
			readyOnce.Do(func() { close(ready) })
			if frameCount > 3 {
				stoppedOnce.Do(func() { close(idleAfterStop) })
			}
		}
		return nil
	}).AnyTimes()
	runner.EXPECT().Run(gomock.Any(), "first").DoAndReturn(
		func(ctx context.Context, _ string) (agent.RunOutcome, error) {
			close(runStarted)
			<-ctx.Done()
			return agent.RunOutcomeAborted, ctx.Err()
		},
	)
	commandCall := 0
	channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
		commandCall++
		switch commandCall {
		case 1:
			<-ready
			return domainui.Command{Kind: domainui.CommandSubmit, Text: "first"}, nil
		case 2:
			<-runStarted
			return domainui.Command{Kind: domainui.CommandSubmit, Text: "queued"}, nil
		case 3:
			return domainui.Command{Kind: domainui.CommandStop, Text: ""}, nil
		default:
			<-idleAfterStop
			return domainui.Command{Kind: domainui.CommandQuit, Text: ""}, nil
		}
	}).Times(4)

	err := NewSession(channel, runner, authenticator, func(context.Context) {}).Run(t.Context(), domainui.Initialization{
		SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
		Availability: domainui.AvailabilityCheckingAuthentication,
	})

	require.NoError(t, err)
	mutex.Lock()
	defer mutex.Unlock()
	assert.True(t, containsInformation(frames, "not ready"))
	assert.True(t, containsAvailability(frames, domainui.AvailabilityRunning))
	assert.True(t, containsAvailability(frames, domainui.AvailabilityIdle))
}

// TestSessionRunsMultipleTurnsThroughTheSameRunner verifies retained Agent Core ownership across requests.
func (s *SessionSuite) TestSessionRunsMultipleTurnsThroughTheSameRunner() {
	t := s.T()

	channel := s.channel
	runner := s.runner
	authenticator := s.authenticator
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)
	gomock.InOrder(
		runner.EXPECT().Run(gomock.Any(), "first").Return(agent.RunOutcomeCompleted, nil),
		runner.EXPECT().Run(gomock.Any(), "second").Return(agent.RunOutcomeCompleted, nil),
	)
	idleSignals := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	idleCount := 0
	var mutex sync.Mutex
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		if frame.Lifecycle.Availability == domainui.AvailabilityIdle {
			mutex.Lock()
			index := idleCount
			idleCount++
			mutex.Unlock()
			close(idleSignals[index])
		}
		return nil
	}).AnyTimes()
	commandCall := 0
	channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
		index := commandCall
		commandCall++
		<-idleSignals[index]
		switch index {
		case 0:
			return domainui.Command{Kind: domainui.CommandSubmit, Text: "first"}, nil
		case 1:
			return domainui.Command{Kind: domainui.CommandSubmit, Text: "second"}, nil
		default:
			return domainui.Command{Kind: domainui.CommandQuit, Text: ""}, nil
		}
	}).Times(3)

	err := NewSession(channel, runner, authenticator, func(context.Context) {}).Run(t.Context(), domainui.Initialization{
		SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
		Availability: domainui.AvailabilityCheckingAuthentication,
	})

	require.NoError(t, err)
	assert.Equal(t, 3, idleCount)
}

// TestSessionSignInRequiredRunWaitsForExplicitAuthenticationRetry verifies revoked-token recovery.
func (s *SessionSuite) TestSessionSignInRequiredRunWaitsForExplicitAuthenticationRetry() {
	t := s.T()

	channel := s.channel
	runner := s.runner
	authenticator := s.authenticator
	signInRequired := errors.New("sign-in required")
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)
	authenticator.EXPECT().IsSignInRequired(signInRequired).Return(true)
	authenticator.EXPECT().SignIn(gomock.Any()).Return(nil)
	runner.EXPECT().Run(gomock.Any(), "request").Return(agent.RunOutcomeFailed, signInRequired)
	var mutex sync.Mutex
	frames := make([]domainui.Frame, 0, 8)
	initialIdle := make(chan struct{})
	runErrorSent := make(chan struct{})
	terminalReady := make(chan struct{})
	var initialIdleOnce sync.Once
	var runErrorOnce sync.Once
	var terminalReadyOnce sync.Once
	idleCount := 0
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		mutex.Lock()
		frames = append(frames, frame)
		if frame.Lifecycle.Availability == domainui.AvailabilityIdle {
			idleCount++
		}
		currentIdleCount := idleCount
		mutex.Unlock()
		if currentIdleCount == 1 {
			initialIdleOnce.Do(func() { close(initialIdle) })
		}
		if currentIdleCount >= 2 || (frame.Kind == domainui.FrameInformation && strings.Contains(frame.Text, "retry is not available")) {
			terminalReadyOnce.Do(func() { close(terminalReady) })
		}
		if frame.Kind == domainui.FrameError && frame.Text == signInRequired.Error() {
			runErrorOnce.Do(func() { close(runErrorSent) })
		}
		return nil
	}).AnyTimes()
	commandCall := 0
	channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
		commandCall++
		switch commandCall {
		case 1:
			<-initialIdle
			return domainui.Command{Kind: domainui.CommandSubmit, Text: "request"}, nil
		case 2:
			<-runErrorSent
			return domainui.Command{Kind: domainui.CommandRetryAuthentication, Text: ""}, nil
		default:
			<-terminalReady
			return domainui.Command{Kind: domainui.CommandQuit, Text: ""}, nil
		}
	}).Times(3)

	err := NewSession(channel, runner, authenticator, func(context.Context) {}).Run(t.Context(), domainui.Initialization{
		SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
		Availability: domainui.AvailabilityCheckingAuthentication,
	})

	require.NoError(t, err)
	mutex.Lock()
	defer mutex.Unlock()
	errorCount := 0
	for _, frame := range frames {
		if frame.Kind == domainui.FrameError && frame.Text == signInRequired.Error() {
			errorCount++
		}
	}
	assert.Equal(t, 1, errorCount)
	assert.True(t, containsAvailability(frames, domainui.AvailabilityAuthenticationFailed))
	assert.True(t, containsAvailability(frames, domainui.AvailabilityAuthenticating))
	assert.Equal(t, 2, idleCount)
}

// TestSessionImmediateQuitCancelsAuthenticationCheck verifies command-first termination.
func (s *SessionSuite) TestSessionImmediateQuitCancelsAuthenticationCheck() {
	t := s.T()

	channel := s.channel
	authenticator := s.authenticator
	channel.EXPECT().Send(gomock.Any()).Return(nil)
	channel.EXPECT().Receive().Return(domainui.Command{Kind: domainui.CommandQuit, Text: ""}, nil)
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	err := NewSession(channel, s.runner, authenticator, func(context.Context) {}).Run(
		t.Context(),
		domainui.Initialization{
			SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
			Availability: domainui.AvailabilityCheckingAuthentication,
		},
	)

	require.NoError(t, err)
}

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
		return domainui.Command{Kind: domainui.CommandQuit, Text: ""}, nil
	})
	s.authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)

	err := NewSession(channel, s.runner, s.authenticator, func(context.Context) {}).Run(
		t.Context(),
		domainui.Initialization{
			SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
			Availability: domainui.AvailabilityCheckingAuthentication,
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
		return domainui.Command{Kind: domainui.CommandQuit, Text: ""}, nil
	}).AnyTimes()
	s.authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)

	err := NewSession(channel, s.runner, s.authenticator, func(context.Context) {}).Run(
		t.Context(),
		domainui.Initialization{
			SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
			Availability: domainui.AvailabilityCheckingAuthentication,
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
		if frame.Lifecycle.Availability == domainui.AvailabilityIdle {
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
			return domainui.Command{Kind: domainui.CommandSubmit, Text: "request"}, nil
		}
		<-runStarted
		return domainui.Command{}, io.ErrUnexpectedEOF
	}).Times(2)

	err := NewSession(channel, runner, authenticator, func(context.Context) {}).Run(t.Context(), domainui.Initialization{
		SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
		Availability: domainui.AvailabilityCheckingAuthentication,
	})

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	select {
	case <-runStopped:
	default:
		assert.Fail(t, "active run was not awaited before stream failure returned")
	}
}

// TestSessionSuite runs isolated UI session lifecycle scenarios.
func TestSessionSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SessionSuite))
}

// containsRetryableError reports whether one retryable error frame matches text.
func containsRetryableError(frames []domainui.Frame, text string) bool {
	for _, frame := range frames {
		if frame.Kind == domainui.FrameError && frame.RetryAuthentication && frame.Text == text {
			return true
		}
	}
	return false
}

// containsInformation reports whether one information frame contains text.
func containsInformation(frames []domainui.Frame, text string) bool {
	for _, frame := range frames {
		if frame.Kind == domainui.FrameInformation && strings.Contains(frame.Text, text) {
			return true
		}
	}
	return false
}

// containsAvailability reports whether one lifecycle frame carries availability.
func containsAvailability(frames []domainui.Frame, availability domainui.Availability) bool {
	for _, frame := range frames {
		if frame.Kind == domainui.FrameLifecycle && frame.Lifecycle.Availability == availability {
			return true
		}
	}
	return false
}
