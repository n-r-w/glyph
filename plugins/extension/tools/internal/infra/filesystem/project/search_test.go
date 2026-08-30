//go:build integration

package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
)

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceGrepReturnsRelativeLinesAndContext(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("nested", 0o755))
	require.NoError(t, os.WriteFile("nested/notes.txt", []byte("one\ntwo match\nthree\n"), 0o644))

	result, err := New().Grep(t.Context(), searchtool.GrepCommand{Pattern: "match", Path: ".", Glob: "", IgnoreCase: false, Literal: false, Context: 1, Limit: mo.EmptyableToOption[uint](100)})

	require.NoError(t, err)
	require.Equal(t, "nested/notes.txt:1:one\nnested/notes.txt:2:two match\nnested/notes.txt:3:three\n", result.Text)
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceGrepSkipsSymlinksAndReportsObservedMatchLimit(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("a.txt", []byte("match\n"), 0o644))
	require.NoError(t, os.WriteFile("b.txt", []byte("match\n"), 0o644))
	require.NoError(t, os.Symlink("a.txt", "link.txt"))

	result, err := New().Grep(t.Context(), searchtool.GrepCommand{Pattern: "match", Path: ".", Glob: "", IgnoreCase: false, Literal: false, Context: 0, Limit: mo.EmptyableToOption[uint](1)})

	require.NoError(t, err)
	require.Equal(t, "a.txt:1:match\n[Match limit reached.]\n", result.Text)
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceFindReturnsLinkWithoutEnteringAndUsesRecursiveGlob(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("one/two", 0o755))
	require.NoError(t, os.WriteFile("one/two/file.go", []byte("package two"), 0o644))
	require.NoError(t, os.Symlink("one", "linked"))
	require.NoError(t, os.Symlink("one/two/file.go", "linked.go"))

	result, err := New().Find(t.Context(), searchtool.FindCommand{Pattern: "**/*.go", Path: ".", Limit: mo.EmptyableToOption[uint](1000)})

	require.NoError(t, err)
	require.Equal(t, "linked.go\none/two/file.go\n", result.Text)
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceEscapesLineBreaksInDisplayedNames(t *testing.T) {
	t.Chdir(t.TempDir())
	name := "line\nbreak\r.txt"
	require.NoError(t, os.WriteFile(name, []byte("match\n"), 0o644))
	service := New()

	grepResult, grepErr := service.Grep(t.Context(), searchtool.GrepCommand{
		Pattern: "match", Path: ".", Glob: "", IgnoreCase: false, Literal: false, Context: 0, Limit: mo.EmptyableToOption[uint](100),
	})
	findResult, findErr := service.Find(t.Context(), searchtool.FindCommand{Pattern: "*.txt", Path: ".", Limit: mo.EmptyableToOption[uint](1000)})
	listResult, listErr := service.List(t.Context(), searchtool.ListCommand{Path: ".", Limit: mo.EmptyableToOption[uint](500)})

	require.NoError(t, grepErr)
	require.Equal(t, "line\\nbreak\\r.txt:1:match\n", grepResult.Text)
	require.NoError(t, findErr)
	require.Equal(t, "line\\nbreak\\r.txt\n", findResult.Text)
	require.NoError(t, listErr)
	require.Equal(t, "line\\nbreak\\r.txt\n", listResult.Text)
	require.Equal(t, 1, strings.Count(grepResult.Text, "\n"))
	require.Equal(t, 1, strings.Count(findResult.Text, "\n"))
	require.Equal(t, 1, strings.Count(listResult.Text, "\n"))
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceListReturnsBrokenSymbolicLink(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.Symlink("missing", "broken"))

	result, err := New().List(t.Context(), searchtool.ListCommand{Path: ".", Limit: mo.EmptyableToOption[uint](500)})

	require.NoError(t, err)
	require.Equal(t, "broken\n", result.Text)
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceListIncludesHiddenAndMarksDirectoryLinks(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir("dir", 0o755))
	require.NoError(t, os.WriteFile(".hidden", []byte{}, 0o644))
	require.NoError(t, os.Symlink("dir", "linked"))

	result, err := New().List(t.Context(), searchtool.ListCommand{Path: ".", Limit: mo.EmptyableToOption[uint](500)})

	require.NoError(t, err)
	require.Equal(t, ".hidden\ndir/\nlinked/\n", result.Text)
	require.NoFileExists(t, filepath.Join("linked", "anything"))
}
