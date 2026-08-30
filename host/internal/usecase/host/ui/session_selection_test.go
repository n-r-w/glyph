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
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

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
				TargetEntryID:   mo.None[string](),
				SummaryMode:     domainui.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
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
				TargetEntryID:   mo.None[string](),
				SummaryMode:     domainui.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
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
			name:         "submit text",
			command:      testUICommand(domainui.CommandSubmit, mo.None[string]()),
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
				TargetEntryID:   mo.None[string](),
				SummaryMode:     domainui.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
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
				TargetEntryID:   mo.None[string](),
				SummaryMode:     domainui.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
			},
			expectedKind: domainui.FrameError,
			expectedText: "Could not change model selection.",
		},
		{
			name:         "reasoning choice",
			command:      testUICommand(domainui.CommandSelectReasoningChoice, mo.None[string]()),
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
				TargetEntryID:   mo.None[string](),
				SummaryMode:     domainui.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
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
				TargetEntryID:   mo.None[string](),
				SummaryMode:     domainui.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
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
			TargetEntryID:   mo.None[string](),
			SummaryMode:     domainui.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
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
			TargetEntryID:   mo.None[string](),
			SummaryMode:     domainui.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
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
			return testUICommand(domainui.CommandSubmit, mo.Some("request")), nil
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
				TargetEntryID:   mo.None[string](),
				SummaryMode:     domainui.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
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
	s.modelCatalog.EXPECT().
		SelectReasoningChoice(model.ReasoningChoiceMax).
		Return(model.Selection{}, errors.New("secret detail"))
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
		TargetEntryID:   mo.None[string](),
		SummaryMode:     domainui.SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	}, make(chan operationResult))

	require.NoError(t, err)
}
