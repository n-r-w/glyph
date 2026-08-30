package edit

import (
	"context"
	"testing"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestServiceEdit applies one replacement through one atomic update.
func TestServiceEdit(t *testing.T) {
	t.Parallel()

	editor := NewMockProjectEditor(gomock.NewController(t))
	editor.EXPECT().UpdateFile(t.Context(), "notes.txt", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, update func([]byte) ([]byte, error)) error {
			updated, err := update([]byte("before old after"))
			require.NoError(t, err)
			assert.Equal(t, "before new after", string(updated))
			return nil
		},
	)

	err := New(
		editor,
	).Edit(t.Context(), "notes.txt", []extensioncontroller.Replacement{{OldText: "old", NewText: "new"}})

	require.NoError(t, err)
}

// TestServiceEditManyRejectsOverlappingSources rejects overlapping original ranges.
func TestServiceEditManyRejectsOverlappingSources(t *testing.T) {
	t.Parallel()

	editor := NewMockProjectEditor(gomock.NewController(t))
	editor.EXPECT().UpdateFile(t.Context(), "notes.txt", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, update func([]byte) ([]byte, error)) error {
			_, err := update([]byte("abcdef"))
			return err
		},
	)

	err := New(editor).Edit(t.Context(), "notes.txt", []extensioncontroller.Replacement{
		{OldText: "abc", NewText: "one"},
		{OldText: "bcd", NewText: "two"},
	})

	require.Error(t, err)
}

// TestServiceEditRejectsNonUniqueSource rejects missing and non-unique source fragments.
func TestServiceEditRejectsNonUniqueSource(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{"missing": "other", "duplicate": "old old"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			editor := NewMockProjectEditor(gomock.NewController(t))
			editor.EXPECT().UpdateFile(t.Context(), "notes.txt", gomock.Any()).DoAndReturn(
				func(_ context.Context, _ string, update func([]byte) ([]byte, error)) error {
					_, err := update([]byte(content))
					return err
				},
			)

			err := New(editor).Edit(
				t.Context(),
				"notes.txt",
				[]extensioncontroller.Replacement{{OldText: "old", NewText: "new"}},
			)

			require.Error(t, err)
			assert.ErrorContains(t, err, "occur exactly once")
		})
	}
}
