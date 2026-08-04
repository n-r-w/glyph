package read

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestServiceRead verifies that the use case returns the complete content supplied by the project reader.
func TestServiceRead(t *testing.T) {
	t.Parallel()

	// Arrange: require the project reader to receive the caller's context and requested path.
	projectReader := NewMockProjectReader(gomock.NewController(t))
	projectReader.EXPECT().ReadFile(t.Context(), "notes.txt").Return("first\nsecond\n", nil)
	service := New(projectReader)

	// Act: read the file through the use-case boundary.
	content, err := service.Read(t.Context(), "notes.txt")

	// Assert: preserve the complete content without transformation.
	require.NoError(t, err)
	assert.Equal(t, "first\nsecond\n", content)
}

// TestServiceReadError verifies that a project read failure remains an explicit tool-operation error.
func TestServiceReadError(t *testing.T) {
	t.Parallel()

	// Arrange: make the project reader reject the requested file.
	readErr := errors.New("file is unavailable")
	projectReader := NewMockProjectReader(gomock.NewController(t))
	projectReader.EXPECT().ReadFile(t.Context(), "missing.txt").Return("", readErr)
	service := New(projectReader)

	// Act: read the unavailable file through the use-case boundary.
	content, err := service.Read(t.Context(), "missing.txt")

	// Assert: return no content and retain the underlying failure for error inspection.
	assert.Empty(t, content)
	require.ErrorIs(t, err, readErr)
}
