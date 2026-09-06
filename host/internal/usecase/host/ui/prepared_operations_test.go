//go:build !integration

package ui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestInitializeFailurePreservesCause verifies startup delivery ownership.
func TestInitializeFailurePreservesCause(t *testing.T) {
	t.Parallel()

	// Arrange one unique wrapped initialization delivery failure.
	controller := gomock.NewController(t)
	channel := NewMockOutput(controller)
	source := errors.New("UI process exited during initialization")
	wrapped := fmt.Errorf("send initialization frame: %w", source)
	channel.EXPECT().Initialize(gomock.Any(), gomock.Any()).Return(wrapped)
	activated := false
	service := NewSession(
		channel, NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), nil, nil, nil, func(context.Context) { activated = true },

		Initialization{},
	)

	// Act through Host startup.
	err := service.Initialize(t.Context())

	// Assert cause preservation and no Host activation or operation receipt.
	require.ErrorIs(t, err, source)
	require.ErrorContains(t, err, wrapped.Error())
	assert.False(t, activated)
}

// TestActivationCleanupCancelsAndJoinsAuthenticationCheck verifies startup worker ownership.
func TestActivationCleanupCancelsAndJoinsAuthenticationCheck(t *testing.T) {
	t.Parallel()

	// Arrange a startup authentication check blocked until its owned context is canceled.
	controller := gomock.NewController(t)
	channel := NewMockOutput(controller)
	authenticator := NewMockAuthenticator(controller)
	started := make(chan struct{})
	stopped := make(chan struct{})
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return context.Cause(ctx)
	})
	authenticator.EXPECT().IsSignInRequired(gomock.Any()).AnyTimes().Return(false)
	service := NewSession(
		channel, NewMockAgentRunner(controller), authenticator, NewMockModelCatalog(controller), nil, nil,
		nil, func(context.Context) {},

		Initialization{},
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Act by starting readiness work and requesting its owned cleanup.
	stop := service.Activate(ctx)
	<-started
	stop()

	// Assert cleanup joined the authentication worker before returning.
	select {
	case <-stopped:
	default:
		t.Fatal("activation cleanup returned before authentication stopped")
	}
}

// TestPrepareRejectsOrdinaryOperationBeforeAuthenticationReadiness verifies bounded NOT_READY admission.
func TestPrepareRejectsOrdinaryOperationBeforeAuthenticationReadiness(t *testing.T) {
	t.Parallel()

	// Arrange a new session whose startup authentication check has not completed.
	controller := gomock.NewController(t)
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), NewMockActiveSessions(controller), nil, nil, func(context.Context) {},

		Initialization{},
	)
	command := newCommandForPreparedTest(controllerui.CommandGetSessionInfo)

	// Act through bounded operation preparation.
	prepared, err := service.Prepare(t.Context(), command)

	// Assert no operation is created and the complete classified rejection is preserved.
	assert.Nil(t, prepared)
	var rejection *PreparationError
	require.ErrorAs(t, err, &rejection)
	assert.Equal(t, controllerui.RejectionCodeNotReady, rejection.PreparationCode())
	assert.ErrorIs(t, err, errors.Unwrap(err))
}

// TestPrepareReservesSessionMutationBeforeRun verifies admission, execution, and release ownership.
func TestPrepareReservesSessionMutationBeforeRun(t *testing.T) {
	t.Parallel()

	// Arrange one ready session and an observable mutation reservation.
	controller := gomock.NewController(t)
	control := NewMockActiveSessions(controller)
	gate := NewMockGate(controller)
	released := false
	gate.EXPECT().TryAcquire().Return(func() { released = true }, true)
	control.EXPECT().CreateActive().Return(session.Info{
		ID: "session", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.None[string](), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0),
	}, nil, nil)
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, nil, gate, func(context.Context) {},

		Initialization{},
	)
	service.setOperationAvailability(AvailabilityIdle)
	command := newCommandForPreparedTest(controllerui.CommandCreateSession)

	// Act by preparing before execution and then running admitted work.
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	assert.False(t, released)
	outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
	prepared.Release()

	// Assert success is terminal only after durable work and reservation release.
	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
	frame, present := outcome.Result()
	require.True(t, present)
	assert.Equal(t, controllerui.FrameSessionChanged, frame.Kind)
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
				run: func(context.Context, operation.Reporter[controllerui.Frame]) (controllerui.Frame, error) {
					return controllerui.Frame{}, runErr
				},
				failureCode: func(error) string { return controllerui.FailureCodeInternal },
				release:     func() {}, releaseOnce: sync.Once{},
			}

			// Act through operation outcome classification.
			outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})

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
	control := NewMockActiveSessions(controller)
	gate := NewMockGate(controller)
	source := fmt.Errorf("create session file: %w", session.ErrPersistenceUnavailable)
	gate.EXPECT().TryAcquire().Return(func() {}, true)
	control.EXPECT().CreateActive().Return(session.Info{}, nil, source)
	service := NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), control, nil, gate, func(context.Context) {},

		Initialization{},
	)
	service.setOperationAvailability(AvailabilityIdle)
	prepared, err := service.Prepare(t.Context(), newCommandForPreparedTest(controllerui.CommandCreateSession))
	require.NoError(t, err)

	// Act through accepted operation execution.
	outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
	prepared.Release()

	// Assert category, complete text, and original cause remain available.
	assert.Equal(t, operation.TerminalStateFailed, outcome.State())
	assert.Equal(t, controllerui.FailureCodePersistence, outcome.Code())
	require.EqualError(t, outcome.Err(), source.Error())
	assert.ErrorIs(t, outcome.Err(), session.ErrPersistenceUnavailable)
}

// TestSelectionFailureCodesMatchHostCategories verifies exact public category parity.
func TestSelectionFailureCodesMatchHostCategories(t *testing.T) {
	t.Parallel()
	// Arrange each model-selection source category and its required Host failure category.
	for source, expected := range map[string]string{
		selectionCodeNotFound:     controllerui.FailureCodeNotFound,
		selectionCodeReasoning:    controllerui.FailureCodeReasoning,
		selectionCodeProviderAuth: controllerui.FailureCodeProviderAuth,
		"unknown":                 controllerui.FailureCodeInternal,
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
func expectSessionMutationGate(gate *MockGate, times int) {
	gate.EXPECT().TryAcquire().Times(times).DoAndReturn(func() (func(), bool) { return func() {}, true })
}

// runPreparedCommand executes one admitted command and returns its completed frame.
func runPreparedCommand(t *testing.T, service *Session, command controllerui.Command) (controllerui.Frame, error) {
	t.Helper()
	prepared, err := service.Prepare(t.Context(), command)
	if err != nil {
		return controllerui.Frame{}, err
	}
	defer prepared.Release()
	outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
	if outcome.Err() != nil {
		return controllerui.Frame{}, outcome.Err()
	}
	frame, ok := outcome.Result()
	if !ok {
		return controllerui.Frame{}, errors.New("prepared command did not complete")
	}
	return frame, nil
}

// newCommandForPreparedTest creates one complete operation request with absent optional fields.
func newCommandForPreparedTest(kind controllerui.CommandKind) controllerui.Command {
	return controllerui.Command{
		OperationID: "operation", Kind: kind, Text: mo.None[string](), ProviderID: mo.None[string](),
		ModelID: mo.None[string](), ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID: mo.None[string](), SessionName: mo.None[string](), TargetEntryID: mo.None[string](),
		SummaryMode: controllerui.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
	}
}
