package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestSetSessionNamePreservesTranscriptFrameKind verifies naming returns information with current statistics.
func TestSetSessionNamePreservesTranscriptFrameKind(t *testing.T) {
	t.Parallel()

	// Arrange a successful name change and current empty-session statistics.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	control := NewMockSessionControl(controller)
	info := session.Info{}
	control.EXPECT().SetName(gomock.Any(), "renamed").Return(info, nil)
	control.EXPECT().Information().Return(session.InformationSnapshot{
		Info: info,
		Statistics: session.Statistics{
			UserMessages: 0, ModelResponses: 0, ToolCalls: 0, ToolResults: 0, TotalMessages: 0,
			TokenUsage: mo.Some(session.TokenUsage{}), EstimatedCost: mo.None[session.EstimatedCost](), CostBreakdown: nil,
		},
	})
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		assert.Equal(t, domainui.FrameSessionInformation, frame.Kind)
		assert.Equal(t, info, frame.SessionInfo.MustGet())
		return nil
	})
	usecase := NewSession(channel, nil, nil, nil, control, nil)

	// Act by applying the name command.
	handled, err := usecase.applySessionCommand(t.Context(), domainui.Command{
		Kind:            domainui.CommandSetSessionName,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.Some("renamed"),
	})
	// Assert the command is handled and the information frame is sent.
	require.NoError(t, err)
	assert.True(t, handled)
}

// sessionContextKey isolates context-propagation evidence within this test package.
type sessionContextKey struct{}

// testUICommand creates a complete command for focused session lifecycle tests.
func testUICommand(kind domainui.CommandKind, text mo.Option[string]) domainui.Command {
	return domainui.Command{
		Kind: kind, Text: text, ProviderID: mo.None[string](), ModelID: mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](), SessionID: mo.None[string](),
		SessionName: mo.None[string](),
	}
}

// SessionSuite shares channel, runner, and authenticator mocks across lifecycle scenarios.
type SessionSuite struct {
	suite.Suite
	channel       *MockChannel
	runner        *MockAgentRunner
	authenticator *MockAuthenticator
	modelCatalog  *MockModelCatalog
}

// SetupTest creates isolated session boundaries for each lifecycle scenario.
func (s *SessionSuite) SetupTest() {
	controller := gomock.NewController(s.T())
	s.channel = NewMockChannel(controller)
	s.channel.EXPECT().Close().AnyTimes()
	s.runner = NewMockAgentRunner(controller)
	s.authenticator = NewMockAuthenticator(controller)
	s.modelCatalog = NewMockModelCatalog(controller)
}

// TestSessionInitializationDeliveryFailureSkipsActivation starts no Host work before initialization succeeds.
func (s *SessionSuite) TestSessionInitializationDeliveryFailureSkipsActivation() {
	t := s.T()

	s.channel.EXPECT().Send(gomock.Any()).Return(io.ErrClosedPipe)
	activated := false

	err := NewSession(s.channel, s.runner, s.authenticator, s.modelCatalog, nil, func(context.Context) {
		activated = true
	}).Run(t.Context(), domainui.Initialization{
		SelectedUIID:   "test-ui",
		StartupContent: nil,
		Extensions:     nil,
		Availability:   domainui.AvailabilityCheckingAuthentication,
		Models:         nil,
		ModelSelection: mo.Some(domainui.ModelSelection{}),
		SessionInfo:    session.Info{},
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
		if frame.Kind == domainui.FrameLifecycle && frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityAuthenticationFailed {
			authFailedOnce.Do(func() { close(authFailed) })
		}
		if frame.Kind == domainui.FrameLifecycle && frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
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
			return domainui.Command{
				Kind:            domainui.CommandSubmit,
				Text:            mo.Some("blocked"),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		case 2:
			return domainui.Command{
				Kind:            domainui.CommandRetryAuthentication,
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		default:
			<-ready
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

// TestSessionSelectionCommandsRejectActiveAuthenticationOperations verifies authentication is not interrupted.
func (s *SessionSuite) TestSessionSelectionCommandsRejectActiveAuthenticationOperations() {
	t := s.T()
	tests := []struct {
		name         string
		availability domainui.Availability
		activeKind   operationKind
		command      domainui.Command
	}{
		{
			name:         "authentication check",
			availability: domainui.AvailabilityCheckingAuthentication,
			activeKind:   operationAuthenticationCheck,
			command: domainui.Command{
				Kind:            domainui.CommandSelectModel,
				ProviderID:      mo.Some("openrouter"),
				ModelID:         mo.Some("sonnet"),
				Text:            mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			},
		},
		{
			name:         "interactive sign-in",
			availability: domainui.AvailabilityAuthenticating,
			activeKind:   operationSignIn,
			command: domainui.Command{
				Kind:            domainui.CommandSelectReasoningChoice,
				ReasoningChoice: mo.Some(domainui.ReasoningChoiceHigh),
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := NewMockChannel(gomock.NewController(t))
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				assert.Equal(t, domainui.FrameError, frame.Kind)
				assert.Equal(t, "Could not change model selection.", frame.Text.MustGet())
				return nil
			})
			canceled := false
			cancel := func() { canceled = true }
			session := &Session{
				channel:             channel,
				runner:              s.runner,
				authenticator:       s.authenticator,
				modelCatalog:        s.modelCatalog,
				afterInitialization: func(context.Context) {},
				sessionControl:      nil,
			}

			availability, activeCancel, activeKind, err := session.applyCommand(
				t.Context(), test.availability, cancel, test.activeKind, test.command, make(chan operationResult),
			)

			require.NoError(t, err)
			assert.Equal(t, test.availability, availability)
			assert.Equal(t, test.activeKind, activeKind)
			assert.NotNil(t, activeCancel)
			assert.False(t, canceled)
		})
	}
}

// TestSessionRejectsMissingSelectedCommandPayload verifies malformed commands use safe errors.
func (s *SessionSuite) TestSessionRejectsMissingSelectedCommandPayload() {
	t := s.T()
	tests := []struct {
		name         string
		command      domainui.Command
		expectedKind domainui.FrameKind
		expectedText string
	}{
		{
			name: "submit text",
			command: domainui.Command{
				Kind:            domainui.CommandSubmit,
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			},
			expectedKind: domainui.FrameInformation,
			expectedText: "A nonempty request is required.",
		},
		{
			name: "model provider",
			command: domainui.Command{
				Kind:            domainui.CommandSelectModel,
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.Some("sonnet"),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			},
			expectedKind: domainui.FrameError,
			expectedText: "Could not change model selection.",
		},
		{
			name: "model identifier",
			command: domainui.Command{
				Kind:            domainui.CommandSelectModel,
				Text:            mo.None[string](),
				ProviderID:      mo.Some("openrouter"),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			},
			expectedKind: domainui.FrameError,
			expectedText: "Could not change model selection.",
		},
		{
			name: "reasoning choice",
			command: domainui.Command{
				Kind:            domainui.CommandSelectReasoningChoice,
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			},
			expectedKind: domainui.FrameError,
			expectedText: "Could not change model selection.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			channel := NewMockChannel(gomock.NewController(t))
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				assert.Equal(t, test.expectedKind, frame.Kind)
				assert.Equal(t, test.expectedText, frame.Text.MustGet())
				return nil
			})
			session := &Session{
				channel:             channel,
				runner:              s.runner,
				authenticator:       s.authenticator,
				modelCatalog:        s.modelCatalog,
				afterInitialization: func(context.Context) {},
				sessionControl:      nil,
			}

			availability, activeCancel, activeKind, err := session.applyCommand(
				t.Context(), domainui.AvailabilityIdle, nil, 0, test.command, make(chan operationResult),
			)

			require.NoError(t, err)
			assert.Equal(t, domainui.AvailabilityIdle, availability)
			assert.Nil(t, activeCancel)
			assert.Zero(t, activeKind)
		})
	}
}

// TestSessionSelectionCommandsAllowNonAuthenticationStates verifies availability alone does not block selection.
func (s *SessionSuite) TestSessionSelectionCommandsAllowNonAuthenticationStates() {
	t := s.T()
	tests := []struct {
		name         string
		availability domainui.Availability
		command      domainui.Command
	}{
		{
			name:         "idle model",
			availability: domainui.AvailabilityIdle,
			command: domainui.Command{
				Kind:            domainui.CommandSelectModel,
				ProviderID:      mo.Some("openrouter"),
				ModelID:         mo.Some("sonnet"),
				Text:            mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			},
		},
		{
			name:         "authentication failed reasoning",
			availability: domainui.AvailabilityAuthenticationFailed,
			command: domainui.Command{
				Kind:            domainui.CommandSelectReasoningChoice,
				ReasoningChoice: mo.Some(domainui.ReasoningChoiceHigh),
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			catalog := NewMockModelCatalog(controller)
			selection := model.Selection{
				Provider:        "openrouter",
				Model:           "sonnet",
				ReasoningChoice: model.ReasoningChoiceHigh,
			}
			if test.command.Kind == domainui.CommandSelectModel {
				catalog.EXPECT().SelectModel(
					gomock.Any(), model.ProviderID("openrouter"), model.ID("sonnet"),
				).Return(selection, nil)
			} else {
				catalog.EXPECT().SelectReasoningChoice(model.ReasoningChoiceHigh).Return(selection, nil)
			}
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				assert.Equal(t, domainui.FrameModelSelectionChanged, frame.Kind)
				return nil
			})
			session := &Session{
				channel:             channel,
				runner:              s.runner,
				authenticator:       s.authenticator,
				modelCatalog:        catalog,
				afterInitialization: func(context.Context) {},
				sessionControl:      nil,
			}

			availability, activeCancel, activeKind, err := session.applyCommand(
				t.Context(), test.availability, nil, 0, test.command, make(chan operationResult),
			)

			require.NoError(t, err)
			assert.Equal(t, test.availability, availability)
			assert.Nil(t, activeCancel)
			assert.Zero(t, activeKind)
		})
	}
}

// TestSessionSelectionCommandsCommitDuringActiveRun verifies selection does not cancel the run.
func (s *SessionSuite) TestSessionSelectionCommandsCommitDuringActiveRun() {
	t := s.T()
	ctx := context.WithValue(t.Context(), sessionContextKey{}, "selection")
	canceled := false
	cancel := func() { canceled = true }
	selection := model.Selection{
		Provider:        "openrouter",
		Model:           "sonnet",
		ReasoningChoice: model.ReasoningChoiceHigh,
	}

	s.modelCatalog.EXPECT().SelectModel(ctx, model.ProviderID("openrouter"), model.ID("sonnet")).Return(selection, nil)
	s.modelCatalog.EXPECT().SelectReasoningChoice(model.ReasoningChoiceHigh).Return(selection, nil)
	s.channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		assert.Equal(t, domainui.FrameModelSelectionChanged, frame.Kind)
		assert.Equal(t, domainui.ModelSelection{
			ProviderID:      "openrouter",
			ModelID:         "sonnet",
			ReasoningChoice: domainui.ReasoningChoiceHigh,
		}, frame.ModelSelection.MustGet())
		return nil
	}).Times(2)
	session := &Session{
		channel:             s.channel,
		runner:              s.runner,
		authenticator:       s.authenticator,
		modelCatalog:        s.modelCatalog,
		afterInitialization: func(context.Context) {},
		sessionControl:      nil,
	}

	availability, activeCancel, activeKind, err := session.applyCommand(
		ctx, domainui.AvailabilityRunning, cancel, operationRun, domainui.Command{
			Kind:            domainui.CommandSelectModel,
			ProviderID:      mo.Some("openrouter"),
			ModelID:         mo.Some("sonnet"),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, make(chan operationResult),
	)
	require.NoError(t, err)
	availability, activeCancel, activeKind, err = session.applyCommand(
		ctx, availability, activeCancel, activeKind, domainui.Command{
			Kind:            domainui.CommandSelectReasoningChoice,
			ReasoningChoice: mo.Some(domainui.ReasoningChoiceHigh),
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, make(chan operationResult),
	)

	require.NoError(t, err)
	assert.Equal(t, domainui.AvailabilityRunning, availability)
	assert.Equal(t, operationRun, activeKind)
	assert.NotNil(t, activeCancel)
	assert.False(t, canceled)
}

// TestSessionSelectionSendFailureCancelsAndAwaitsActiveRun verifies command delivery failure owns operation teardown.
func (s *SessionSuite) TestSessionSelectionSendFailureCancelsAndAwaitsActiveRun() {
	t := s.T()
	selectionSendErr := errors.New("send selection confirmation")
	ready := make(chan struct{})
	runStarted := make(chan struct{})
	runCanceled := make(chan struct{})
	runFinished := make(chan struct{})
	selection := model.Selection{
		Provider:        "openrouter",
		Model:           "sonnet",
		ReasoningChoice: model.ReasoningChoiceHigh,
	}

	gomock.InOrder(
		s.channel.EXPECT().Send(gomock.Any()).Return(nil),
		s.channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
			lifecycle, lifecyclePresent := frame.Lifecycle.Get()
			require.True(t, lifecyclePresent)
			availability, availabilityPresent := lifecycle.Availability.Get()
			require.True(t, availabilityPresent)
			assert.Equal(t, domainui.AvailabilityIdle, availability)
			close(ready)
			return nil
		}),
		s.channel.EXPECT().Send(gomock.Any()).Return(nil),
		s.channel.EXPECT().Send(gomock.Any()).Return(selectionSendErr),
	)
	s.authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)
	s.runner.EXPECT().Run(gomock.Any(), "request").DoAndReturn(
		func(ctx context.Context, _ string) (agent.RunOutcome, error) {
			defer close(runFinished)
			close(runStarted)
			<-ctx.Done()
			close(runCanceled)
			return agent.RunOutcomeAborted, ctx.Err()
		},
	)
	s.modelCatalog.EXPECT().SelectModel(
		gomock.Any(), model.ProviderID("openrouter"), model.ID("sonnet"),
	).Return(selection, nil)
	gomock.InOrder(
		s.channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
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
		}),
		s.channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
			<-runStarted
			return domainui.Command{
				Kind:            domainui.CommandSelectModel,
				Text:            mo.None[string](),
				ProviderID:      mo.Some("openrouter"),
				ModelID:         mo.Some("sonnet"),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		}),
		s.channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
			<-runCanceled
			return domainui.Command{}, context.Canceled
		}),
	)

	err := NewSession(s.channel, s.runner, s.authenticator, s.modelCatalog, nil, func(context.Context) {}).Run(
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

	require.ErrorIs(t, err, selectionSendErr)
	select {
	case <-runCanceled:
	default:
		assert.Fail(t, "active run context was not canceled before selection send failure returned")
	}
	select {
	case <-runFinished:
	default:
		assert.Fail(t, "active run was not awaited before selection send failure returned")
	}
}

// TestSessionSelectionFailureSendsCauseWithoutConfirmation verifies rejected selection preserves status and details.
func (s *SessionSuite) TestSessionSelectionFailureSendsCauseWithoutConfirmation() {
	t := s.T()
	s.modelCatalog.EXPECT().SelectReasoningChoice(model.ReasoningChoiceMax).Return(model.Selection{}, errors.New("secret detail"))
	s.channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		assert.Equal(t, domainui.FrameError, frame.Kind)
		assert.Equal(t, "Could not change model selection: secret detail", frame.Text.MustGet())
		return nil
	})

	_, _, _, err := (&Session{
		channel:             s.channel,
		runner:              s.runner,
		authenticator:       s.authenticator,
		modelCatalog:        s.modelCatalog,
		afterInitialization: func(context.Context) {},
		sessionControl:      nil,
	}).applyCommand(t.Context(), domainui.AvailabilityRunning, func() {}, operationRun, domainui.Command{
		Kind:            domainui.CommandSelectReasoningChoice,
		ReasoningChoice: mo.Some(domainui.ReasoningChoiceMax),
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	}, make(chan operationResult))

	require.NoError(t, err)
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
		if frame.Kind == domainui.FrameLifecycle && frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
			idleCount++
		}
		currentIdleCount := idleCount
		mutex.Unlock()
		if currentIdleCount == 1 {
			initialIdleOnce.Do(func() { close(initialIdle) })
		}
		if currentIdleCount >= 2 || (frame.Kind == domainui.FrameInformation && strings.Contains(frame.Text.MustGet(), "retry is not available")) {
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
			return domainui.Command{
				Kind:            domainui.CommandSubmit,
				Text:            mo.Some("request"),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		case 2:
			<-runErrorSent
			return domainui.Command{
				Kind:            domainui.CommandRetryAuthentication,
				Text:            mo.None[string](),
				ProviderID:      mo.None[string](),
				ModelID:         mo.None[string](),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](),
				SessionID:       mo.None[string](),
				SessionName:     mo.None[string](),
			}, nil
		default:
			<-terminalReady
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
	channel.EXPECT().Receive().Return(domainui.Command{
		Kind:            domainui.CommandQuit,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	}, nil)
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

// TestResultDeliveryClosurePreservesSourceProvenance verifies source EOF cannot be mistaken for UI closure.
func TestResultDeliveryClosurePreservesSourceProvenance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		deliveryErr error
		closure     receivedCommand
		authCheck   bool
	}{
		{
			name: "run source with delivery cancellation and Quit", deliveryErr: context.Canceled,
			closure: receivedCommand{
				command: testUICommand(domainui.CommandQuit, mo.None[string]()), err: nil,
			},
			authCheck: false,
		},
		{
			name: "authentication source with delivery EOF and receive EOF", deliveryErr: io.EOF,
			closure: receivedCommand{command: domainui.Command{}, err: io.EOF}, authCheck: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange an EOF-wrapping source and an independently failed error-frame delivery.
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			authenticator := NewMockAuthenticator(controller)
			sourceErr := fmt.Errorf("unique source failed: %w", io.EOF)
			authenticator.EXPECT().IsSignInRequired(sourceErr).Return(test.authCheck)
			channel.EXPECT().Send(gomock.Any()).Return(test.deliveryErr)
			sessionService := &Session{
				channel: channel, runner: nil, authenticator: authenticator, modelCatalog: nil,
				sessionControl: nil, afterInitialization: nil,
			}
			var deliveryErr error
			if test.authCheck {
				results := make(chan operationResult, 1)
				_, _, _, deliveryErr = sessionService.applyAuthenticationCheck(
					t.Context(), domainui.AvailabilityCheckingAuthentication, sourceErr, results,
				)
			} else {
				_, _, _, deliveryErr = sessionService.applyRunResult(domainui.AvailabilityRunning, sourceErr)
			}
			commands := make(chan receivedCommand, 1)
			commands <- test.closure

			// Act through receiver-confirmed owner closure.
			err := sessionService.resolveDeliveryFailure(t.Context(), commands, deliveryErr)

			// Assert the unchanged source wrapper remains once while the delivery leaf disappears.
			require.ErrorIs(t, err, sourceErr)
			require.ErrorIs(t, err, io.EOF)
			assert.Equal(t, 1, strings.Count(err.Error(), sourceErr.Error()))
			require.NotErrorIs(t, err, context.Canceled)
		})
	}
}

// TestApplyRunResultFiltersOnlyCancellationLeaves verifies mixed cancellation keeps independent failures visible.
func TestApplyRunResultFiltersOnlyCancellationLeaves(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		runErr        error
		expectedError string
	}{
		{
			name:          "pure wrapped cancellation",
			runErr:        fmt.Errorf("stop run: %w", context.Canceled),
			expectedError: "",
		},
		{
			name:          "mixed cancellation",
			runErr:        errors.Join(context.Canceled, errors.New("unique mixed cancellation failure")),
			expectedError: "unique mixed cancellation failure",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange generated Host UI boundary mocks and collect delivered frames.
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			authenticator := NewMockAuthenticator(controller)
			var frames []domainui.Frame
			if test.expectedError != "" {
				authenticator.EXPECT().IsSignInRequired(gomock.Any()).Return(false).AnyTimes()
			}
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				frames = append(frames, frame)
				return nil
			}).AnyTimes()
			sessionService := &Session{
				channel: channel, runner: nil, authenticator: authenticator, modelCatalog: nil,
				sessionControl: nil, afterInitialization: nil,
			}

			// Act by applying the completed run error.
			availability, cancel, kind, err := sessionService.applyRunResult(domainui.AvailabilityRunning, test.runErr)

			// Assert pure cancellation is silent, while mixed cancellation emits one detailed error and returns idle.
			require.NoError(t, err)
			assert.Equal(t, domainui.AvailabilityIdle, availability)
			assert.Nil(t, cancel)
			assert.Zero(t, kind)
			var errorFrames []domainui.Frame
			for _, frame := range frames {
				if frame.Kind == domainui.FrameError {
					errorFrames = append(errorFrames, frame)
				}
			}
			if test.expectedError == "" {
				assert.Empty(t, errorFrames)
			} else {
				require.Len(t, errorFrames, 1)
				assert.Contains(t, errorFrames[0].Text.MustGet(), test.expectedError)
			}
		})
	}
}

// TestApplyRunResultJoinsSourceAndFrameSendFailures verifies local reporting keeps both causes.
func TestApplyRunResultJoinsSourceAndFrameSendFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		signInRequired bool
	}{
		{name: "run error frame", signInRequired: false},
		{name: "authentication error frame", signInRequired: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a source failure and an independent UI frame delivery failure.
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			authenticator := NewMockAuthenticator(controller)
			sourceErr := errors.New("unique source failure")
			sendErr := errors.New("unique error frame send failure")
			authenticator.EXPECT().IsSignInRequired(sourceErr).Return(test.signInRequired)
			channel.EXPECT().Send(gomock.Any()).Return(sendErr)
			sessionService := &Session{
				channel: channel, runner: nil, authenticator: authenticator, modelCatalog: nil,
				sessionControl: nil, afterInitialization: nil,
			}

			// Act by applying a failed run whose error frame cannot be delivered.
			_, _, _, err := sessionService.applyRunResult(domainui.AvailabilityRunning, sourceErr)

			// Assert the local caller receives each independent cause once.
			require.ErrorIs(t, err, sourceErr)
			require.ErrorIs(t, err, sendErr)
			assert.Equal(t, 1, strings.Count(err.Error(), sourceErr.Error()))
			assert.Equal(t, 1, strings.Count(err.Error(), sendErr.Error()))
		})
	}
}

// TestResolveDeliveryFailureFiltersOnlyConfirmedClosureLeaves verifies source errors survive UI closure.
func TestResolveDeliveryFailureFiltersOnlyConfirmedClosureLeaves(t *testing.T) {
	t.Parallel()

	sourceErr := errors.New("unique error-frame source failure")
	tests := []struct {
		name        string
		deliveryErr error
		received    receivedCommand
		expectedErr error
	}{
		{
			name:        "source and EOF with Quit",
			deliveryErr: errors.Join(sourceErr, io.EOF),
			received: receivedCommand{
				command: testUICommand(domainui.CommandQuit, mo.None[string]()), err: nil,
			},
			expectedErr: sourceErr,
		},
		{
			name:        "source and EOF with receive EOF",
			deliveryErr: errors.Join(sourceErr, io.EOF),
			received:    receivedCommand{command: domainui.Command{}, err: io.EOF},
			expectedErr: sourceErr,
		},
		{
			name:        "pure cancellation with canceled receive",
			deliveryErr: context.Canceled,
			received:    receivedCommand{command: domainui.Command{}, err: context.Canceled},
			expectedErr: nil,
		},
		{
			name:        "source and cancellation with canceled receive",
			deliveryErr: errors.Join(sourceErr, context.Canceled),
			received:    receivedCommand{command: domainui.Command{}, err: context.Canceled},
			expectedErr: sourceErr,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a buffered receiver terminal that confirms owner closure.
			commands := make(chan receivedCommand, 1)
			commands <- test.received
			sessionService := &Session{
				channel: nil, runner: nil, authenticator: nil, modelCatalog: nil,
				sessionControl: nil, afterInitialization: nil,
			}

			// Act by resolving the failed error-frame delivery against receiver ownership.
			err := sessionService.resolveDeliveryFailure(t.Context(), commands, test.deliveryErr)

			// Assert only closure leaves disappear and any source remains once.
			if test.expectedErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.expectedErr)
				assert.Equal(t, 1, strings.Count(err.Error(), test.expectedErr.Error()))
			}
			require.NotErrorIs(t, err, io.EOF)
			require.NotErrorIs(t, err, context.Canceled)
		})
	}
}

// TestSessionCommandDeliveryClosurePreservesOperationSource verifies command sources survive failed responses.
func TestSessionCommandDeliveryClosurePreservesOperationSource(t *testing.T) {
	t.Parallel()

	repositoryErr := fmt.Errorf("unique session repository failure: %w", io.EOF)
	selectionErr := fmt.Errorf("unique credential catalog failure: %w", io.EOF)
	tests := []struct {
		name        string
		command     domainui.Command
		failureKind domainui.FrameKind
		sendErr     error
		closure     receivedCommand
		expectedErr error
		setup       func(*MockSessionControl, *MockModelCatalog)
	}{
		{
			name:        "session source with response EOF",
			command:     testUICommand(domainui.CommandListSessions, mo.None[string]()),
			failureKind: domainui.FrameInformation, sendErr: io.EOF,
			closure:     receivedCommand{command: domainui.Command{}, err: io.EOF},
			expectedErr: repositoryErr,
			setup: func(control *MockSessionControl, _ *MockModelCatalog) {
				control.EXPECT().List(gomock.Any()).Return(nil, repositoryErr)
			},
		},
		{
			name: "selection source with response cancellation",
			command: domainui.Command{
				Kind: domainui.CommandSelectModel, Text: mo.None[string](),
				ProviderID: mo.Some("provider"), ModelID: mo.Some("model"),
				ReasoningChoice: mo.None[domainui.ReasoningChoice](), SessionID: mo.None[string](),
				SessionName: mo.None[string](),
			},
			failureKind: domainui.FrameError, sendErr: context.Canceled,
			closure: receivedCommand{
				command: testUICommand(domainui.CommandQuit, mo.None[string]()), err: nil,
			},
			expectedErr: selectionErr,
			setup: func(_ *MockSessionControl, catalog *MockModelCatalog) {
				catalog.EXPECT().SelectModel(gomock.Any(), model.ProviderID("provider"), model.ID("model")).
					Return(model.Selection{}, selectionErr)
			},
		},
		{
			name:        "pure command response cancellation",
			command:     testUICommand(domainui.CommandKind(255), mo.None[string]()),
			failureKind: domainui.FrameInformation, sendErr: context.Canceled,
			closure:     receivedCommand{command: domainui.Command{}, err: context.Canceled},
			expectedErr: nil,
			setup:       func(_ *MockSessionControl, _ *MockModelCatalog) {},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a command operation and a response failure followed by confirmed owner closure.
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			authenticator := NewMockAuthenticator(controller)
			control := NewMockSessionControl(controller)
			catalog := NewMockModelCatalog(controller)
			test.setup(control, catalog)
			ready := make(chan struct{})
			var readyOnce sync.Once
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				if frame.Kind == domainui.FrameLifecycle &&
					frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
					readyOnce.Do(func() { close(ready) })
					return nil
				}
				if frame.Kind == test.failureKind {
					return test.sendErr
				}
				return nil
			}).AnyTimes()
			channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
				<-ready
				return test.command, nil
			})
			channel.EXPECT().Receive().Return(test.closure.command, test.closure.err)
			channel.EXPECT().Close()
			authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)
			sessionService := NewSession(channel, nil, authenticator, catalog, control, func(context.Context) {})

			// Act through command execution, failed response delivery, and receiver-confirmed closure.
			err := sessionService.Run(t.Context(), domainui.Initialization{
				SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
				Availability: domainui.AvailabilityCheckingAuthentication, Models: nil,
				ModelSelection: mo.Some(domainui.ModelSelection{}), SessionInfo: session.Info{},
			})

			// Assert delivery closure disappears while any EOF-wrapping operation source remains once.
			if test.expectedErr == nil {
				require.NoError(t, err)
				require.NotErrorIs(t, err, io.EOF)
			} else {
				require.ErrorIs(t, err, test.expectedErr)
				require.ErrorIs(t, err, io.EOF)
				assert.Equal(t, 1, strings.Count(err.Error(), test.expectedErr.Error()))
			}
			require.NotErrorIs(t, err, context.Canceled)
		})
	}
}

// TestSessionQuitPreservesReadyProviderEOF verifies graceful closure does not filter provider errors.
func TestSessionQuitPreservesReadyProviderEOF(t *testing.T) {
	t.Parallel()

	providerErr := fmt.Errorf("unique provider stream failure: %w", io.EOF)
	tests := []struct {
		name        string
		receiveErr  error
		activeErr   error
		expectedErr []error
	}{
		{
			name: "Quit with provider EOF", receiveErr: nil, activeErr: providerErr,
			expectedErr: []error{providerErr},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange an active run whose result becomes available only after graceful closure cancels it.
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			runner := NewMockAgentRunner(controller)
			authenticator := NewMockAuthenticator(controller)
			ready := make(chan struct{})
			runStarted := make(chan struct{})
			var readyOnce sync.Once
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				if frame.Kind == domainui.FrameLifecycle &&
					frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
					readyOnce.Do(func() { close(ready) })
				}
				return nil
			}).AnyTimes()
			channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
				<-ready
				return testUICommand(domainui.CommandSubmit, mo.Some("request")), nil
			})
			channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
				<-runStarted
				if test.receiveErr != nil {
					return domainui.Command{}, test.receiveErr
				}
				return testUICommand(domainui.CommandQuit, mo.None[string]()), nil
			})
			channel.EXPECT().Close()
			authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)
			runner.EXPECT().Run(gomock.Any(), "request").DoAndReturn(
				func(ctx context.Context, _ string) (agent.RunOutcome, error) {
					close(runStarted)
					<-ctx.Done()
					return agent.RunOutcomeAborted, test.activeErr
				},
			)
			sessionService := NewSession(channel, runner, authenticator, nil, nil, func(context.Context) {})

			// Act while Quit or receive EOF wins before the canceled active result.
			err := sessionService.Run(t.Context(), domainui.Initialization{
				SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
				Availability: domainui.AvailabilityCheckingAuthentication, Models: nil,
				ModelSelection: mo.Some(domainui.ModelSelection{}), SessionInfo: session.Info{},
			})

			// Assert the provider-originated wrapped EOF remains visible once.
			for _, expectedErr := range test.expectedErr {
				require.ErrorIs(t, err, expectedErr)
				assert.Equal(t, 1, strings.Count(err.Error(), expectedErr.Error()))
			}
		})
	}
}

// TestShutdownClosesBeforeWaitingForActiveResult verifies shutdown can unblock terminal delivery.
func TestShutdownClosesBeforeWaitingForActiveResult(t *testing.T) {
	t.Parallel()

	persistenceErr := errors.New("unique shutdown persistence failure")
	tests := []struct {
		name        string
		activeErr   error
		expectedErr error
	}{
		{name: "close cancellation", activeErr: context.Canceled, expectedErr: nil},
		{
			name:        "close cancellation with persistence failure",
			activeErr:   errors.Join(context.Canceled, persistenceErr),
			expectedErr: persistenceErr,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange an active result that cannot become ready until the owned channel closes.
				controller := gomock.NewController(t)
				channel := NewMockChannel(controller)
				results := make(chan operationResult, 1)
				closed := make(chan struct{})
				receiverCanceled := make(chan struct{})
				receiverDone := make(chan struct{})
				close(receiverDone)
				channel.EXPECT().Close().Do(func() {
					select {
					case <-receiverCanceled:
					default:
						assert.Fail(t, "receiver was not canceled before channel close")
					}
					close(closed)
					select {
					case results <- operationResult{kind: operationRun, err: test.activeErr}:
					default:
					}
				})
				sessionService := &Session{
					channel: channel, runner: nil, authenticator: nil, modelCatalog: nil,
					sessionControl: nil, afterInitialization: nil,
				}
				shutdownDone := make(chan error, 1)

				// Act by starting shutdown while terminal delivery remains blocked on the open channel.
				go func() {
					shutdownDone <- sessionService.shutdown(
						func() {}, operationRun, results, func() { close(receiverCanceled) }, receiverDone,
					)
				}()
				synctest.Wait()

				// Assert close happens before the active-result wait, then release legacy order for clean RED exit.
				closedBeforeWait := false
				select {
				case <-closed:
					closedBeforeWait = true
				default:
				}
				assert.True(t, closedBeforeWait, "channel must close before shutdown waits for the active result")
				if !closedBeforeWait {
					results <- operationResult{kind: operationRun, err: test.activeErr}
				}
				synctest.Wait()
				shutdownErr := <-shutdownDone
				if test.expectedErr == nil {
					require.NoError(t, shutdownErr)
				} else {
					require.ErrorIs(t, shutdownErr, test.expectedErr)
					require.NotErrorIs(t, shutdownErr, context.Canceled)
				}
			})
		})
	}
}

// TestSessionQuitReturnsIndependentActiveRunFailures verifies shutdown keeps non-cancellation causes.
func TestSessionQuitReturnsIndependentActiveRunFailures(t *testing.T) {
	t.Parallel()

	persistenceErr := errors.New("unique quit persistence failure")
	settlementErr := errors.New("unique quit settlement failure")
	tests := []struct {
		name           string
		runErr         error
		expectedErrors []error
	}{
		{
			name:           "pure cancellation",
			runErr:         fmt.Errorf("stop active run: %w", context.Canceled),
			expectedErrors: nil,
		},
		{
			name:           "cancellation and persistence",
			runErr:         errors.Join(context.Canceled, persistenceErr),
			expectedErrors: []error{persistenceErr},
		},
		{
			name:           "cancellation and independent failures",
			runErr:         errors.Join(context.Canceled, persistenceErr, settlementErr),
			expectedErrors: []error{persistenceErr, settlementErr},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange an authenticated session that receives quit while one run is active.
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			runner := NewMockAgentRunner(controller)
			authenticator := NewMockAuthenticator(controller)
			ready := make(chan struct{})
			runStarted := make(chan struct{})
			var readyOnce sync.Once
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				if frame.Kind == domainui.FrameLifecycle &&
					frame.Lifecycle.MustGet().Availability.MustGet() == domainui.AvailabilityIdle {
					readyOnce.Do(func() { close(ready) })
				}
				return nil
			}).AnyTimes()
			channel.EXPECT().Close().AnyTimes()
			authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)
			runner.EXPECT().Run(gomock.Any(), "active request").DoAndReturn(
				func(ctx context.Context, _ string) (agent.RunOutcome, error) {
					close(runStarted)
					<-ctx.Done()
					return agent.RunOutcomeAborted, test.runErr
				},
			)
			commandCall := 0
			channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
				commandCall++
				if commandCall == 1 {
					<-ready
					return domainui.Command{
						Kind: domainui.CommandSubmit, Text: mo.Some("active request"),
						ProviderID: mo.None[string](), ModelID: mo.None[string](),
						ReasoningChoice: mo.None[domainui.ReasoningChoice](),
						SessionID:       mo.None[string](), SessionName: mo.None[string](),
					}, nil
				}
				<-runStarted
				return domainui.Command{
					Kind: domainui.CommandQuit, Text: mo.None[string](),
					ProviderID: mo.None[string](), ModelID: mo.None[string](),
					ReasoningChoice: mo.None[domainui.ReasoningChoice](),
					SessionID:       mo.None[string](), SessionName: mo.None[string](),
				}, nil
			}).Times(2)
			sessionService := NewSession(channel, runner, authenticator, nil, nil, func(context.Context) {})

			// Act by running the session through active-operation quit.
			err := sessionService.Run(t.Context(), domainui.Initialization{
				SelectedUIID: "ui", StartupContent: nil, Extensions: nil,
				Availability: domainui.AvailabilityCheckingAuthentication, Models: nil,
				ModelSelection: mo.Some(domainui.ModelSelection{}), SessionInfo: session.Info{},
			})

			// Assert pure cancellation is clean and every independent active-run failure is returned.
			if len(test.expectedErrors) == 0 {
				require.NoError(t, err)
			} else {
				for _, expectedErr := range test.expectedErrors {
					require.ErrorIs(t, err, expectedErr)
				}
			}
		})
	}
}

// TestSessionSuite runs isolated UI session lifecycle scenarios.
func TestSessionSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(SessionSuite))
}

// TestSessionSendFailureClosesPendingReceive verifies teardown unblocks and joins a pending receive call.
func TestSessionSendFailureClosesPendingReceive(t *testing.T) {
	t.Parallel()
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	authenticator := NewMockAuthenticator(controller)
	receiveStarted := make(chan struct{})
	receiveRelease := make(chan struct{})
	receiveCompleted := make(chan struct{})
	var releaseOnce sync.Once
	releaseReceive := func() {
		releaseOnce.Do(func() { close(receiveRelease) })
	}
	defer releaseReceive()

	gomock.InOrder(
		channel.EXPECT().Send(gomock.Any()).Return(nil),
		channel.EXPECT().Send(gomock.Any()).Return(io.ErrUnexpectedEOF),
	)
	channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
		close(receiveStarted)
		<-receiveRelease
		close(receiveCompleted)
		return domainui.Command{}, context.Canceled
	})
	channel.EXPECT().Close().Do(releaseReceive).AnyTimes()
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).DoAndReturn(func(context.Context) error {
		<-receiveStarted
		return nil
	})

	err := NewSession(
		channel,
		NewMockAgentRunner(controller),
		authenticator,
		NewMockModelCatalog(controller),
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

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	select {
	case <-receiveCompleted:
	default:
		assert.Fail(t, "pending command receive was not completed before session return")
	}
}

// TestReceiveCommandsCancellationUnblocksCompletedHandoff verifies cancellation releases a completed receive result.
func TestReceiveCommandsCancellationUnblocksCompletedHandoff(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		controller := gomock.NewController(t)
		channel := NewMockChannel(controller)
		ctx, cancel := context.WithCancel(t.Context())
		commands := make(chan receivedCommand)
		receiveCompleted := make(chan struct{})
		receiverDone := make(chan struct{})
		channel.EXPECT().Receive().DoAndReturn(func() (domainui.Command, error) {
			close(receiveCompleted)
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

		go func() {
			defer close(receiverDone)
			(&Session{
				channel:             channel,
				runner:              nil,
				authenticator:       nil,
				modelCatalog:        nil,
				afterInitialization: nil,
				sessionControl:      nil,
			}).receiveCommands(ctx, commands)
		}()
		<-receiveCompleted
		synctest.Wait()
		cancel()
		synctest.Wait()

		select {
		case <-receiverDone:
		default:
			assert.Fail(t, "completed command receive remained blocked on result handoff")
			<-commands
			synctest.Wait()
		}
	})
}

// containsRetryableError reports whether one retryable error frame matches text.
func containsRetryableError(frames []domainui.Frame, text string) bool {
	for _, frame := range frames {
		if frame.Kind == domainui.FrameError && frame.RetryAuthentication.MustGet() && frame.Text.MustGet() == text {
			return true
		}
	}
	return false
}

// containsInformation reports whether one information frame contains text.
func containsInformation(frames []domainui.Frame, text string) bool {
	for _, frame := range frames {
		if frame.Kind == domainui.FrameInformation && strings.Contains(frame.Text.MustGet(), text) {
			return true
		}
	}
	return false
}

// containsAvailability reports whether one lifecycle frame carries availability.
func containsAvailability(frames []domainui.Frame, availability domainui.Availability) bool {
	for _, frame := range frames {
		if frame.Kind == domainui.FrameLifecycle && frame.Lifecycle.MustGet().Availability.MustGet() == availability {
			return true
		}
	}
	return false
}
