//go:build !integration

package headless

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestRendererWriteErrorShowsCurrentCategory verifies the visible category of current causes and the complete text.
func TestRendererWriteErrorShowsCurrentCategory(t *testing.T) {
	t.Parallel()
	coded := extensionsdk.Fail("EXTENSION_FAILED", errors.New(`middleware handler "input-text": handler failed`))
	for name, test := range map[string]struct {
		err      error
		expected string
	}{
		"coded current cause": {
			err: fmt.Errorf("run headless agent: process user input: %w", coded),
			expected: "[error] EXTENSION_FAILED: run headless agent: process user input: " +
				"middleware handler \"input-text\": handler failed\n",
		},
		"logical model failure": {
			err: fmt.Errorf("run headless agent: %w", &modelexecution.LogicalFailureError{
				Category: modelexecution.FailureRetryExhausted, Cause: errors.New("provider unavailable"),
			}),
			expected: "[error] RETRY_EXHAUSTED: run headless agent: provider unavailable\n",
		},
		"uncategorized": {
			err:      errors.New("provider failed"),
			expected: "[error] provider failed\n",
		},
		"category only in diagnostic history": {
			err:      diagnostic.NewCurrentCause(context.Canceled, coded),
			expected: "[error] context canceled\nmiddleware handler \"input-text\": handler failed\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one renderer with captured error output.
			var stderr bytes.Buffer
			renderer := NewRenderer(&bytes.Buffer{}, &stderr)

			// Act by rendering the terminal error.
			require.NoError(t, renderer.WriteError(test.err))

			// Assert the visible category and the complete error text.
			assert.Equal(t, test.expected, stderr.String())
		})
	}
}
