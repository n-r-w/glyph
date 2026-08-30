//go:build integration

package project

import (
	"context"
	"os"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
)

// grepCommand keeps table-driven grep cases focused on input behavior.
func grepCommand(pattern, path, glob string, ignoreCase, literal bool, context, limit uint) searchtool.GrepCommand {
	return searchtool.GrepCommand{
		Pattern: pattern, Path: path, Glob: glob, IgnoreCase: ignoreCase, Literal: literal,
		Context: context, Limit: mo.EmptyableToOption[uint](limit),
	}
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceGrepFiltersAndErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("a.go", []byte("Alpha\nliteral.*\n"), 0o644))
	require.NoError(t, os.WriteFile("a.txt", []byte("alpha\n"), 0o644))
	service := New()
	cases := []struct {
		name    string
		cmd     searchtool.GrepCommand
		want    string
		wantErr string
	}{
		{"regex", grepCommand("Al.ha", ".", "", false, false, 0, 100), "a.go:1:Alpha\n", ""},
		{"literal", grepCommand("literal.*", ".", "", false, true, 0, 100), "a.go:2:literal.*\n", ""},
		{"case insensitive", grepCommand("alpha", ".", "", true, false, 0, 100), "a.go:1:Alpha\na.txt:1:alpha\n", ""},
		{"glob", grepCommand("alpha", ".", "*.txt", false, false, 0, 100), "a.txt:1:alpha\n", ""},
		{"malformed regex", grepCommand("[", ".", "", false, false, 0, 100), "", "compile grep pattern"},
		{"malformed glob", grepCommand("alpha", ".", "[", false, false, 0, 100), "", "invalid grep glob"},
		{"missing path", grepCommand("alpha", "missing", "", false, false, 0, 100), "", "grep project files"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := service.Grep(t.Context(), testCase.cmd)
			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.want, result.Text)
		})
	}
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceGrepLimitsAndCancellation(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("a.txt", []byte("match\nmatch\n"), 0o644))
	service := New()
	result, err := service.Grep(t.Context(), grepCommand("match", ".", "", false, false, 0, 0))
	require.NoError(t, err)
	require.NotContains(t, result.Text, "Match limit reached")
	result, err = service.Grep(t.Context(), grepCommand("match", ".", "", false, false, 0, 1))
	require.NoError(t, err)
	require.Equal(t, "a.txt:1:match\n[Match limit reached.]\n", result.Text)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.Grep(ctx, grepCommand("match", ".", "", false, false, 0, 1))
	require.ErrorIs(t, err, context.Canceled)
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceRejectsMalformedGlobsInEmptyRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	service := New()

	_, grepErr := service.Grep(t.Context(), grepCommand("match", ".", "[", false, false, 0, 100))
	_, findErr := service.Find(t.Context(), searchtool.FindCommand{Pattern: "[", Path: ".", Limit: mo.EmptyableToOption[uint](1000)})

	require.ErrorContains(t, grepErr, "invalid grep glob")
	require.ErrorContains(t, findErr, "invalid find glob")
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceFindFiltersLimitsAndErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("root/nested", 0o755))
	require.NoError(t, os.WriteFile("root/a.go", nil, 0o644))
	require.NoError(t, os.WriteFile("root/nested/b.go", nil, 0o644))
	service := New()
	result, err := service.Find(t.Context(), searchtool.FindCommand{Pattern: "root/*.go", Path: "root", Limit: mo.EmptyableToOption[uint](1000)})
	require.NoError(t, err)
	require.Contains(t, result.Text, "root/a.go\n")
	require.NotContains(t, result.Text, "nested/b.go")
	result, err = service.Find(t.Context(), searchtool.FindCommand{Pattern: "**/*.go", Path: "root", Limit: mo.EmptyableToOption[uint](1)})
	require.NoError(t, err)
	require.Contains(t, result.Text, "[Result limit reached.]\n")
	_, err = service.Find(t.Context(), searchtool.FindCommand{Pattern: "[", Path: "root", Limit: mo.EmptyableToOption[uint](1)})
	require.ErrorContains(t, err, "invalid find glob")
	_, err = service.Find(t.Context(), searchtool.FindCommand{Pattern: "*", Path: "missing", Limit: mo.EmptyableToOption[uint](1)})
	require.ErrorContains(t, err, "find project files")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.Find(ctx, searchtool.FindCommand{Pattern: "*", Path: "root", Limit: mo.EmptyableToOption[uint](1)})
	require.ErrorIs(t, err, context.Canceled)
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceListLimitsErrorsAndCancellation(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.Mkdir("dir", 0o755))
	require.NoError(t, os.WriteFile("file", nil, 0o644))
	service := New()
	result, err := service.List(t.Context(), searchtool.ListCommand{Path: ".", Limit: mo.EmptyableToOption[uint](1)})
	require.NoError(t, err)
	require.Contains(t, result.Text, "[Entry limit reached.]\n")
	result, err = service.List(t.Context(), searchtool.ListCommand{Path: "dir", Limit: mo.EmptyableToOption[uint](500)})
	require.NoError(t, err)
	require.Empty(t, result.Text)
	_, err = service.List(t.Context(), searchtool.ListCommand{Path: "missing", Limit: mo.EmptyableToOption[uint](1)})
	require.ErrorContains(t, err, "list project directory")
	_, err = service.List(t.Context(), searchtool.ListCommand{Path: "file", Limit: mo.EmptyableToOption[uint](1)})
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.List(ctx, searchtool.ListCommand{Path: ".", Limit: mo.EmptyableToOption[uint](1)})
	require.ErrorIs(t, err, context.Canceled)
}
