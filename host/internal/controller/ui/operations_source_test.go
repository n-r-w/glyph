//go:build !integration

package ui

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/internal/operation"
)

//go:generate go tool mockgen -build_constraint=!integration -destination=prepared_mock_test.go -package=ui github.com/n-r-w/glyph/internal/operation Prepared

// TestHostPreparedRetainsCanceledSource checks diagnostic retention through UI outcome conversion.
func TestHostPreparedRetainsCanceledSource(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name   string
		source error
	}{
		{name: "plain cancellation", source: nil},
		{name: "diagnosed cancellation", source: errors.Join(context.Canceled, errors.New("recovered UI diagnostic"))},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange an admitted operation with its original canceled source.
			prepared := NewMockPrepared[Frame, Frame](gomock.NewController(t))
			prepared.EXPECT().
				Run(gomock.Any(), gomock.Any()).
				Return(operation.CanceledWithSource[Frame](scenario.source))
			converted := &hostPrepared{prepared: prepared}
			// Act through the UI controller's outcome conversion.
			outcome := converted.Run(t.Context(), operation.Reporter[Frame]{})
			// Assert source identity and canceled state survive while failure-only error stays absent.
			require.Equal(t, operation.TerminalStateCanceled, outcome.State())
			require.NoError(t, outcome.Err())
			if scenario.source == nil {
				require.NoError(t, outcome.SourceError())
			} else {
				require.NotNil(t, outcome.SourceError())
				require.Same(t, scenario.source, outcome.SourceError())
			}
		})
	}
}
