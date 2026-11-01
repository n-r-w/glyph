//go:build integration

package app

import (
	"encoding/json/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// buildToolsExecutable compiles the real tools command into a test-owned temporary directory.
func buildToolsExecutable(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	output := filepath.Join(t.TempDir(), "glyph-tools")
	command := exec.CommandContext(
		t.Context(),
		"go",
		"build",
		"-o",
		output,
		"./plugins/extension/tools/cmd/glyph-tools",
	)
	command.Dir = root
	outputBytes, err := command.CombinedOutput()
	require.NoError(t, err, string(outputBytes))
	return filepath.Dir(output)
}

// buildExternalCommand builds a command of the testdata/external-plugins module into an existing directory under the
// given executable name.
func buildExternalCommand(t *testing.T, directory, command, executableName string) {
	t.Helper()
	build := exec.CommandContext(
		t.Context(), "go", "build", "-o", filepath.Join(directory, executableName), "./cmd/"+command,
	)
	build.Dir = filepath.Join(repoRoot(t), "testdata", "external-plugins")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
}

// enableTraceFile makes the extension processes of the test write their trace to a new file named by the
// environment variable and returns the path of the file.
func enableTraceFile(t *testing.T, environment string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trace.tsv")
	t.Setenv(environment, path)
	return path
}

// readTraceLines returns the lines of a trace file in the order in which the processes wrote them.
func readTraceLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// toolCallSSE requests one call of the named tool with the given arguments.
func toolCallSSE(t *testing.T, callID, toolName, arguments string) string {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	require.NoError(t, err)
	return `data: {"type":"response.completed","response":{"id":"resp-` + callID + `","model":"gpt-active",` +
		`"status":"completed","output":[` +
		`{"id":"fc-` + callID + `","type":"function_call","call_id":"` + callID + `","name":"` + toolName +
		`","arguments":` + string(encoded) + `,"status":"completed"}]}}` + "\n\n" + "data: [DONE]\n\n"
}

// receiveStartupIssues sends one session-info request and returns the startup issues that arrive ahead of every
// message of that request.
func receiveStartupIssues(
	t *testing.T,
	fixture *programmaticFixture,
	operationID string,
) []*programmaticv1.StartupIssue {
	t.Helper()
	request := new(programmaticv1.OpenRequest)
	request.SetOperationId(operationID)
	programmaticRequest(request).SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	require.NoError(t, fixture.stream.Send(request))
	issues := make([]*programmaticv1.StartupIssue, 0)
	requestStarted := false
	for completed := false; !completed; {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetConnectionEvent().HasStartupIssue() {
			require.False(t, requestStarted, "startup issue arrived after a message of the request")
			issues = append(issues, response.GetConnectionEvent().GetStartupIssue())
		}
		if response.GetOperationId() == operationID {
			requestStarted = true
			completed = response.GetEvent().HasCompleted()
		}
	}
	return issues
}
