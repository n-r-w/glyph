//go:build !integration

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/errtree"
	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestInitializationCanceledMessage checks that Host UI initialization retains received canceled diagnostics.
func TestInitializationCanceledMessage(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"", "UI initialization canceled source\nUnicode: λ  "} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			// Arrange a canceled UI initialization event with optional diagnostics.
			response := new(uipb.OpenResponse)
			response.SetOperationId("initialize-canceled")
			response.SetEvent(new(uipb.UIEvent))
			response.GetEvent().SetCanceled(operationpb.Canceled_builder{Message: new(text)}.Build())
			// Act through the actual UI event mapper and initialization terminal reader.
			mapped := mapUIOperationEvent(response)
			service := &Service{}
			cause := service.applyInitializationTerminal(mapped, true)
			// Assert that Host retains cancellation identity and full received text without activating the UI.
			require.Equal(t, text, mapped.Message)
			require.False(t, service.ready)
			require.ErrorIs(t, cause, context.Canceled)
			require.Equal(
				t,
				text == "",
				errtree.IsPlainCancellation(cause, func(leaf error) bool { return errors.Is(leaf, context.Canceled) }),
			)
			if text != "" {
				require.ErrorContains(t, cause, text)
			}
		})
	}
}
