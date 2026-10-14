//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"testing"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestCommandCanceledSource checks that the adapter retains cancellation errors with diagnostics
// after a session mutation.
func TestCommandCanceledSource(t *testing.T) {
	t.Parallel()
	// Arrange a canceled session mutation with a diagnostic error whose identity the test can check.
	control := NewMockActiveSessions(gomock.NewController(t))
	gate := NewMockGate(gomock.NewController(t))
	gate.EXPECT().TryAcquire().Return(func() {}, true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := diagnostic.NewCurrentCause(context.Canceled, errors.New("session mutation cancellation diagnostics"))
	control.EXPECT().
		SetActiveName(gomock.Any(), "new name").
		DoAndReturn(func(context.Context, string) (session.Info, error) {
			cancel()
			return session.Info{}, source
		})
	service := New(nil, nil, testStateQuery(t, false), control, nil, gate, testRunOutput(t), nil, nil)
	command := testProgrammaticCommand("name-source", controller.CommandSetSessionName)
	command.SessionName = mo.Some("new name")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	// Act through the Programmatic prepared operation.
	outcome := prepared.Run(ctx, operation.Reporter[controller.OperationProgress]{})
	// Assert that the adapter retains the source while keeping canceled disposition.
	require.Equal(t, operation.TerminalStateCanceled, outcome.State())
	require.Same(t, source, outcome.SourceError())
	require.NoError(t, outcome.Err())
}
