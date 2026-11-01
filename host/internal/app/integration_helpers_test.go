//go:build integration

package app

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// commandCache keeps one build of each command for the whole test process.
type commandCache struct {
	// mutex serializes builds, so that concurrent tests never compile the same command twice.
	mutex sync.Mutex
	// directory holds the built executables; it is empty until the first build.
	directory string
	// paths maps a build key to its executable.
	paths map[string]string
}

// builtCommands is the command cache of the test process, which TestMain removes at the end.
var builtCommands = commandCache{mutex: sync.Mutex{}, directory: "", paths: make(map[string]string)}

// TestMain removes the executables that the tests of the process built.
func TestMain(m *testing.M) {
	code := m.Run()
	if err := builtCommands.remove(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

// path returns the executable of the package built in the module directory, which the first call compiles.
func (c *commandCache) path(t *testing.T, moduleDir, pkg string) string {
	t.Helper()
	c.mutex.Lock()
	defer c.mutex.Unlock()
	key := moduleDir + "\x00" + pkg
	if path, built := c.paths[key]; built {
		return path
	}
	if c.directory == "" {
		directory, err := os.MkdirTemp("", "glyph-test-commands-")
		require.NoError(t, err)
		c.directory = directory
	}
	path := filepath.Join(c.directory, strconv.Itoa(len(c.paths)))
	build := exec.CommandContext(t.Context(), "go", "build", "-o", path, pkg)
	build.Dir = moduleDir
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	c.paths[key] = path
	return path
}

// remove deletes the built executables.
func (c *commandCache) remove() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.directory == "" {
		return nil
	}
	return os.RemoveAll(c.directory)
}

// linkBuiltCommand makes executableName in directory a hard link to the executable of the package built in the module
// directory; the process compiles each package once, whatever the number of tests and names.
func linkBuiltCommand(t *testing.T, directory, moduleDir, pkg, executableName string) {
	t.Helper()
	require.NoError(t, os.Link(builtCommands.path(t, moduleDir, pkg), filepath.Join(directory, executableName)))
}

// buildToolsExecutable provides the real tools command in a new test-owned temporary directory.
func buildToolsExecutable(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	linkBuiltCommand(t, directory, repoRoot(t), "./plugins/extension/tools/cmd/glyph-tools", "glyph-tools")
	return directory
}

// buildExternalCommand provides a command of the testdata/external-plugins module in an existing directory under the
// given executable name.
func buildExternalCommand(t *testing.T, directory, command, executableName string) {
	t.Helper()
	linkBuiltCommand(
		t, directory, filepath.Join(repoRoot(t), "testdata", "external-plugins"), "./cmd/"+command, executableName,
	)
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
