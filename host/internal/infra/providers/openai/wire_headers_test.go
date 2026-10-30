//go:build !integration

package openai

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestWireHeaderValidationRejectsControls rejects invalid HTTP values before a serializer accepts a replacement.
func TestWireHeaderValidationRejectsControls(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"bad\x01", "bad\x1f", "bad\x7f", "bad\r\n"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			// Arrange an authenticated original and one invalid public replacement.
			headers := http.Header{"Authorization": []string{"Bearer private"}, "X-Original": []string{"unchanged"}}
			// Act through the validation shared by per-handler and final bridge admission.
			err := replaceWireHeaders(headers, modelexecution.WireHeaders{"X-New": []string{value}})
			// Assert validation fails atomically before changing authentication or public values.
			require.Error(t, err)
			require.Equal(t, "Bearer private", headers.Get("Authorization"))
			require.Equal(t, "unchanged", headers.Get("X-Original"))
		})
	}
}
