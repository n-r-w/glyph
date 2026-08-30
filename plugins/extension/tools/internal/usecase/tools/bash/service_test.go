package bash

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

// TestServiceExecute emits status before forwarding process output.
func TestServiceExecute(t *testing.T) {
	t.Parallel()

	runner := NewMockProcessRunner(gomock.NewController(t))
	runner.EXPECT().Run(t.Context(), "printf ok", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, handler ProgressHandler) (ProcessResult, error) {
			require.NoError(t, handler(StreamStdout, "ok"))
			return ProcessResult{
				Output: "ok\n\n[Exit code: 0]\n", ExitCode: 0,
				Truncation: textbudget.Truncation{
					Truncated: false, TotalBytes: 0, TotalLines: 0, FullOutputPath: "",
				},
			}, nil
		},
	)
	events := make([]string, 0, 2)

	result, err := New(runner).Execute(t.Context(), "printf ok", func(progress extensioncontroller.BashProgress) error {
		events = append(events, fmt.Sprintf("%d:%s", progress.Channel, progress.Content))
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"0:running", "1:ok"}, events)
	assert.Equal(t, extensioncontroller.BashResult{
		Text: "ok\n\n[Exit code: 0]\n", ExitCode: 0,
		Truncation: textbudget.Truncation{
			Truncated: false, TotalBytes: 0, TotalLines: 0, FullOutputPath: "",
		},
	}, result)
}

// TestServiceExecutePreservesTimeoutOutput keeps bounded output available to the controller.
func TestServiceExecutePreservesTimeoutOutput(t *testing.T) {
	t.Parallel()

	timeoutErr := errors.New("bash command timed out after 1 seconds")
	runner := NewMockProcessRunner(gomock.NewController(t))
	runner.EXPECT().Run(t.Context(), "sleep 30", gomock.Any()).Return(
		ProcessResult{
			Output: "started\n\n[bash command timed out after 1 seconds]\n", ExitCode: -1,
			Truncation: textbudget.Truncation{
				Truncated: false, TotalBytes: 0, TotalLines: 0, FullOutputPath: "",
			},
		},
		timeoutErr,
	)

	result, err := New(runner).Execute(
		t.Context(),
		"sleep 30",
		func(extensioncontroller.BashProgress) error { return nil },
	)

	require.ErrorIs(t, err, timeoutErr)
	assert.Equal(t, "started\n\n[bash command timed out after 1 seconds]\n", result.Text)
}
