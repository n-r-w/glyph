//go:build !integration

package ui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestRunOperationsInitializationDeliveryFailurePreventsActivation verifies startup delivery ownership.
func TestRunOperationsInitializationDeliveryFailurePreventsActivation(t *testing.T) {
	t.Parallel()

	// Arrange one unique wrapped initialization delivery failure.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	source := errors.New("UI process exited during initialization")
	wrapped := fmt.Errorf("send initialization frame: %w", source)
	channel.EXPECT().Initialize(gomock.Any(), gomock.Any()).Return(wrapped)
	activated := false
	service := NewSession(
		channel, NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), nil, func(context.Context) { activated = true },
	)

	// Act through Host startup.
	err := service.RunOperations(t.Context(), domainui.Initialization{})

	// Assert cause preservation and no Host activation or operation receipt.
	require.ErrorIs(t, err, source)
	require.ErrorContains(t, err, wrapped.Error())
	assert.False(t, activated)
}

// TestRunOperationsCancelsAndJoinsAuthenticationCheck verifies startup worker ownership.
func TestRunOperationsCancelsAndJoinsAuthenticationCheck(t *testing.T) {
	t.Parallel()

	// Arrange a startup authentication check blocked until its owned context is canceled.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	authenticator := NewMockAuthenticator(controller)
	started := make(chan struct{})
	stopped := make(chan struct{})
	channel.EXPECT().Initialize(gomock.Any(), gomock.Any()).Return(nil)
	channel.EXPECT().RunOperations(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(
		_ context.Context,
		activate func(),
		_ func(context.Context, domainui.Command) (operation.Prepared[domainui.Frame, domainui.Frame], error),
	) error {
		activate()
		<-started
		return nil
	})
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return context.Cause(ctx)
	})
	authenticator.EXPECT().IsSignInRequired(gomock.Any()).AnyTimes().Return(false)
	channel.EXPECT().Send(gomock.Any()).AnyTimes().Return(nil)
	service := NewSession(
		channel, NewMockAgentRunner(controller), authenticator, NewMockModelCatalog(controller), nil,
		func(context.Context) {},
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Act through normal UI operation-loop return.
	err := service.RunOperations(ctx, domainui.Initialization{})

	// Assert authentication stopped before RunOperations returned.
	require.NoError(t, err)
	select {
	case <-stopped:
	default:
		cancel()
		t.Fatal("RunOperations returned before authentication check stopped")
	}
}

// TestPrepareRejectsOrdinaryOperationBeforeAuthenticationReadiness verifies bounded NOT_READY admission.
func TestPrepareRejectsOrdinaryOperationBeforeAuthenticationReadiness(t *testing.T) {
	t.Parallel()

	// Arrange a new session whose startup authentication check has not completed.
	controller := gomock.NewController(t)
	service := NewSession(
		NewMockChannel(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), NewMockSessionControl(controller), func(context.Context) {},
	)
	command := newCommandForPreparedTest(domainui.CommandGetSessionInfo)

	// Act through bounded operation preparation.
	prepared, err := service.Prepare(t.Context(), command)

	// Assert no operation is created and the complete classified rejection is preserved.
	assert.Nil(t, prepared)
	var rejection *PreparationError
	require.ErrorAs(t, err, &rejection)
	assert.Equal(t, rejectionCodeNotReady, rejection.Code())
	assert.ErrorIs(t, err, errors.Unwrap(err))
}

// TestPrepareReservesSessionMutationBeforeRun verifies admission, execution, and release ownership.
func TestPrepareReservesSessionMutationBeforeRun(t *testing.T) {
	t.Parallel()

	// Arrange one ready session and an observable mutation reservation.
	controller := gomock.NewController(t)
	control := NewMockSessionControl(controller)
	released := false
	control.EXPECT().TryAcquire().Return(func() { released = true }, true)
	control.EXPECT().Create(gomock.Any()).Return(session.Replacement{Info: session.Info{
		ID: "session", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.None[string](), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}, Entries: nil}, nil)
	service := NewSession(
		NewMockChannel(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityIdle)
	command := newCommandForPreparedTest(domainui.CommandCreateSession)

	// Act by preparing before execution and then running admitted work.
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	assert.False(t, released)
	outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
	prepared.Release()

	// Assert success is terminal only after durable work and reservation release.
	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
	frame, present := outcome.Result()
	require.True(t, present)
	assert.Equal(t, domainui.FrameSessionChanged, frame.Kind)
	assert.True(t, released)
}

// TestPreparedCancellationRemovesOnlyCancellationLeaves verifies mixed failure preservation.
func TestPreparedCancellationRemovesOnlyCancellationLeaves(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		mixed         bool
		expectedState operation.TerminalState
	}{
		{name: "pure cancellation", mixed: false, expectedState: operation.TerminalStateCanceled},
		{name: "joined independent failure", mixed: true, expectedState: operation.TerminalStateFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one prepared operation with a controlled source result.
			independent := errors.New("settlement failed")
			runErr := error(context.Canceled)
			if test.mixed {
				runErr = errors.Join(context.Canceled, independent)
			}
			prepared := &preparedUIOperation{
				run: func(context.Context, operation.Reporter[domainui.Frame]) (domainui.Frame, error) {
					return domainui.Frame{}, runErr
				},
				failureCode: func(error) string { return failureCodeInternal },
				release:     func() {}, releaseOnce: sync.Once{},
			}

			// Act through operation outcome classification.
			outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})

			// Assert pure cancellation stays canceled and independent failure stays reachable.
			assert.Equal(t, test.expectedState, outcome.State())
			if test.mixed {
				assert.ErrorIs(t, outcome.Err(), independent)
				assert.NotErrorIs(t, outcome.Err(), context.Canceled)
			}
		})
	}
}

// TestPreparedFailurePreservesCategoryTextAndCause verifies accepted-operation error semantics.
func TestPreparedFailurePreservesCategoryTextAndCause(t *testing.T) {
	t.Parallel()

	// Arrange one admitted mutation whose durable operation fails with a classified cause.
	controller := gomock.NewController(t)
	control := NewMockSessionControl(controller)
	source := fmt.Errorf("create session file: %w", session.ErrPersistenceUnavailable)
	control.EXPECT().TryAcquire().Return(func() {}, true)
	control.EXPECT().Create(gomock.Any()).Return(session.Replacement{}, source)
	service := NewSession(
		NewMockChannel(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityIdle)
	prepared, err := service.Prepare(t.Context(), newCommandForPreparedTest(domainui.CommandCreateSession))
	require.NoError(t, err)

	// Act through accepted operation execution.
	outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
	prepared.Release()

	// Assert category, complete text, and original cause remain available.
	assert.Equal(t, operation.TerminalStateFailed, outcome.State())
	assert.Equal(t, failureCodePersistence, outcome.Code())
	require.EqualError(t, outcome.Err(), source.Error())
	assert.ErrorIs(t, outcome.Err(), session.ErrPersistenceUnavailable)
}

// TestSelectionFailureCodesMatchHostCategories verifies exact public category parity.
func TestSelectionFailureCodesMatchHostCategories(t *testing.T) {
	t.Parallel()
	// Arrange each model-selection source category and its required Host failure category.
	for source, expected := range map[string]string{
		selectionCodeNotFound:     failureCodeNotFound,
		selectionCodeReasoning:    failureCodeReasoning,
		selectionCodeProviderAuth: failureCodeProviderAuth,
		"unknown":                 failureCodeInternal,
	} {
		// Act by classifying the case-specific selection error.
		actual := selectionFailureCode(selectionCodeTestError(source))

		// Assert the classifier returns the exact public Host category.
		assert.Equal(t, expected, actual)
	}
}

// selectionCodeTestError exposes one stable model-selection source category.
type selectionCodeTestError string

// Error returns the source category as complete test text.
func (e selectionCodeTestError) Error() string { return string(e) }

// SelectionCode returns the stable source category.
func (e selectionCodeTestError) SelectionCode() string { return string(e) }

// expectSessionMutationGate configures successful gate ownership for prepared mutation tests.
func expectSessionMutationGate(control *MockSessionControl, times int) {
	control.EXPECT().TryAcquire().Times(times).DoAndReturn(func() (func(), bool) { return func() {}, true })
}

// runPreparedCommand executes one admitted command and returns its completed frame.
func runPreparedCommand(t *testing.T, service *Session, command domainui.Command) (domainui.Frame, error) {
	t.Helper()
	prepared, err := service.Prepare(t.Context(), command)
	if err != nil {
		return domainui.Frame{}, err
	}
	defer prepared.Release()
	outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
	if outcome.Err() != nil {
		return domainui.Frame{}, outcome.Err()
	}
	frame, ok := outcome.Result()
	if !ok {
		return domainui.Frame{}, errors.New("prepared command did not complete")
	}
	return frame, nil
}

// newCommandForPreparedTest creates one complete operation request with absent optional fields.
func newCommandForPreparedTest(kind domainui.CommandKind) domainui.Command {
	return domainui.Command{
		OperationID: "operation", Kind: kind, Text: mo.None[string](), ProviderID: mo.None[string](),
		ModelID: mo.None[string](), ReasoningChoice: mo.None[domainui.ReasoningChoice](),
		SessionID: mo.None[string](), SessionName: mo.None[string](), TargetEntryID: mo.None[string](),
		SummaryMode: domainui.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
	}
}
