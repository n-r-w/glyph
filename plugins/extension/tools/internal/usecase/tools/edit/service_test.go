package edit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestServiceEdit replaces a uniquely occurring source fragment and writes once.
func TestServiceEdit(t *testing.T) {
	t.Parallel()

	editor := NewMockProjectEditor(gomock.NewController(t))
	editor.EXPECT().ReadFile(t.Context(), "notes.txt").Return("before old after", nil)
	editor.EXPECT().WriteFile(t.Context(), "notes.txt", "before new after").Return(nil)

	err := New(editor).Edit(t.Context(), "notes.txt", "old", "new")

	require.NoError(t, err)
}

// TestServiceEditRejectsNonUniqueSource preserves the file when the source count is not one.
func TestServiceEditRejectsNonUniqueSource(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{"missing": "other", "duplicate": "old old"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			editor := NewMockProjectEditor(gomock.NewController(t))
			editor.EXPECT().ReadFile(t.Context(), "notes.txt").Return(content, nil)

			err := New(editor).Edit(t.Context(), "notes.txt", "old", "new")

			require.Error(t, err)
			assert.ErrorContains(t, err, "occur exactly once")
		})
	}
}
