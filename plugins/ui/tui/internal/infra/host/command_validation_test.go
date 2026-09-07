//go:build !integration

package host

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	presentation "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestMapCommandRejectsMissingSelectedPayload verifies command option ownership remains strict.
func TestMapCommandRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()
	// Arrange tests for mapCommand to verify command option ownership remains strict.

	tests := []presentation.Command{
		commandFixture(presentation.CommandSubmit, mo.None[string]()),
		commandFixture(presentation.CommandResumeSession, mo.None[string]()),
		commandFixture(presentation.CommandSetSessionName, mo.None[string]()),
	}
	for _, command := range tests {
		// Act by invoking mapCommand to exercise command option ownership remains strict.
		_, err := mapCommand(command)
		// Assert command option ownership remains strict.
		require.Error(t, err)
	}
}
