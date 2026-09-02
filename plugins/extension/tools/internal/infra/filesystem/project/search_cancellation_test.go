//go:build !integration

package project

//go:generate go tool mockgen -build_constraint=!integration -destination=io_reader_mock_test.go -package=project -mock_names=Reader=MockIOReader io Reader

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newCancelingReader returns a mock reader that cancels after each bounded source read.
func newCancelingReader(
	t *testing.T,
	cancel context.CancelFunc,
	text string,
) (*MockIOReader, func() int) {
	t.Helper()
	read := 0
	source := NewMockIOReader(gomock.NewController(t))
	source.EXPECT().Read(gomock.Any()).AnyTimes().DoAndReturn(func(buffer []byte) (int, error) {
		if read >= len(text) {
			return 0, io.EOF
		}
		end := min(read+32, len(text))
		count := copy(buffer, text[read:end])
		read += count
		cancel()
		return count, nil
	})
	return source, func() int { return read }
}

func TestGrepReaderStopsRegexMatchingWhenCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	text := strings.Repeat("x", 1_000_000)
	source, readCount := newCancelingReader(t, cancel, text)

	_, _, _, err := grepReader(ctx, "large.txt", source, regexp.MustCompile("match$"), 0, 100, newSearchOutput())

	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, readCount(), len(text))
}

func TestGrepReaderStopsDrainingWhenCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	text := strings.Repeat("x", 1_000_000)
	source, readCount := newCancelingReader(t, cancel, text)

	_, _, _, err := grepReader(ctx, "large.txt", source, regexp.MustCompile("x"), 0, 100, newSearchOutput())

	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, readCount(), len(text))
}

func TestGrepReaderPreservesSourceError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("source failure")
	read := false
	source := NewMockIOReader(gomock.NewController(t))
	source.EXPECT().Read(gomock.Any()).AnyTimes().DoAndReturn(func(buffer []byte) (int, error) {
		if read {
			return 0, wantErr
		}
		read = true
		buffer[0] = 'x'
		return 1, nil
	})

	_, _, _, err := grepReader(
		t.Context(),
		"file.txt",
		source,
		regexp.MustCompile("missing"),
		0,
		100,
		newSearchOutput(),
	)

	require.ErrorIs(t, err, wantErr)
}
