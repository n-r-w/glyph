//go:build integration

package sessions_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/infra/persistence/sessionfilesystem"
	persistedsessions "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
)

// TestReplayVersion2Tree verifies restart restores complete tree state and optional summary accounting.
func TestReplayVersion2Tree(t *testing.T) {
	t.Parallel()

	// Arrange a version 2 file with two branches, navigation, a label, session information, and a summary.
	root := t.TempDir()
	project := t.TempDir()
	repository := persistedsessions.New(root, project, sessionfilesystem.New())
	require.NoError(t, repository.Initialize(t.Context()))
	createdAt := time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC)
	content := fmt.Sprintf(`{"type":"session","version":2,"id":"stored","createdAt":"%s","cwd":%q}
{"type":"entry","entry":{"type":"user","id":"root","parentId":null,"createdAt":"%s","message":{"content":[{"kind":1,"text":"root"}]}}}
{"type":"entry","entry":{"type":"model","id":"old","parentId":"root","createdAt":"%s","response":{"content":[],"outcome":1,"diagnostics":[]}}}
{"type":"navigation","navigation":{"destinationId":"root"}}
{"type":"entry","entry":{"type":"model","id":"new","parentId":"root","createdAt":"%s","response":{"content":[],"outcome":1,"diagnostics":[]}}}
{"type":"label","label":{"targetId":"old","label":"kept branch"}}
{"type":"session_info","sessionInfo":{"name":"branched session","createdAt":"%s"}}
{"type":"navigation","navigation":{"destinationId":"root","branchSummary":{"type":"branch_summary","id":"summary","parentId":"root","createdAt":"%s","summary":"completed work","firstEntryId":"old","lastEntryId":"old","provider":"provider","model":"model","reasoningChoice":"low","usage":{"inputTokens":1,"outputTokens":2,"cacheReadTokens":3,"cacheWriteTokens":4,"reasoningTokens":1,"totalTokens":10},"estimatedCost":{"input":1,"output":2,"cacheRead":3,"cacheWrite":4,"total":10}}}}
`, createdAt.Format(time.RFC3339Nano), project, createdAt.Format(time.RFC3339Nano), createdAt.Add(time.Second).Format(time.RFC3339Nano), createdAt.Add(2*time.Second).Format(time.RFC3339Nano), createdAt.Add(3*time.Second).Format(time.RFC3339Nano), createdAt.Add(4*time.Second).Format(time.RFC3339Nano))
	path := sessionPath(
		root,
		project,
		session.Header{Version: 2, ID: "stored", CreatedAt: createdAt, WorkingDirectory: project},
	)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	// Act by replaying the complete JSONL file.
	loaded, err := repository.Load(t.Context(), "stored")

	// Assert all aggregate state and explicit option states survive exactly.
	require.NoError(t, err)
	require.Equal(t, mo.Some("summary"), loaded.Tree.ActiveLeafID())
	require.Equal(t, map[string]string{"old": "kept branch"}, loaded.Tree.Labels())
	require.Equal(
		t,
		[]string{"root", "old", "new", "summary"},
		lo.Map(loaded.Tree.Entries(), func(entry session.Entry, _ int) string {
			return entry.ID
		}),
	)
	require.Equal(t, mo.Some(session.Information{Name: "branched session"}), loaded.Information)
	summary := loaded.Tree.Entries()[3].BranchSummary.MustGet()
	require.Equal(t, model.ReasoningChoiceLow, summary.ReasoningChoice)
	require.Equal(
		t,
		mo.Some(
			session.TokenUsage{
				InputTokens:      1,
				OutputTokens:     2,
				CacheReadTokens:  3,
				CacheWriteTokens: 4,
				ReasoningTokens:  1,
				TotalTokens:      10,
			},
		),
		summary.Usage,
	)
	require.Equal(
		t,
		mo.Some(session.EstimatedCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10}),
		summary.EstimatedCost,
	)
}

// TestReplayRejectsInvalidVersion2Tree verifies corruption never returns a partial aggregate.
func TestReplayRejectsInvalidVersion2Tree(t *testing.T) {
	t.Parallel()

	// Arrange closed corruption cases after a valid version 2 header.
	cases := map[string]string{
		"version one":            `{"type":"session","version":1,"id":"stored","createdAt":"2026-08-29T01:00:00Z","cwd":%q}` + "\n",
		"forward parent":         `{"type":"session","version":2,"id":"stored","createdAt":"2026-08-29T01:00:00Z","cwd":%q}` + "\n" + `{"type":"entry","entry":{"type":"user","id":"child","parentId":"later","createdAt":"2026-08-29T01:00:00Z","message":{"content":[]}}}` + "\n",
		"multiple payloads":      `{"type":"session","version":2,"id":"stored","createdAt":"2026-08-29T01:00:00Z","cwd":%q}` + "\n" + `{"type":"entry","entry":{"type":"user","id":"root","parentId":null,"createdAt":"2026-08-29T01:00:00Z","message":{"content":[]}},"label":{"targetId":"root","label":"bad"}}` + "\n",
		"unknown navigation":     validRootRecord() + `{"type":"navigation","navigation":{"destinationId":"missing"}}` + "\n",
		"unknown label":          validRootRecord() + `{"type":"label","label":{"targetId":"missing","label":"bad"}}` + "\n",
		"invalid branch summary": validRootRecord() + `{"type":"navigation","navigation":{"destinationId":"root","branchSummary":{"type":"branch_summary","id":"summary","parentId":"root","createdAt":"2026-08-29T01:00:01Z","summary":"","firstEntryId":"root","lastEntryId":"root","provider":"provider","model":"model","reasoningChoice":"low"}}}` + "\n",
	}
	for name, template := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			project := t.TempDir()
			repository := persistedsessions.New(root, project, sessionfilesystem.New())
			require.NoError(t, repository.Initialize(t.Context()))
			content := fmt.Sprintf(template, project)
			path := filepath.Join(projectDirectory(root, project), "invalid.jsonl")
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

			// Act by replaying the corrupt file.
			loaded, err := repository.Load(t.Context(), "stored")

			// Assert no partial tree escapes the repository.
			require.ErrorIs(t, err, session.ErrUnavailable)
			require.Empty(t, loaded.Tree.Entries())
		})
	}
}

func projectDirectory(root, project string) string {
	return filepath.Join(root, persistedsessions.ProjectDirectoryName(project))
}

func sessionPath(root, project string, header session.Header) string {
	return filepath.Join(projectDirectory(root, project), persistedsessions.SessionFilename(header))
}

func validRootRecord() string {
	return `{"type":"session","version":2,"id":"stored","createdAt":"2026-08-29T01:00:00Z","cwd":%q}` + "\n" +
		`{"type":"entry","entry":{"type":"user","id":"root","parentId":null,"createdAt":"2026-08-29T01:00:00Z","message":{"content":[]}}}` + "\n"
}
