//go:build integration

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestProgrammaticClientReceivesStartupIssuesBeforeFirstResult verifies that a tool-name conflict between two
// extensions reaches the client as startup-issue events ahead of every message of its first request.
func TestProgrammaticClientReceivesStartupIssuesBeforeFirstResult(t *testing.T) {
	t.Parallel()

	// Arrange two extensions that register the same tool names: the bundled tools extension under two names.
	directory := buildToolsExecutable(t)
	require.NoError(t, os.Link(filepath.Join(directory, "glyph-tools"), filepath.Join(directory, "glyph-tools-copy")))
	fixture := startProgrammaticFixtureWithExtension(t, testPaths(t, codexSettings("")), directory)
	defer fixture.closeOwner(t)

	// Act by sending the first request and reading the stream until that request completes.
	request := new(programmaticv1.OpenRequest)
	request.SetOperationId("first")
	programmaticRequest(request).SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	require.NoError(t, fixture.stream.Send(request))
	issues := make([]*programmaticv1.StartupIssue, 0)
	requestStarted := false
	for completed := false; !completed; {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetConnectionEvent().HasStartupIssue() {
			require.False(t, requestStarted, "startup issue arrived after a message of the first request")
			issues = append(issues, response.GetConnectionEvent().GetStartupIssue())
		}
		if response.GetOperationId() == "first" {
			requestStarted = true
			completed = response.GetEvent().HasCompleted()
		}
	}

	// Assert that each conflict names both extensions and holds the complete startup error text.
	require.NotEmpty(t, issues)
	texts := make([]string, 0, len(issues))
	for _, issue := range issues {
		assert.Equal(t, []string{"glyph-tools", "glyph-tools-copy"}, issue.GetExtensionIds())
		assert.Empty(t, issue.GetCatalogPath())
		texts = append(texts, issue.GetText())
	}
	assert.Contains(t, texts, `tool name "bash" conflicts`)
}
