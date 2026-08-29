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

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

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
