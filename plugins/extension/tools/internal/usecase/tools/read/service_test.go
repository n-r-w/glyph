//go:build !integration

package read

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

// TestServiceRead verifies continuation notices retain the requested result text.
func TestServiceRead(t *testing.T) {
	t.Parallel()

	reader := NewMockProjectReader(gomock.NewController(t))
	reader.EXPECT().ReadFile(t.Context(), "notes.txt", mo.Some(uint(2)), mo.Some(uint(3))).Return(
		Content{
			Text: mo.Some("second\nthird"), Image: mo.None[Image](), Start: mo.Some(uint(2)),
			End: mo.Some(uint(3)), Total: mo.Some(uint(4)), Next: mo.Some(uint(4)),
			OversizedSize: mo.None[int64](),
		}, nil,
	)

	result, err := New(reader).Read(t.Context(), "notes.txt", mo.Some(uint(2)), mo.Some(uint(3)))

	require.NoError(t, err)
	assert.Equal(t, mo.Some("second\nthird\n[Showing lines 2-3 of 4. Use offset=4 to continue.]"), result.Text)
	assert.True(t, result.Image.IsNone())
}

// TestServiceReadImage maps the image alternative without a text alternative.
func TestServiceReadImage(t *testing.T) {
	t.Parallel()

	image := Image{MediaType: "image/png", Data: []byte{1, 2, 3}}
	reader := NewMockProjectReader(gomock.NewController(t))
	reader.EXPECT().ReadFile(t.Context(), "image.data", mo.None[uint](), mo.None[uint]()).Return(
		Content{
			Text: mo.None[string](), Image: mo.Some(image), Start: mo.None[uint](),
			End: mo.None[uint](), Total: mo.None[uint](), Next: mo.None[uint](), OversizedSize: mo.None[int64](),
		}, nil,
	)

	result, err := New(reader).Read(t.Context(), "image.data", mo.None[uint](), mo.None[uint]())

	require.NoError(t, err)
	assert.True(t, result.Text.IsNone())
	assert.Equal(t, mo.Some(extensioncontroller.ReadImage{MediaType: image.MediaType, Data: image.Data}), result.Image)
}

// TestServiceReadOversizedLineProvidesBoundedCommand verifies safe shell quoting and a byte bound.
func TestServiceReadOversizedLineProvidesBoundedCommand(t *testing.T) {
	t.Parallel()

	reader := NewMockProjectReader(gomock.NewController(t))
	reader.EXPECT().ReadFile(t.Context(), "dir/a'b.txt", mo.Some(uint(7)), mo.Some(uint(1))).Return(
		Content{
			Text: mo.Some(""), Image: mo.None[Image](), Start: mo.Some(uint(7)),
			End: mo.None[uint](), Total: mo.Some(uint(1)), Next: mo.None[uint](),
			OversizedSize: mo.Some(int64(textbudget.MaximumBytes + 1)),
		}, nil,
	)

	result, err := New(reader).Read(t.Context(), "dir/a'b.txt", mo.Some(uint(7)), mo.Some(uint(1)))

	require.NoError(t, err)
	assert.Equal(
		t,
		"[Line 7 is 51201 bytes and exceeds the 51200 byte limit. "+
			"Use `sed -n '7p' 'dir/a'\\''b.txt' | head -c 51200` to inspect that line.]",
		result.Text.OrEmpty(),
	)
}

// TestServiceReadRejectsMissingOversizedMetadata verifies an oversized line requires its start position.
func TestServiceReadRejectsMissingOversizedMetadata(t *testing.T) {
	t.Parallel()

	reader := NewMockProjectReader(gomock.NewController(t))
	reader.EXPECT().ReadFile(t.Context(), "notes.txt", mo.None[uint](), mo.None[uint]()).Return(Content{
		Text: mo.Some(""), Image: mo.None[Image](), Start: mo.None[uint](), End: mo.None[uint](),
		Total: mo.None[uint](), Next: mo.None[uint](), OversizedSize: mo.Some(int64(textbudget.MaximumBytes + 1)),
	}, nil)

	_, err := New(reader).Read(t.Context(), "notes.txt", mo.None[uint](), mo.None[uint]())

	require.ErrorContains(t, err, "oversized content has no start line")
}

// TestServiceReadRejectsMissingPartialMetadata verifies continuation output requires its line range.
func TestServiceReadRejectsMissingPartialMetadata(t *testing.T) {
	t.Parallel()

	reader := NewMockProjectReader(gomock.NewController(t))
	reader.EXPECT().ReadFile(t.Context(), "notes.txt", mo.None[uint](), mo.None[uint]()).Return(Content{
		Text: mo.Some("text"), Image: mo.None[Image](), Start: mo.None[uint](), End: mo.None[uint](),
		Total: mo.None[uint](), Next: mo.Some(uint(2)), OversizedSize: mo.None[int64](),
	}, nil)

	_, err := New(reader).Read(t.Context(), "notes.txt", mo.None[uint](), mo.None[uint]())

	require.ErrorContains(t, err, "continuation metadata is incomplete")
}

// TestServiceReadError preserves project-reader failures.
func TestServiceReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("file is unavailable")
	reader := NewMockProjectReader(gomock.NewController(t))
	reader.EXPECT().ReadFile(t.Context(), "missing.txt", mo.Some(uint(1)), mo.None[uint]()).Return(Content{
		Text: mo.None[string](), Image: mo.None[Image](), Start: mo.None[uint](),
		End: mo.None[uint](), Total: mo.None[uint](), Next: mo.None[uint](), OversizedSize: mo.None[int64](),
	}, readErr)

	result, err := New(reader).Read(t.Context(), "missing.txt", mo.Some(uint(1)), mo.None[uint]())

	assert.Empty(t, result)
	require.ErrorIs(t, err, readErr)
}
