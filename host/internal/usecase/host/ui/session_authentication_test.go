package ui

import (
	"context"
	"errors"
	"fmt"

	"strings"
	"sync"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

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
		if frame.Kind == domainui.FrameLifecycle &&
			frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityAuthenticationFailed {
			authFailedOnce.Do(func() { close(authFailed) })
		}
		if frame.Kind == domainui.FrameLifecycle &&
			frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
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
			return testUICommand(domainui.CommandSubmit, mo.Some("blocked")), nil
		case 2:
			return testUICommand(domainui.CommandRetryAuthentication, mo.None[string]()), nil
		default:
			<-ready
			return testUICommand(domainui.CommandQuit, mo.None[string]()), nil
		}
	}).Times(3)

	err := NewSession(
		channel,
		runner,
		authenticator,
		s.modelCatalog,
		nil,
		func(context.Context) {},
	).Run(t.Context(), domainui.Initialization{
		SelectedUIID:   "ui",
		StartupContent: nil,
		Extensions:     nil,
		Availability:   domainui.AvailabilityCheckingAuthentication,
		Models:         nil,
		ModelSelection: mo.Some(domainui.ModelSelection{}),
		SessionInfo:    session.Info{},
	})

	require.NoError(t, err)
	mutex.Lock()
	defer mutex.Unlock()
	assert.True(t, containsRetryableError(frames, oauthFailure.Error()))
	assert.True(t, containsInformation(frames, "not ready"))
	assert.True(t, containsAvailability(frames, domainui.AvailabilityIdle))
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
		if frame.Kind == domainui.FrameLifecycle &&
			frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
			idleCount++
		}
		currentIdleCount := idleCount
		mutex.Unlock()
		if currentIdleCount == 1 {
			initialIdleOnce.Do(func() { close(initialIdle) })
		}
		if currentIdleCount >= 2 ||
			(frame.Kind == domainui.FrameInformation && strings.Contains(frame.Text.MustGet(), "retry is not available")) {
			terminalReadyOnce.Do(func() { close(terminalReady) })
		}
		if frame.Kind == domainui.FrameError && frame.Text.MustGet() == signInRequired.Error() {
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
			return testUICommand(domainui.CommandSubmit, mo.Some("request")), nil
		case 2:
			<-runErrorSent
			return testUICommand(domainui.CommandRetryAuthentication, mo.None[string]()), nil
		default:
			<-terminalReady
			return testUICommand(domainui.CommandQuit, mo.None[string]()), nil
		}
	}).Times(3)

	err := NewSession(
		channel,
		runner,
		authenticator,
		s.modelCatalog,
		nil,
		func(context.Context) {},
	).Run(t.Context(), domainui.Initialization{
		SelectedUIID:   "ui",
		StartupContent: nil,
		Extensions:     nil,
		Availability:   domainui.AvailabilityCheckingAuthentication,
		Models:         nil,
		ModelSelection: mo.Some(domainui.ModelSelection{}),
		SessionInfo:    session.Info{},
	})

	require.NoError(t, err)
	mutex.Lock()
	defer mutex.Unlock()
	errorCount := 0
	for _, frame := range frames {
		if frame.Kind == domainui.FrameError && frame.Text.MustGet() == signInRequired.Error() {
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
	channel.EXPECT().Receive().Return(testUICommand(domainui.CommandQuit, mo.None[string]()), nil)
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	err := NewSession(channel, s.runner, authenticator, s.modelCatalog, nil, func(context.Context) {}).Run(
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

// TestDetailedAuthenticationCheckPrecedesAutomaticSignIn verifies complete check detail reaches the UI first.
func TestDetailedAuthenticationCheckPrecedesAutomaticSignIn(t *testing.T) {
	t.Parallel()

	// Arrange a classified sign-in error with one independent refresh parser cause.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	authenticator := NewMockAuthenticator(controller)
	detailErr := errors.New("unique refresh response parser failure")
	checkErr := fmt.Errorf("stored authentication requires sign-in: %w", detailErr)
	authenticator.EXPECT().IsSignInRequired(checkErr).Return(true)
	signInStarted := make(chan struct{})
	authenticator.EXPECT().SignIn(gomock.Any()).DoAndReturn(func(context.Context) error {
		close(signInStarted)
		return nil
	})
	gomock.InOrder(
		channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
			if assert.Equal(t, domainui.FrameError, frame.Kind) {
				text, present := frame.Text.Get()
				if assert.True(t, present) {
					assert.Contains(t, text, checkErr.Error())
					assert.Contains(t, text, detailErr.Error())
				}
			}
			return nil
		}),
		channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
			if assert.Equal(t, domainui.FrameLifecycle, frame.Kind) {
				assert.Equal(t, domainui.AvailabilityAuthenticating, frame.Lifecycle.MustGet().Availability.MustGet())
			}
			return nil
		}),
	)
	results := make(chan operationResult, 1)
	sessionService := &Session{
		channel: channel, runner: nil, authenticator: authenticator, modelCatalog: nil,
		sessionControl: nil, afterInitialization: nil,
	}

	// Act through the authentication check result.
	availability, cancel, kind, err := sessionService.applyAuthenticationCheck(
		t.Context(), domainui.AvailabilityCheckingAuthentication, checkErr, results,
	)
	<-signInStarted

	// Assert detailed delivery precedes and preserves automatic sign-in behavior.
	require.NoError(t, err)
	assert.Equal(t, domainui.AvailabilityAuthenticating, availability)
	assert.Equal(t, operationSignIn, kind)
	require.NotNil(t, cancel)
	cancel()
}
