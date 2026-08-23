package write

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestServiceWrite delegates one complete file write.
func TestServiceWrite(t *testing.T) {
	t.Parallel()

	writer := NewMockProjectWriter(gomock.NewController(t))
	writer.EXPECT().WriteFile(t.Context(), "nested/notes.txt", "content").Return(nil)

	err := New(writer).Write(t.Context(), "nested/notes.txt", "content")

	require.NoError(t, err)
}
