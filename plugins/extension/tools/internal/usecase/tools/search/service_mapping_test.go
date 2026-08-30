package search

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

func TestServiceFindAndListMapControllerArguments(t *testing.T) {
	t.Parallel()
	control := gomock.NewController(t)
	files := NewMockProjectFiles(control)
	files.EXPECT().
		Find(t.Context(), FindCommand{Pattern: "**/*.go", Path: "src", Limit: mo.Some(uint(3))}).
		Return(FindResult{Text: "src/a.go\n"}, nil)
	files.EXPECT().
		List(t.Context(), ListCommand{Path: "src", Limit: mo.Some(uint(4))}).
		Return(ListResult{Text: "a.go\n"}, nil)
	service := New(files)
	find, err := service.Find(
		t.Context(),
		extensioncontroller.FindArguments{Pattern: "**/*.go", Path: "src", Limit: mo.Some(uint(3))},
	)
	require.NoError(t, err)
	require.Equal(t, "src/a.go\n", find)
	list, err := service.List(t.Context(), extensioncontroller.ListArguments{Path: "src", Limit: mo.Some(uint(4))})
	require.NoError(t, err)
	require.Equal(t, "a.go\n", list)
}
