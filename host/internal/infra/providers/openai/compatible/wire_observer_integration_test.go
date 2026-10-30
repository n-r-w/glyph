//go:build integration

package compatible

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// newSuccessfulWireCallbacks supplies a preserving observer that verifies safe metadata before stream completion.
func newSuccessfulWireCallbacks(t *testing.T, observed *bool) modelexecution.WireCallbacks {
	t.Helper()
	return modelexecution.WireCallbacks{
		Headers: func(
			_ context.Context, h modelexecution.WireHeaders, _ func(modelexecution.WireHeaders) error,
		) (modelexecution.WireHeaders, error) {
			return h, nil
		},
		Body: func(_ context.Context, b []byte, _ func([]byte) error) (modelexecution.WireBodyResult, error) {
			return modelexecution.WireBodyResult{Body: b, Replaced: false}, nil
		},
		Response: func(_ context.Context, response modelexecution.WireResponse) error {
			*observed = true
			assert.Equal(t, int64(http.StatusOK), response.Status)
			assert.NotContains(t, response.Headers, "Set-Cookie")
			return nil
		},
		Failed: func(err error) { t.Errorf("successful observer must not report middleware failure: %v", err) },
	}
}
