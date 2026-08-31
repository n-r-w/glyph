//go:build !integration

package headless

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

// TestServiceExecuteAcceptsOnlyCompletedRun verifies terminal outcome and error handling.
func TestServiceExecuteAcceptsOnlyCompletedRun(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		outcome agent.RunOutcome
		runErr  error
		wantErr bool
	}{
		"completed": {outcome: agent.RunOutcomeCompleted, runErr: nil, wantErr: false},
		"failed":    {outcome: agent.RunOutcomeFailed, runErr: errors.New("provider failed"), wantErr: true},
		"aborted":   {outcome: agent.RunOutcomeAborted, runErr: context.Canceled, wantErr: true},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner := NewMockAgentRunner(gomock.NewController(t))
			runner.EXPECT().Run(t.Context(), "request").Return(testCase.outcome, testCase.runErr)
			service := New(runner)

			err := service.Execute(t.Context(), "request")

			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
