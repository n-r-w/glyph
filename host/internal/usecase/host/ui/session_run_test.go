package ui

import (
	"context"

	"sync"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestSessionReadyRunAndQuit verifies initialization, ready submission, completion, and termination order.
func (s *SessionSuite) TestSessionReadyRunAndQuit() {
	t := s.T()

	// Arrange: return one submit command and withhold quit until the run completes.
	channel := s.channel
	runner := s.runner
	authenticator := s.authenticator
	initialization := domainui.Initialization{
		SelectedUIID:   "test-ui",
		StartupContent: nil,
		Extensions:     nil,
		Availability:   domainui.AvailabilityCheckingAuthentication,
		Models:         nil,
		ModelSelection: mo.Some(domainui.ModelSelection{}),
		SessionInfo:    session.Info{},
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
		if frame.Kind == domainui.FrameLifecycle && frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
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
			return domainui.Command{
				Kind:            domainui.CommandSubmit,
				Text:            mo.Some("first request"),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		}
		<-idleAfterRun
		return domainui.Command{
			Kind:            domainui.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, nil
	}).Times(2)

	// Act: run the complete UI session.
	err := NewSession(channel, runner, authenticator, s.modelCatalog, nil, func(ctx context.Context) {
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
	assert.Equal(t, domainui.AvailabilityIdle, frames[1].Lifecycle.MustGet().Availability.MustGet())
	assert.Equal(t, domainui.AvailabilityRunning, frames[2].Lifecycle.MustGet().Availability.MustGet())
	assert.Equal(t, domainui.AvailabilityIdle, frames[3].Lifecycle.MustGet().Availability.MustGet())
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
		if frame.Kind == domainui.FrameLifecycle && frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
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
			return domainui.Command{
				Kind:            domainui.CommandSubmit,
				Text:            mo.Some("first"),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		case 2:
			<-runStarted
			return domainui.Command{
				Kind:            domainui.CommandSubmit,
				Text:            mo.Some("queued"),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		case 3:
			return domainui.Command{
				Kind:            domainui.CommandStop,
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		default:
			<-idleAfterStop
			return domainui.Command{
				Kind:            domainui.CommandQuit,
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		}
	}).Times(4)

	err := NewSession(channel, runner, authenticator, s.modelCatalog, nil, func(context.Context) {}).Run(t.Context(), domainui.Initialization{
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
		if frame.Kind == domainui.FrameLifecycle && frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
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
			return domainui.Command{
				Kind:            domainui.CommandSubmit,
				Text:            mo.Some("first"),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		case 1:
			return domainui.Command{
				Kind:            domainui.CommandSubmit,
				Text:            mo.Some("second"),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		default:
			return domainui.Command{
				Kind:            domainui.CommandQuit,
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		}
	}).Times(3)

	err := NewSession(channel, runner, authenticator, s.modelCatalog, nil, func(context.Context) {}).Run(t.Context(), domainui.Initialization{
		SelectedUIID:   "ui",
		StartupContent: nil,
		Extensions:     nil,
		Availability:   domainui.AvailabilityCheckingAuthentication,
		Models:         nil,
		ModelSelection: mo.Some(domainui.ModelSelection{}),
		SessionInfo:    session.Info{},
	})

	require.NoError(t, err)
	assert.Equal(t, 3, idleCount)
}
