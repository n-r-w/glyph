package search

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

func TestServiceGrepMapsControllerArguments(t *testing.T) {
	t.Parallel()
	control := gomock.NewController(t)
	projectFiles := NewMockProjectFiles(control)
	projectFiles.EXPECT().Grep(t.Context(), GrepCommand{Pattern: "needle", Path: "src", Glob: "**/*.go", IgnoreCase: true, Literal: true, Context: 2, Limit: 7}).Return(GrepResult{Text: "result"}, nil)

	result, err := New(projectFiles).Grep(t.Context(), extensioncontroller.GrepArguments{Pattern: "needle", Path: "src", Glob: "**/*.go", IgnoreCase: true, Literal: true, Context: 2, Limit: 7})

	require.NoError(t, err)
	require.Equal(t, "result", result)
}
