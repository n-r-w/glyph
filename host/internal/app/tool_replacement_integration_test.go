//go:build integration

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// toolOverrideTraceEnvironment names the trace file written by the tool-replacement reference extension.
	toolOverrideTraceEnvironment = "GLYPH_TOOLOVERRIDE_TRACE"
	// toolOverrideRunID identifies the user request of the tool-replacement scenarios.
	toolOverrideRunID = "tool-override-run"
	// toolOverrideFirst and toolOverrideSecond are the executables of the replacing extensions.
	toolOverrideFirst  = "tooloverride-1"
	toolOverrideSecond = "tooloverride-2"
	// toolOverrideDescription is the description that the replacing extension gives to the replaced tool.
	toolOverrideDescription = "Read the value of a note. This replacement of the read tool takes the note as value."
	// replacedToolName is the bundled tool that the replacing extensions replace.
	replacedToolName = "read"
	// toolOverrideNote is the value of the call of the replacing read tool.
	toolOverrideNote = "note"
	// projectNoteName and projectNoteText are the file that the test creates in its working directory and its content.
	projectNoteName = "project-note.txt"
	projectNoteText = "content that only a read of the project file returns"
)

// buildToolOverrideFixture builds the bundled tools extension and the public-only tool-replacement reference once
// for each executable name into one extension directory.
func buildToolOverrideFixture(t *testing.T, names ...string) string {
	t.Helper()
	directory := buildToolsExecutable(t)
	for _, name := range names {
		buildExternalCommand(t, directory, "tooloverride", name)
	}
	return directory
}

// enterProjectWithNote makes a new directory with the note file the working directory of the test, which the
// bundled tools use as the project.
func enterProjectWithNote(t *testing.T) {
	t.Helper()
	project := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(project, projectNoteName), []byte(projectNoteText), 0o600))
	t.Chdir(project)
}

// toolOverrideTraceLine builds the expected trace line of an event of one reference executable for one tool.
func toolOverrideTraceLine(executable, event, toolName string) string {
	return executable + "\t" + event + "\t" + toolName
}

// toolResultTexts returns the text of each tool result of one observed request, in order.
func toolResultTexts(observation finalizedObservation) []string {
	results := observation.toolResults[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT]
	texts := make([]string, len(results))
	for index, result := range results {
		texts[index] = toolResultText(result)
	}
	return texts
}

// TestPublicToolReplacementReplacesBundledTool verifies that the model sees the replacing tool once with its
// description, that the replacing extension executes its calls under its schema, and that the other bundled tool and
// the handler of the replacing extension keep working.
//
//nolint:paralleltest // The scenario sets the trace environment of the extension processes.
func TestPublicToolReplacementReplacesBundledTool(t *testing.T) {
	// Arrange the bundled tools extension, one replacing extension, and a provider that calls read and then ls.
	trace := enableTraceFile(t, toolOverrideTraceEnvironment)
	provider := startQueueProvider(
		t, 0,
		toolCallSSE(t, "call-1", replacedToolName, `{"value":"`+toolOverrideNote+`"}`),
		toolCallSSE(t, "call-2", "ls", `{}`),
		finalResponseSSE,
	)
	extensions := buildToolOverrideFixture(t, toolOverrideFirst)
	enterProjectWithNote(t)
	fixture := startQueueFixture(t, provider, extensions)
	defer fixture.closeOwner(t)

	// Act through one user request.
	require.NoError(t, fixture.stream.Send(userRequest(toolOverrideRunID, "run the tools")))
	observation := observeFinalizedRequest(t, fixture, toolOverrideRunID)

	// Assert the model request, the executor of each call, and the handler of the replacing extension.
	require.Empty(t, observation.code, observation.message)
	bodies := provider.snapshot()
	require.Len(t, bodies, 3)
	assert.Equal(t, 1, strings.Count(bodies[0], `"name":"`+replacedToolName+`"`))
	assert.Contains(t, bodies[0], toolOverrideDescription)
	texts := toolResultTexts(observation)
	require.Len(t, texts, 2)
	assert.Contains(t, texts[0], toolOverrideNote)
	assert.Contains(t, texts[1], projectNoteName)
	assert.Equal(t, []string{
		toolOverrideTraceLine(toolOverrideFirst, "handler", replacedToolName),
		toolOverrideTraceLine(toolOverrideFirst, "tool", replacedToolName),
		toolOverrideTraceLine(toolOverrideFirst, "handler", "ls"),
	}, readTraceLines(t, trace))
}

// TestPublicToolReplacementConflictKeepsOriginalTool verifies that two replacements of the same tool reach the client
// as a startup issue that names both extensions, that the model sees the original tool whose calls the bundled
// extension executes, and that the other tools and the handlers of all three extensions keep working.
//
//nolint:paralleltest // The scenario sets the trace environment of the extension processes.
func TestPublicToolReplacementConflictKeepsOriginalTool(t *testing.T) {
	// Arrange the bundled tools extension, two replacing extensions, and a provider that calls read and then ls.
	trace := enableTraceFile(t, toolOverrideTraceEnvironment)
	provider := startQueueProvider(
		t, 0,
		toolCallSSE(t, "call-1", replacedToolName, `{"path":"`+projectNoteName+`"}`),
		toolCallSSE(t, "call-2", "ls", `{}`),
		finalResponseSSE,
	)
	extensions := buildToolOverrideFixture(t, toolOverrideFirst, toolOverrideSecond)
	enterProjectWithNote(t)
	fixture := startQueueFixture(t, provider, extensions)
	defer fixture.closeOwner(t)

	// Act by reading the startup issues and sending one user request.
	issues := receiveStartupIssues(t, fixture, "startup-issues")
	require.NoError(t, fixture.stream.Send(userRequest(toolOverrideRunID, "run the tools")))
	observation := observeFinalizedRequest(t, fixture, toolOverrideRunID)

	// Assert the reported conflict, the original tool in the model request, the executors, and the handlers.
	require.Len(t, issues, 1)
	assert.Equal(t, []string{toolOverrideFirst, toolOverrideSecond}, issues[0].GetExtensionIds())
	assert.Contains(t, issues[0].GetText(), `"`+replacedToolName+`"`)
	require.Empty(t, observation.code, observation.message)
	bodies := provider.snapshot()
	require.Len(t, bodies, 3)
	assert.Equal(t, 1, strings.Count(bodies[0], `"name":"`+replacedToolName+`"`))
	assert.NotContains(t, bodies[0], toolOverrideDescription)
	texts := toolResultTexts(observation)
	require.Len(t, texts, 2)
	assert.Contains(t, texts[0], projectNoteText)
	assert.Contains(t, texts[1], projectNoteName)
	assert.Equal(t, []string{
		toolOverrideTraceLine(toolOverrideFirst, "handler", replacedToolName),
		toolOverrideTraceLine(toolOverrideSecond, "handler", replacedToolName),
		toolOverrideTraceLine(toolOverrideFirst, "handler", "ls"),
		toolOverrideTraceLine(toolOverrideSecond, "handler", "ls"),
	}, readTraceLines(t, trace))
}
