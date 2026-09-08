//go:build integration

package bash

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// TestFlushCancellationKillsDescendant checks a consumer failure emitted only by the final UTF-8 projection.
func TestFlushCancellationKillsDescendant(t *testing.T) {
	t.Parallel()
	// Arrange an ordinary redirected descendant and a trailing incomplete UTF-8 sequence.
	cause := errors.New("trailing UTF-8 progress delivery failed Ω")
	var identity string
	// Act with a consumer failure only when the stream flush emits its replacement character.
	_, err := New().Run(t.Context(), `sleep 30 >/dev/null 2>&1 & printf '%s %s\n' "$$" "$!"; printf '\342'; exit 0`,
		func(_ bashusecase.Stream, content string) error {
			if content == "?" {
				return cause
			}
			identity += content
			return nil
		})
	fields := strings.Fields(identity)
	require.Len(t, fields, 2)
	pgid, child := processNumber(t, fields[0]), processNumber(t, fields[1])
	t.Cleanup(func() { cleanupGroup(t, pgid) })
	// Assert the callback failure requests real descendant termination, not just operation completion.
	require.ErrorIs(t, err, cause)
	assert.Eventually(t, func() bool { return len(runningGroupMembers(t, pgid)) == 0 },
		time.Second, time.Millisecond, "group=%d child=%d remains live after flush cancellation", pgid, child)
}
