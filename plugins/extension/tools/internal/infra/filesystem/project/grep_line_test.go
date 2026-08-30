//go:build integration

package project

import (
	"os"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
)

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceGrepTruncatesMatchingLineLargerThanOneMiB(t *testing.T) {
	t.Chdir(t.TempDir())
	line := strings.Repeat("x", 1024*1024+1) + "match"
	require.NoError(t, os.WriteFile("large.txt", []byte(line+"\n"), 0o644))

	result, err := New().Grep(t.Context(), searchtool.GrepCommand{Pattern: "match", Path: ".", Glob: "", IgnoreCase: false, Literal: false, Context: 0, Limit: mo.EmptyableToOption[uint](100)})

	require.NoError(t, err)
	require.Contains(t, result.Text, "large.txt:1:"+strings.Repeat("x", 500)+"\n")
	require.Contains(t, result.Text, "[Long lines were truncated to 500 characters.]\n")
}
