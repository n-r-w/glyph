package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
)

// writeNumberedFiles creates stable names so limit tests do not depend on directory order.
func writeNumberedFiles(t *testing.T, directory string, count int) {
	t.Helper()
	require.NoError(t, os.MkdirAll(directory, 0o755))
	for index := range count {
		name := filepath.Join(directory, fmt.Sprintf("file-%04d.txt", index))
		require.NoError(t, os.WriteFile(name, nil, 0o644))
	}
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceGrepReportsLimitsOnlyAfterAnAdditionalMatch(t *testing.T) {
	t.Chdir(t.TempDir())
	service := New()
	assertLimit := func(lines, limit int, wantNotice bool) {
		t.Helper()
		require.NoError(t, os.WriteFile("matches.txt", []byte(strings.Repeat("match\n", lines)), 0o644))
		result, err := service.Grep(t.Context(), searchtool.GrepCommand{
			Pattern: "match", Path: ".", Glob: "", IgnoreCase: false, Literal: false,
			Context: 0, Limit: mo.EmptyableToOption[uint](uint(limit)),
		})
		require.NoError(t, err)
		hasNotice := strings.Contains(result.Text, "[Match limit reached.]\n")
		require.Equal(t, wantNotice, hasNotice)
		resultLines := strings.Count(result.Text, "\n")
		if hasNotice {
			resultLines--
		}
		effectiveLimit := limit
		if effectiveLimit == 0 {
			effectiveLimit = 100
		}
		require.Equal(t, min(lines, effectiveLimit), resultLines)
	}

	assertLimit(100, 0, false)
	assertLimit(101, 0, true)
	assertLimit(2, 2, false)
	assertLimit(3, 2, true)
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceFindReportsLimitsOnlyAfterAnAdditionalResult(t *testing.T) {
	t.Chdir(t.TempDir())
	writeNumberedFiles(t, "default-exact", 1000)
	writeNumberedFiles(t, "default-over", 1001)
	writeNumberedFiles(t, "caller-exact", 2)
	writeNumberedFiles(t, "caller-over", 3)
	service := New()
	cases := []struct {
		path       string
		limit      uint
		wantNotice bool
		wantCount  int
	}{
		{path: "default-exact", limit: 0, wantNotice: false, wantCount: 1000},
		{path: "default-over", limit: 0, wantNotice: true, wantCount: 1000},
		{path: "caller-exact", limit: 2, wantNotice: false, wantCount: 2},
		{path: "caller-over", limit: 2, wantNotice: true, wantCount: 2},
	}
	for _, testCase := range cases {
		result, err := service.Find(t.Context(), searchtool.FindCommand{Pattern: "**/*.txt", Path: testCase.path, Limit: mo.EmptyableToOption[uint](testCase.limit)})
		require.NoError(t, err)
		hasNotice := strings.Contains(result.Text, "[Result limit reached.]\n")
		require.Equal(t, testCase.wantNotice, hasNotice)
		resultLines := strings.Count(result.Text, "\n")
		if hasNotice {
			resultLines--
		}
		require.Equal(t, testCase.wantCount, resultLines)
	}
}

//nolint:paralleltest // t.Chdir changes the process working directory used as the project root.
func TestServiceListReportsLimitsOnlyAfterAnAdditionalEntry(t *testing.T) {
	t.Chdir(t.TempDir())
	writeNumberedFiles(t, "default-exact", 500)
	writeNumberedFiles(t, "default-over", 501)
	writeNumberedFiles(t, "caller-exact", 2)
	writeNumberedFiles(t, "caller-over", 3)
	service := New()
	cases := []struct {
		path       string
		limit      uint
		wantNotice bool
		wantCount  int
	}{
		{path: "default-exact", limit: 0, wantNotice: false, wantCount: 500},
		{path: "default-over", limit: 0, wantNotice: true, wantCount: 500},
		{path: "caller-exact", limit: 2, wantNotice: false, wantCount: 2},
		{path: "caller-over", limit: 2, wantNotice: true, wantCount: 2},
	}
	for _, testCase := range cases {
		result, err := service.List(t.Context(), searchtool.ListCommand{Path: testCase.path, Limit: mo.EmptyableToOption[uint](testCase.limit)})
		require.NoError(t, err)
		hasNotice := strings.Contains(result.Text, "[Entry limit reached.]\n")
		require.Equal(t, testCase.wantNotice, hasNotice)
		resultLines := strings.Count(result.Text, "\n")
		if hasNotice {
			resultLines--
		}
		require.Equal(t, testCase.wantCount, resultLines)
	}
}
