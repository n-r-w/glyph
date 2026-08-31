//go:build !integration

package project

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// cancelingReader cancels its context after the first bounded source read.
type cancelingReader struct {
	cancel context.CancelFunc
	text   string
	read   int
}

// Read exposes a bounded prefix, then cancels before the caller can consume the complete source.
func (r *cancelingReader) Read(buffer []byte) (int, error) {
	if r.read >= len(r.text) {
		return 0, io.EOF
	}
	end := min(r.read+32, len(r.text))
	count := copy(buffer, r.text[r.read:end])
	r.read += count
	r.cancel()
	return count, nil
}

func TestGrepReaderStopsRegexMatchingWhenCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	source := &cancelingReader{cancel: cancel, text: strings.Repeat("x", 1_000_000), read: 0}

	_, _, _, err := grepReader(ctx, "large.txt", source, regexp.MustCompile("match$"), 0, 100, newSearchOutput())

	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, source.read, len(source.text))
}

func TestGrepReaderStopsDrainingWhenCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	source := &cancelingReader{cancel: cancel, text: strings.Repeat("x", 1_000_000), read: 0}

	_, _, _, err := grepReader(ctx, "large.txt", source, regexp.MustCompile("x"), 0, 100, newSearchOutput())

	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, source.read, len(source.text))
}

// failingReader returns a non-context source error after its buffered data.
type failingReader struct {
	read bool
	err  error
}

// Read returns one byte before exposing the configured source error.
func (r *failingReader) Read(buffer []byte) (int, error) {
	if r.read {
		return 0, r.err
	}
	r.read = true
	buffer[0] = 'x'
	return 1, nil
}

func TestGrepReaderPreservesSourceError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("source failure")

	_, _, _, err := grepReader(
		t.Context(),
		"file.txt",
		&failingReader{read: false, err: wantErr},
		regexp.MustCompile("missing"),
		0,
		100,
		newSearchOutput(),
	)

	require.ErrorIs(t, err, wantErr)
}
