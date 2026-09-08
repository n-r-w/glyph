//go:build integration

package bash_test

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
	bashprocess "github.com/n-r-w/glyph/plugins/extension/tools/internal/infra/process/bash"
	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// TestServiceCancellationPreservesProgressFailure exercises actual process output during caller cancellation.
func TestServiceCancellationPreservesProgressFailure(t *testing.T) {
	t.Parallel()
	// Arrange the production bash usecase and process adapter with an independent output consumer failure.
	service := bashusecase.New(bashprocess.New())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cause := errors.New("bash progress delivery failed during cancellation Ω")

	// Act by canceling the caller before its progress callback returns the independent failure.
	_, err := service.Execute(ctx, extensioncontroller.BashCommand{
		Text: "printf started", Timeout: mo.None[float64](),
	}, func(event extensioncontroller.BashProgress) error {
		if event.Channel == extensioncontroller.BashProgressStdout {
			cancel()
			return cause
		}
		return nil
	})

	// Assert the real process path retains both cancellation and the independent progress source.
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, context.Canceled)
}
