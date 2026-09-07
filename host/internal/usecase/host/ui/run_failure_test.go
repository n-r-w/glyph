//go:build !integration

package ui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestSubmitClassifiesPersistenceCause verifies accepted run categories and complete joined diagnostics.
func TestSubmitClassifiesPersistenceCause(t *testing.T) {
	t.Parallel()
	source := errors.New("disk sync failed with complete diagnostic suffix")
	persistence := fmt.Errorf("%w: %w", agent.ErrPersistenceUnavailable, source)
	for _, test := range []struct {
		// name identifies the source failure scenario.
		name string
		// cause is the complete error returned by run control.
		cause error
		// code is the expected public failure category.
		code string
	}{
		{name: "persistence", cause: persistence, code: controllerui.FailureCodePersistence},
		{
			name: "joined persistence", cause: errors.Join(errors.New("settlement failed"), persistence),
			code: controllerui.FailureCodePersistence,
		},
		{
			name: "unrelated old prefix", cause: errors.New("session persistence failed upstream"),
			code: controllerui.FailureCodeInternal,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange an admitted run with one source failure and isolated output dependencies.
			mocks := gomock.NewController(t)
			output := NewMockOutput(mocks)
			runner := NewMockAgentRunner(mocks)
			auth := NewMockAuthenticator(mocks)
			runner.EXPECT().PrepareRun().Return("run", nil)
			runner.EXPECT().RunPrepared(gomock.Any(), "run", "hello").Return(agent.RunOutcomeFailed, test.cause)
			runner.EXPECT().CancelPrepared("run")
			output.EXPECT().BindProgress(gomock.Any()).Return(func() {})
			output.EXPECT().SetAvailability(gomock.Any()).Return(nil).Times(2)
			auth.EXPECT().IsSignInRequired(test.cause).Return(false)
			service := NewSession(output, runner, auth, nil, nil, nil, nil, nil)
			service.setOperationAvailability(AvailabilityIdle)
			command := newCommandForPreparedTest(controllerui.CommandSubmit)
			command.Text = mo.Some("hello")
			prepared, err := service.Prepare(t.Context(), command)
			require.NoError(t, err)

			// Act after admission and release the prepared work.
			outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
			prepared.Release()

			// Assert classification never replaces any source text or cause.
			require.Equal(t, operation.TerminalStateFailed, outcome.State())
			require.Equal(t, test.code, outcome.Code())
			require.Equal(t, test.cause.Error(), outcome.Err().Error())
			require.ErrorIs(t, outcome.Err(), test.cause)
		})
	}
}
