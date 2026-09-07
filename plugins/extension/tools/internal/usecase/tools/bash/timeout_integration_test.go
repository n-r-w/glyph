//go:build integration

package bash_test

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
	bashprocess "github.com/n-r-w/glyph/plugins/extension/tools/internal/infra/process/bash"
	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// TestServiceTimeoutTerminatesProcess preserves progress, partial output, and timeout text with a real process group.
func TestServiceTimeoutTerminatesProcess(t *testing.T) {
	t.Parallel()

	// Arrange a real process that writes output before waiting beyond the execution limit.
	service := bashusecase.New(bashprocess.New())
	var progress []extensioncontroller.BashProgress

	// Act by running the process under a usecase-owned fractional timeout.
	result, err := service.Execute(t.Context(), extensioncontroller.BashCommand{
		Text: "printf started; sleep 30", Timeout: mo.Some(0.1),
	}, func(event extensioncontroller.BashProgress) error {
		progress = append(progress, event)
		return nil
	})

	// Assert process termination retains the exact cause, earlier output, and progress ordering.
	require.EqualError(t, err, "run bash command: bash command timed out after 0.1 seconds")
	require.Contains(t, result.Text, "started")
	require.Contains(t, result.Text, "bash command timed out after 0.1 seconds")
	require.Equal(t, -1, result.ExitCode)
	require.GreaterOrEqual(t, len(progress), 2)
	require.Equal(t, extensioncontroller.BashProgressStatus, progress[0].Channel)
	require.Equal(t, extensioncontroller.BashProgressStdout, progress[1].Channel)
}
