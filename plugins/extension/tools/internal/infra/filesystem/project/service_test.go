package project

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
	edittool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/edit"
	readtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/read"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceReadFileBoundsRequestedLines verifies one requested line range.
func TestServiceReadFileBoundsRequestedLines(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/notes.txt"
	require.NoError(t, writeTestFile(filePath, "first\nsecond\nthird\n"))

	content, err := New().ReadFile(t.Context(), filePath, mo.EmptyableToOption[uint](2), mo.EmptyableToOption[uint](1))

	require.NoError(t, err)
	assert.Equal(t, readtool.Content{
		Text: mo.Some("second\n"), Image: mo.None[readtool.Image](), Start: mo.Some(uint(2)),
		End: mo.Some(uint(2)), Total: mo.Some(uint(3)), Next: mo.Some(uint(3)),
		OversizedSize: mo.None[int64](),
	}, content)
}

// TestServiceReadFileReturnsCompleteExactByteBudget verifies an exact complete byte budget has no continuation.
func TestServiceReadFileReturnsCompleteExactByteBudget(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/notes.txt"
	content := strings.Repeat("x", textbudget.MaximumBytes)
	require.NoError(t, writeTestFile(filePath, content))

	result, err := New().ReadFile(t.Context(), filePath, mo.EmptyableToOption[uint](1), mo.EmptyableToOption[uint](0))

	require.NoError(t, err)
	assert.Equal(t, mo.Some(content), result.Text)
	assert.Equal(t, mo.Some(uint(1)), result.End)
	assert.True(t, result.Next.IsNone())
}

// TestServiceReadFileReturnsCompleteExactLineBudget verifies an exact complete line budget has no continuation.
func TestServiceReadFileReturnsCompleteExactLineBudget(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/notes.txt"
	content := strings.Repeat("x\n", textbudget.MaximumLines)
	require.NoError(t, writeTestFile(filePath, content))

	result, err := New().ReadFile(t.Context(), filePath, mo.EmptyableToOption[uint](1), mo.EmptyableToOption[uint](0))

	require.NoError(t, err)
	assert.Equal(t, mo.Some(content), result.Text)
	assert.Equal(t, mo.Some(uint(textbudget.MaximumLines)), result.End)
	assert.True(t, result.Next.IsNone())
}

// TestServiceReadFileReservesLineForContinuationNotice verifies partial results reserve one notice line.
func TestServiceReadFileReservesLineForContinuationNotice(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/notes.txt"
	var source strings.Builder
	for line := 1; line <= textbudget.MaximumLines+1; line++ {
		source.WriteString("x\n")
	}
	require.NoError(t, writeTestFile(filePath, source.String()))

	content, err := New().ReadFile(t.Context(), filePath, mo.EmptyableToOption[uint](1), mo.EmptyableToOption[uint](0))

	require.NoError(t, err)
	assert.Equal(t, mo.Some(uint(textbudget.MaximumLines-1)), content.End)
	assert.Equal(t, mo.Some(uint(textbudget.MaximumLines)), content.Next)
}

// TestServiceReadFileKeepsPartialResultWithinCompleteBudget verifies text plus notice fits both budgets.
func TestServiceReadFileKeepsPartialResultWithinCompleteBudget(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/notes.txt"
	content := strings.Repeat("x\n", textbudget.MaximumLines+1)
	require.NoError(t, writeTestFile(filePath, content))

	result, err := readtool.New(New()).Read(t.Context(), filePath, mo.EmptyableToOption[uint](1), mo.EmptyableToOption[uint](0))

	require.NoError(t, err)
	text := result.Text.OrEmpty()
	assert.LessOrEqual(t, len(text), textbudget.MaximumBytes)
	assert.LessOrEqual(t, strings.Count(text, "\n")+1, textbudget.MaximumLines)
	assert.Contains(t, text, "Use offset=2000 to continue.")
}

// TestIsAnimatedPNGRejectsNonChunkMarker verifies raw bytes do not masquerade as an animation chunk.
func TestIsAnimatedPNGRejectsNonChunkMarker(t *testing.T) {
	t.Parallel()

	staticPNG := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte("acTL")...)

	assert.False(t, isAnimatedPNG(staticPNG))
}

// TestServiceReadFileReturnsBoundedNoticeWhenFirstLineLeavesNoContinuationRoom preserves a usable continuation.
func TestServiceReadFileReturnsBoundedNoticeWhenFirstLineLeavesNoContinuationRoom(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/notes.txt"
	content := strings.Repeat("x", textbudget.MaximumBytes-1) + "\nsecond\n"
	require.NoError(t, writeTestFile(filePath, content))

	result, err := readtool.New(New()).Read(t.Context(), filePath, mo.EmptyableToOption[uint](1), mo.EmptyableToOption[uint](0))

	require.NoError(t, err)
	text := result.Text.OrEmpty()
	assert.NotContains(t, text, strings.Repeat("x", textbudget.MaximumBytes-1))
	assert.Contains(t, text, "Line 1")
	assert.Contains(t, text, "head -c 51200")
	assert.Contains(t, text, "Use offset=2 to continue.")
	assert.LessOrEqual(t, len(text), textbudget.MaximumBytes)
}

// TestServiceReadFileReadsEmptyFileAtFirstOffset returns empty content at the first line.
func TestServiceReadFileReadsEmptyFileAtFirstOffset(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/empty.txt"
	require.NoError(t, writeTestFile(filePath, ""))

	for name, offset := range map[string]uint{"default": 0, "explicit": 1} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			content, err := New().ReadFile(t.Context(), filePath, mo.EmptyableToOption[uint](offset), mo.EmptyableToOption[uint](0))

			require.NoError(t, err)
			assert.Equal(t, mo.Some(""), content.Text)
			assert.Equal(t, mo.Some(uint(0)), content.End)
			assert.Equal(t, mo.Some(uint(0)), content.Total)
			assert.True(t, content.Next.IsNone())
		})
	}

	_, err := New().ReadFile(t.Context(), filePath, mo.EmptyableToOption[uint](2), mo.EmptyableToOption[uint](0))
	require.Error(t, err)
}

// TestServiceReadFileRejectsAnimatedPNG verifies APNG bytes do not produce typed image content.
func TestServiceReadFileRejectsAnimatedPNG(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/image.data"
	apng := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte{0, 0, 0, 8, 'a', 'c', 'T', 'L'}...)
	require.NoError(t, os.WriteFile(filePath, apng, 0o600))

	content, err := New().ReadFile(t.Context(), filePath, mo.EmptyableToOption[uint](1), mo.EmptyableToOption[uint](1))

	require.NoError(t, err)
	assert.True(t, content.Image.IsNone())
}

// TestServiceReadFileDetectsImageBytes verifies extension-independent static image detection.
func TestServiceReadFileDetectsImageBytes(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		contentType string
		image       []byte
	}{
		"BMP":  {"image/bmp", []byte{'B', 'M', 0, 0}},
		"GIF":  {"image/gif", []byte{'G', 'I', 'F', '8', '9', 'a', 1, 0, 1, 0, 0, 0, 0}},
		"JPEG": {"image/jpeg", []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0, 0, 0}},
		"PNG":  {"image/png", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}},
		"WebP": {"image/webp", []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P', 'V', 'P'}},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			filePath := t.TempDir() + "/image.data"
			require.NoError(t, os.WriteFile(filePath, testCase.image, 0o600))

			content, err := New().ReadFile(t.Context(), filePath, mo.EmptyableToOption[uint](1), mo.EmptyableToOption[uint](1))

			require.NoError(t, err)
			image, ok := content.Image.Get()
			require.True(t, ok)
			assert.Equal(t, testCase.contentType, image.MediaType)
			assert.Equal(t, testCase.image, image.Data)
			assert.True(t, content.Start.IsNone())
			assert.True(t, content.End.IsNone())
			assert.True(t, content.Total.IsNone())
		})
	}
}

// TestServiceReadFileRetainsLineAcrossReaderFragments verifies fragment assembly preserves full lines.
func TestServiceReadFileRetainsLineAcrossReaderFragments(t *testing.T) {
	t.Parallel()

	line := string(make([]byte, 8192))
	filePath := t.TempDir() + "/notes.txt"
	require.NoError(t, writeTestFile(filePath, line+"\n"))

	content, err := New().ReadFile(t.Context(), filePath, mo.EmptyableToOption[uint](1), mo.EmptyableToOption[uint](1))

	require.NoError(t, err)
	assert.Equal(t, mo.Some(line+"\n"), content.Text)
}

// TestServiceReadFileReportsOversizedFirstLine verifies first-line oversize metadata.
func TestServiceReadFileReportsOversizedFirstLine(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/notes.txt"
	require.NoError(t, writeTestFile(filePath, string(make([]byte, textbudget.MaximumBytes+1))))

	content, err := New().ReadFile(t.Context(), filePath, mo.EmptyableToOption[uint](1), mo.EmptyableToOption[uint](1))

	require.NoError(t, err)
	assert.Equal(t, mo.Some(uint(1)), content.Start)
	assert.True(t, content.End.IsNone())
	assert.Equal(t, mo.Some(int64(textbudget.MaximumBytes+1)), content.OversizedSize)
	assert.Equal(t, mo.Some(""), content.Text)
}

// TestServiceWriteFileCreatesParentDirectories verifies writes create missing parents.
func TestServiceWriteFileCreatesParentDirectories(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/nested/notes.txt"
	err := New().WriteFile(t.Context(), filePath, "created")

	require.NoError(t, err)
	content, readErr := os.ReadFile(filePath)
	require.NoError(t, readErr)
	assert.Equal(t, "created", string(content))
}

// TestServiceEditKeepsOriginalBytesForOverlappingOccurrences verifies overlapping matches reject atomically.
func TestServiceEditKeepsOriginalBytesForOverlappingOccurrences(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/notes.txt"
	require.NoError(t, writeTestFile(filePath, "aaa"))

	err := edittool.New(New()).Edit(t.Context(), filePath, []extensioncontroller.Replacement{{OldText: "aa", NewText: "b"}})

	require.ErrorContains(t, err, "occur exactly once")
	content, readErr := os.ReadFile(filePath)
	require.NoError(t, readErr)
	assert.Equal(t, "aaa", string(content))
}

// TestServiceEditKeepsOriginalBytesWhenAnyReplacementIsInvalid verifies batch validation is atomic.
func TestServiceEditKeepsOriginalBytesWhenAnyReplacementIsInvalid(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/notes.txt"
	original := "alpha beta gamma"
	require.NoError(t, writeTestFile(filePath, original))

	err := edittool.New(New()).Edit(t.Context(), filePath, []extensioncontroller.Replacement{
		{OldText: "alpha", NewText: "one"},
		{OldText: "missing", NewText: "two"},
	})

	require.Error(t, err)
	content, readErr := os.ReadFile(filePath)
	require.NoError(t, readErr)
	assert.Equal(t, original, string(content))
}

// TestServiceSerializesWriteAndUpdateForSamePath verifies same-path mutations cannot interleave.
func TestServiceSerializesWriteAndUpdateForSamePath(t *testing.T) {
	t.Parallel()

	filePath := t.TempDir() + "/notes.txt"
	require.NoError(t, writeTestFile(filePath, "before"))
	service := New()
	entered := make(chan struct{})
	release := make(chan struct{})
	updateDone := make(chan error, 1)
	go func() {
		updateDone <- service.UpdateFile(t.Context(), filePath, func(content []byte) ([]byte, error) {
			close(entered)
			<-release
			return append(content, " edit"...), nil
		})
	}()
	<-entered
	writeDone := make(chan error, 1)
	go func() { writeDone <- service.WriteFile(t.Context(), filePath, "write") }()
	require.Eventually(t, func() bool {
		absolutePath, err := canonicalPath(filePath)
		if err != nil {
			return false
		}
		service.locks.mutex.Lock()
		defer service.locks.mutex.Unlock()
		return service.locks.locks[absolutePath].users == 2
	}, time.Second, time.Millisecond)
	select {
	case err := <-writeDone:
		assert.Failf(t, "write completed before update", "write error: %v", err)
	default:
	}
	close(release)
	require.NoError(t, <-updateDone)
	require.NoError(t, <-writeDone)
	content, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "write", string(content))
}

// TestReadImageDataChecksCancellationAfterRead verifies cancellation during a full image read.
func TestReadImageDataChecksCancellationAfterRead(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	reader, writer := io.Pipe()
	result := make(chan error, 1)
	go func() {
		_, err := readImageData(ctx, reader)
		result <- err
	}()

	_, err := writer.Write([]byte{0xff, 0xd8, 0xff})
	require.NoError(t, err)
	cancel()
	require.NoError(t, writer.Close())

	require.ErrorIs(t, <-result, context.Canceled)
}

// TestServiceReadFileCanceled verifies cancellation before filesystem work.
func TestServiceReadFileCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	content, err := New().ReadFile(ctx, t.TempDir()+"/notes.txt", mo.EmptyableToOption[uint](1), mo.EmptyableToOption[uint](1))

	assert.Empty(t, content)
	require.ErrorIs(t, err, context.Canceled)
}

// writeTestFile creates a private file fixture.
func writeTestFile(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
