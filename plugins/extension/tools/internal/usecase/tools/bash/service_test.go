package bash

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

// TestServiceExecute emits status before forwarding process output.
func TestServiceExecute(t *testing.T) {
	t.Parallel()

	runner := NewMockProcessRunner(gomock.NewController(t))
	runner.EXPECT().Run(t.Context(), "printf ok", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, handler ProgressHandler) (ProcessResult, error) {
			require.NoError(t, handler(StreamStdout, "ok"))
			return ProcessResult{Stdout: "ok", Stderr: "", ExitCode: 0}, nil
		},
	)
	events := make([]string, 0, 2)

	result, err := New(runner).Execute(t.Context(), "printf ok", func(progress extensioncontroller.BashProgress) error {
		events = append(events, fmt.Sprintf("%d:%s", progress.Channel, progress.Content))
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"0:running", "1:ok"}, events)
	assert.Equal(t, extensioncontroller.BashResult{Stdout: "ok", Stderr: "", ExitCode: 0}, result)
}
