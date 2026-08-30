package project

import (
	"bufio"
	"context"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadTextContentCanceled verifies cancellation interrupts a text scan before the next fragment.
func TestReadTextContentCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	content, err := readTextContent(
		ctx,
		bufio.NewReader(strings.NewReader("line\n")),
		"notes.txt",
		mo.EmptyableToOption[uint](1),
		mo.EmptyableToOption[uint](1),
	)

	assert.Empty(t, content)
	require.ErrorIs(t, err, context.Canceled)
}
