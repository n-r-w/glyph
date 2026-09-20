//go:build !integration

package ui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/n-r-w/glyph/host/internal/domain/authentication"
	"github.com/n-r-w/glyph/host/internal/domain/model"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestCompactionFailureCategoryRemainsPublic verifies UI failure mapping keeps orchestration identity.
func TestCompactionFailureCategoryRemainsPublic(t *testing.T) {
	t.Parallel()
	// Arrange one typed compaction failure.
	err := uiCompactionCategoryError{code: "COMPACTION_FAILED"}

	// Act through the direct manual-compaction failure mapping.
	code := sessionOperationFailureCode(err)

	// Assert the public operation category remains exact.
	require.Equal(t, "COMPACTION_FAILED", code)
}

// uiCompactionCategoryError supplies one stable test-only orchestration category.
type uiCompactionCategoryError struct {
	// code is the stable public failure category.
	code string
}

// Error returns the category as complete test failure text.
func (e uiCompactionCategoryError) Error() string { return e.code }

// CompactionFailureCode returns the orchestration-owned category.
func (e uiCompactionCategoryError) CompactionFailureCode() string { return e.code }

// TestManualCompactionPreservesCommittedFailureCategory verifies production UI operation mapping.
func TestManualCompactionPreservesCommittedFailureCategory(t *testing.T) {
	t.Parallel()
	// Arrange post-commit publication, observer, and joined failures.
	publication := errors.New("publication failed after commit")
	observer := errors.New("observer failed after commit")
	tests := []struct {
		name         string
		failure      error
		expectedCode string
	}{
		{
			name:         "publication",
			failure:      uiPostCommitFailure{code: controllerui.FailureCodeInternal, cause: publication},
			expectedCode: controllerui.FailureCodeInternal,
		},
		{
			name:         "observer",
			failure:      uiPostCommitFailure{code: controllerui.FailureCodeExtensionFailed, cause: observer},
			expectedCode: controllerui.FailureCodeExtensionFailed,
		},
		{name: "joined", failure: errors.Join(
			uiPostCommitFailure{code: controllerui.FailureCodeInternal, cause: publication},
			uiPostCommitFailure{code: controllerui.FailureCodeExtensionFailed, cause: observer},
		), expectedCode: controllerui.FailureCodeInternal},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			committed := session.Entry{
				ID: "compaction", ParentID: mo.None[string](), CreatedAt: time.Unix(1, 0),
				Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
				Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
				Extension: mo.None[session.ExtensionEnvelope](), ExtensionMessage: mo.None[session.ExtensionMessage](),
				EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
				Compaction: mo.Some(session.CompactionEntry{
					Summary: "summary", FirstKeptEntryID: "kept",
					Source: session.CompactionSource{
						ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
					},
					EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
				}),
			}
			// Act through the production UI completion mapping used by the prepared operation.
			frame, err := compactionCompletionFrame(
				ManualCompactionResult{Committed: mo.Some(committed), Canceled: false}, testCase.failure,
			)

			// Assert committed state, complete text, and stable category survive together.
			require.ErrorIs(t, err, testCase.failure)
			require.Len(t, frame.SessionEntries, 1)
			require.Equal(t, testCase.failure.Error(), frame.CompactionError.MustGet())
			code, codePresent := frame.CompactionFailureCode.Get()
			require.True(t, codePresent)
			require.Equal(t, testCase.expectedCode, code)
		})
	}
}

// uiPostCommitFailure keeps a stable category and complete post-commit cause.
type uiPostCommitFailure struct {
	// code is the stable public category.
	code string
	// cause is the complete underlying failure.
	cause error
}

// Error returns the complete underlying failure text.
func (e uiPostCommitFailure) Error() string { return e.cause.Error() }

// Unwrap preserves the underlying failure.
func (e uiPostCommitFailure) Unwrap() error { return e.cause }

// CompactionFailureCode returns the stable category.
func (e uiPostCommitFailure) CompactionFailureCode() string { return e.code }

// TestInitializeFailurePreservesCause verifies startup delivery ownership.
func TestInitializeFailurePreservesCause(t *testing.T) {
	t.Parallel()

	// Arrange one unique wrapped initialization delivery failure.
	controller := gomock.NewController(t)
	channel := NewMockOutput(controller)
	source := errors.New("UI process exited during initialization")
	wrapped := fmt.Errorf("send initialization frame: %w", source)
	channel.EXPECT().Initialize(gomock.Any(), gomock.Any()).Return(wrapped)
	catalog := NewMockModelCatalog(controller)
	catalog.EXPECT().Models().Return(nil)
	catalog.EXPECT().ActiveSelection().Return(model.Selection{})
	active := NewMockActiveSessions(controller)
	active.EXPECT().ActiveInformation().Return(session.Info{}, session.Statistics{})
	activation := NewMockRuntimeActivation(controller)
	retry := NewMockRetryControl(controller)
	retry.EXPECT().RetryPolicy().Return(false, int64(0), nil, time.Duration(0))
	service := NewSession(
		channel, NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		catalog, active, nil, nil, activation, nil, retry)

	// Act through Host startup.
	err := service.Initialize(t.Context())

	// Assert cause preservation and no Host activation or operation receipt.
	require.ErrorIs(t, err, source)
	require.ErrorContains(t, err, wrapped.Error())
}

// TestActivationCleanupCancelsAndJoinsAuthenticationCheck verifies startup worker ownership.
func TestActivationCleanupCancelsAndJoinsAuthenticationCheck(t *testing.T) {
	t.Parallel()

	// Arrange a startup authentication check blocked until its owned context is canceled.
	controller := gomock.NewController(t)
	channel := NewMockOutput(controller)
	authenticator := NewMockAuthenticator(controller)
	activation := NewMockRuntimeActivation(controller)
	activated := activation.EXPECT().Activate(gomock.Any())
	activation.EXPECT().StopReporting().After(activated)
	started := make(chan struct{})
	stopped := make(chan struct{})
	authenticator.EXPECT().
		CheckAuthentication(gomock.Any()).
		After(activated).
		DoAndReturn(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(stopped)
			return context.Cause(ctx)
		})
	authenticator.EXPECT().IsSignInRequired(gomock.Any()).AnyTimes().Return(false)
	service := NewSession(
		channel, NewMockAgentRunner(controller), authenticator, NewMockModelCatalog(controller), nil, nil,
		nil, activation, nil, nil)

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
		NewMockModelCatalog(controller), NewMockActiveSessions(controller), nil, nil, nil, nil, nil)

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
		NewMockModelCatalog(controller), control, nil, gate, nil, nil, nil)

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

// TestPreparedCancellationClassifiesPureAndMixedErrors verifies original mixed failure preservation.
func TestPreparedCancellationClassifiesPureAndMixedErrors(t *testing.T) {
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
			runErr := error(context.Canceled)
			if test.mixed {
				independentCause := errors.New("unique UI execution source")
				runErr = fmt.Errorf(
					"execute extension tool %q: %w",
					"extension-tool",
					errors.Join(context.Canceled, independentCause),
				)
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

			// Assert pure cancellation stays canceled and mixed failure keeps the original object.
			assert.Equal(t, test.expectedState, outcome.State())
			if test.mixed {
				require.Same(t, runErr, outcome.Err())
			}
		})
	}
}

// TestPreparedCompactionCancellationMatrix verifies owner cancellation and typed compaction precedence.
func TestPreparedCompactionCancellationMatrix(t *testing.T) {
	t.Parallel()
	independentCause := errors.New("independent UI compaction failure")
	for _, testCase := range []struct {
		name          string
		cancelOwner   bool
		runErr        error
		expectedState operation.TerminalState
	}{
		{
			name: "active owner with handler transport cancellation", cancelOwner: false,
			runErr:        uiPostCommitFailure{code: controllerui.FailureCodeExtensionFailed, cause: context.Canceled},
			expectedState: operation.TerminalStateFailed,
		},
		{
			name: "canceled owner with cancellation only", cancelOwner: true,
			runErr: context.Canceled, expectedState: operation.TerminalStateCanceled,
		},
		{
			name: "canceled owner with independent typed failure", cancelOwner: true,
			runErr: uiPostCommitFailure{
				code:  controllerui.FailureCodeExtensionFailed,
				cause: errors.Join(context.Canceled, independentCause),
			},
			expectedState: operation.TerminalStateFailed,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange the UI operation owner and one direct compaction terminal.
			ctx, cancel := context.WithCancel(t.Context())
			if testCase.cancelOwner {
				cancel()
			} else {
				t.Cleanup(cancel)
			}
			prepared := &preparedUIOperation{
				run: func(context.Context, operation.Reporter[controllerui.Frame]) (controllerui.Frame, error) {
					return controllerui.Frame{}, testCase.runErr
				},
				failureCode: func(err error) string {
					code, found := directCompactionFailureCode(err)
					require.True(t, found)
					return code
				},
				release: func() {}, releaseOnce: sync.Once{},
			}

			// Act through the production UI operation terminal owner.
			outcome := prepared.Run(ctx, operation.Reporter[controllerui.Frame]{})

			// Assert pure owner abort is canceled and typed handler failure remains failed.
			require.Equal(t, testCase.expectedState, outcome.State())
			if testCase.expectedState == operation.TerminalStateFailed {
				require.Equal(t, controllerui.FailureCodeExtensionFailed, outcome.Code())
				require.ErrorIs(t, outcome.Err(), testCase.runErr)
			}
			if testCase.name == "canceled owner with independent typed failure" {
				require.ErrorIs(t, outcome.Err(), independentCause)
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
		NewMockModelCatalog(controller), control, nil, gate, nil, nil, nil)

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
		selectionCodeModelUnavailable:     controllerui.FailureCodeModelUnavailable,
		selectionCodeProviderAuth:         controllerui.FailureCodeProviderAuth,
		selectionCodeExtensionRejected:    controllerui.FailureCodeExtensionRejected,
		selectionCodeExtensionUnavailable: controllerui.FailureCodeExtension,
		"unknown":                         controllerui.FailureCodeInternal,
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

// ModelSelectionCode returns the stable source category.
func (e selectionCodeTestError) ModelSelectionCode() string { return string(e) }

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
		AuthenticationMethod: authentication.MethodUnspecified,
		OperationID:          "operation", Kind: kind, Text: mo.None[string](), ProviderID: mo.None[string](),
		ModelID: mo.None[string](), ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID: mo.None[string](), SessionName: mo.None[string](), TargetEntryID: mo.None[string](),
		SummaryMode: controllerui.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
	}
}
