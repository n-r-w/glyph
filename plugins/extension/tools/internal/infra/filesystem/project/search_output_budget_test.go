//go:build integration

package project

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
)

// requireOutputLimit checks the complete result, including its output-limit notice.
func requireOutputLimit(t *testing.T, text string) {
	t.Helper()
	require.LessOrEqual(t, len(text), textbudget.MaximumBytes)
	require.LessOrEqual(t, strings.Count(text, "\n"), textbudget.MaximumLines)
	require.Contains(t, text, "[Output limit reached.]\n")
}

func TestSearchOutputAccountsForMultipleNotices(t *testing.T) {
	t.Parallel()
	output := newSearchOutput()
	for range textbudget.MaximumBytes / 512 {
		output.add(strings.Repeat("x", 511) + "\n")
	}

	output.notice(strings.Repeat("a", 399) + "\n")
	output.notice(strings.Repeat("b", 399) + "\n")

	require.LessOrEqual(t, len(output.text()), textbudget.MaximumBytes)
	require.LessOrEqual(t, strings.Count(output.text(), "\n"), textbudget.MaximumLines)
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceGrepOutputByteLimit(t *testing.T) {
	t.Chdir(t.TempDir())
	line := strings.Repeat("x", 500) + " match\n"
	require.NoError(t, os.WriteFile("matches.txt", []byte(strings.Repeat(line, 110)), 0o644))

	result, err := New().Grep(t.Context(), searchtool.GrepCommand{
		Pattern: "match", Path: ".", Glob: "", IgnoreCase: false, Literal: false, Context: 0,
		Limit: mo.EmptyableToOption[uint](200),
	})

	require.NoError(t, err)
	requireOutputLimit(t, result.Text)
	require.Contains(t, result.Text, "[Long lines were truncated to 500 characters.]\n")
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceGrepOutputLineLimit(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("matches.txt", []byte(strings.Repeat("match\n", 2001)), 0o644))

	result, err := New().Grep(t.Context(), searchtool.GrepCommand{
		Pattern: "match", Path: ".", Glob: "", IgnoreCase: false, Literal: false, Context: 0,
		Limit: mo.EmptyableToOption[uint](3000),
	})

	require.NoError(t, err)
	requireOutputLimit(t, result.Text)
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceFindOutputByteLimit(t *testing.T) {
	t.Chdir(t.TempDir())
	for index := range 1000 {
		name := fmt.Sprintf("file-%04d-%s.txt", index, strings.Repeat("x", 60))
		require.NoError(t, os.WriteFile(name, nil, 0o644))
	}

	result, err := New().Find(t.Context(), searchtool.FindCommand{
		Pattern: "*.txt", Path: ".", Limit: mo.EmptyableToOption[uint](1000),
	})

	require.NoError(t, err)
	requireOutputLimit(t, result.Text)
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceListOutputByteLimit(t *testing.T) {
	t.Chdir(t.TempDir())
	for index := range 500 {
		name := fmt.Sprintf("file-%04d-%s", index, strings.Repeat("x", 110))
		require.NoError(t, os.WriteFile(name, nil, 0o644))
	}

	result, err := New().List(t.Context(), searchtool.ListCommand{Path: ".", Limit: mo.EmptyableToOption[uint](500)})

	require.NoError(t, err)
	requireOutputLimit(t, result.Text)
}
