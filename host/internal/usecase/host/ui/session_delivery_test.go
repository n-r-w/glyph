package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

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
