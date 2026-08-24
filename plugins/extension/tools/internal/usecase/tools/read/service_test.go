package read

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

// TestServiceRead verifies continuation notices retain the requested result text.
func TestServiceRead(t *testing.T) {
	t.Parallel()

	reader := NewMockProjectReader(gomock.NewController(t))
	reader.EXPECT().ReadFile(t.Context(), "notes.txt", uint(2), uint(3)).Return(
		Content{Text: "second\nthird", Image: nil, Start: 2, End: 3, Total: 4, Next: 4, OversizedSize: 0}, nil,
	)

	result, err := New(reader).Read(t.Context(), "notes.txt", 2, 3)

	require.NoError(t, err)
	assert.Equal(t, "second\nthird\n[Showing lines 2-3 of 4. Use offset=4 to continue.]", result.Text)
}

// TestServiceReadOversizedLineProvidesBoundedCommand verifies safe shell quoting and a byte bound.
func TestServiceReadOversizedLineProvidesBoundedCommand(t *testing.T) {
	t.Parallel()

	reader := NewMockProjectReader(gomock.NewController(t))
	reader.EXPECT().ReadFile(t.Context(), "dir/a'b.txt", uint(7), uint(1)).Return(
		Content{Text: "", Image: nil, Start: 7, End: 0, Total: 0, Next: 0, OversizedSize: textbudget.MaximumBytes + 1}, nil,
	)

	result, err := New(reader).Read(t.Context(), "dir/a'b.txt", 7, 1)

	require.NoError(t, err)
	assert.Equal(
		t,
		"[Line 7 is 51201 bytes and exceeds the 51200 byte limit. Use `sed -n '7p' 'dir/a'\\''b.txt' | head -c 51200` to inspect that line.]",
		result.Text,
	)
}

// TestServiceReadError preserves project-reader failures.
func TestServiceReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("file is unavailable")
	reader := NewMockProjectReader(gomock.NewController(t))
	reader.EXPECT().ReadFile(t.Context(), "missing.txt", uint(1), uint(0)).Return(Content{Text: "", Image: nil, Start: 0, End: 0, Total: 0, Next: 0, OversizedSize: 0}, readErr)

	result, err := New(reader).Read(t.Context(), "missing.txt", 1, 0)

	assert.Empty(t, result)
	require.ErrorIs(t, err, readErr)
}
