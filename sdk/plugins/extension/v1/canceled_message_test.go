//go:build !integration

package extensionv1

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/internal/errtree"
	"github.com/n-r-w/glyph/internal/operation"
	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestExtensionCanceledMessage checks that the SDK preserves received cancellation text and identity.
func TestExtensionCanceledMessage(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"", "failure wording stays canceled\nUnicode: λ\n" + strings.Repeat("diagnostic ", 512) + "  "} {
		t.Run(text[:min(len(text), 24)], func(t *testing.T) {
			t.Parallel()
			// Arrange a serialized canceled terminal with optional diagnostics.
			event := new(extensionpb.ExtensionEvent)
			event.SetCanceled(operationpb.Canceled_builder{Message: new(text)}.Build())
			encoded, err := proto.Marshal(event)
			require.NoError(t, err)
			decoded := new(extensionpb.ExtensionEvent)
			require.NoError(t, proto.Unmarshal(encoded, decoded))
			// Act through the actual extension mapper and canceled terminal reader.
			mapped, terminal, err := mapTerminalExtensionEvent(
				operation.Event[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted]{},
				requestExecute,
				decoded,
			)
			require.NoError(t, err)
			_, consumed, cause := (&Operation{}).handleEvent(mapped, nil)
			// Assert that the SDK retains received text without changing cancellation state.
			require.True(t, terminal)
			require.True(t, consumed)
			require.ErrorIs(t, cause, context.Canceled)
			require.IsType(t, &CanceledError{}, cause)
			require.True(
				t,
				errtree.IsCurrentCancellation(
					cause,
					func(leaf error) bool { return errors.Is(leaf, context.Canceled) },
				),
			)
			require.Equal(
				t,
				text == "",
				errtree.IsPlainCancellation(cause, func(leaf error) bool { return errors.Is(leaf, context.Canceled) }),
			)
			if text != "" {
				require.Equal(t, text, mapped.Message)
				require.Contains(t, cause.Error(), text)
				require.Same(t, cause, operationOutcome[struct{}](cause).SourceError())
			}
		})
	}
}
