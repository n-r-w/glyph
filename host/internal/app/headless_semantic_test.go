package app

import (
	"bytes"

	"encoding/json"

	"fmt"

	"net/http"

	"os"

	"path/filepath"

	"strings"
	"sync/atomic"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestHostSemanticClientMatchesHeadlessOutcome verifies shared public semantics.
func TestHostSemanticClientMatchesHeadlessOutcome(t *testing.T) {
	// Arrange shared paths, credentials, provider transport, and extension executable.
	paths := testPaths(t, codexSettings(""))
	accessToken := semanticAccessToken(t, "account")
	require.NoError(t, os.WriteFile(paths.CredentialsFile, fmt.Appendf(nil, `{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`, accessToken), 0o600))
	requestCount := &atomic.Int32{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = deterministicCodexTransport{
		requestCount: requestCount,
		lastBody:     nil,
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	extensionDirectory := buildToolsExecutable(t)
	var headlessStdout, headlessStderr bytes.Buffer
	// Act by running equivalent headless and Host UI tool turns.
	headlessErr := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeHeadless,
		Headless: headless.Command{
			UserText:           "read input.txt",
			ExtensionDirectory: extensionDirectory,
		},
		ExtensionDirectory: "",
		UIDirectory:        "",
		UIID:               "",
		SocketPath:         "",
	}, &headlessStdout, &headlessStderr)
	require.NoError(t, headlessErr)
	assert.Equal(t, int32(2), requestCount.Load())
	headlessObservation := parseHeadlessOutcome(headlessStdout.String(), headlessStderr.String(), headlessErr)
	expected := sharedOutcome{
		FinalText:        "Request complete.",
		ToolName:         "bash",
		ToolStartName:    "bash",
		ToolEndName:      "bash",
		ToolStatus:       "ok",
		ToolStarted:      true,
		ToolEnded:        true,
		CommandSucceeded: true,
	}
	assert.Equal(t, expected, headlessObservation)

	requestCount.Store(0)
	uiDirectory := t.TempDir()
	tracePath := filepath.Join(t.TempDir(), "semantic-ui.jsonl")
	t.Setenv(appUIBehaviorEnvironment, "semantic")
	t.Setenv(appUITraceEnvironment, tracePath)
	writeUIExecutable(t, uiDirectory, "Semantic_UI")
	uiErr := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: extensionDirectory,
		},
		ExtensionDirectory: extensionDirectory,
		UIDirectory:        uiDirectory,
		UIID:               "semantic-ui",
		SocketPath:         "",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	require.NoError(t, uiErr)
	assert.Equal(t, int32(2), requestCount.Load())
	ui := parseUIObservation(t, tracePath)
	// Assert both clients expose the same lifecycle and terminal outcome.
	assert.Equal(t, loadSemanticLifecycle(t), ui.Records)
	assert.Equal(t, expected, ui.Shared)
	assert.Equal(t, headlessObservation, ui.Shared)
}

// sharedOutcome contains only fields observable from both Host client boundaries.
type sharedOutcome struct {
	FinalText, ToolName, ToolStatus string
	ToolStartName, ToolEndName      string
	ToolStarted, ToolEnded          bool
	CommandSucceeded                bool
}

// semanticLifecycleRecord is the stable subset shared with the standard consumer fixture.
type semanticLifecycleRecord struct {
	Type               string          `json:"type"`
	ToolName           string          `json:"tool_name,omitempty"`
	ToolStatus         string          `json:"tool_status,omitempty"`
	Text               string          `json:"text,omitempty"`
	ToolResultContents json.RawMessage `json:"tool_result_contents,omitempty"`
	ModelText          string          `json:"model_text,omitempty"`
	Outcome            string          `json:"outcome,omitempty"`
	Availability       string          `json:"availability,omitempty"`
}

// uiObservation stores normalized UI lifecycle records and its derived public outcome.
type uiObservation struct {
	Shared  sharedOutcome
	Records []semanticLifecycleRecord
}

// parseHeadlessOutcome reads shared fields from the one-shot public output.
func parseHeadlessOutcome(stdout, stderr string, err error) sharedOutcome {
	observation := sharedOutcome{
		FinalText:        strings.TrimSpace(stdout),
		CommandSucceeded: err == nil,
		ToolName:         "",
		ToolStatus:       "",
		ToolStartName:    "",
		ToolEndName:      "",
		ToolStarted:      false,
		ToolEnded:        false,
	}
	for line := range strings.SplitSeq(stderr, "\n") {
		switch {
		case strings.HasPrefix(line, "[tool:start] "):
			observation.ToolName = strings.TrimPrefix(line, "[tool:start] ")
			observation.ToolStartName = observation.ToolName
			observation.ToolStarted = true
		case strings.HasPrefix(line, "[tool:end] "):
			parts := strings.SplitN(strings.TrimPrefix(line, "[tool:end] "), ": ", 2)
			if len(parts) == 2 {
				observation.ToolName, observation.ToolStatus = parts[0], parts[1]
				observation.ToolEndName = parts[0]
				observation.ToolEnded = true
			}
		}
	}
	return observation
}

// parseUIObservation validates and normalizes every semantic UI trace line.
func parseUIObservation(t *testing.T, path string) uiObservation {
	t.Helper()
	payload, err := os.ReadFile(path)
	require.NoError(t, err)
	var records []semanticLifecycleRecord
	var finalText, toolName, toolStatus string
	var toolStarted, toolEnded, agentCompleted, settled bool
	var toolStartName, toolEndName string
	for line := range strings.SplitSeq(strings.TrimSpace(string(payload)), "\n") {
		var item struct {
			Type               uipb.LifecycleType `json:"type"`
			Text               string             `json:"text"`
			ModelText          string             `json:"model_text"`
			ToolName           string             `json:"tool_name"`
			ToolStatus         bool               `json:"tool_status"`
			Outcome            string             `json:"outcome"`
			Availability       uipb.Availability  `json:"availability"`
			ToolResultContents json.RawMessage    `json:"tool_result_contents"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &item))
		record := semanticLifecycleRecord{
			Type:               "",
			ToolName:           "",
			ToolStatus:         "",
			Text:               "",
			ToolResultContents: nil,
			ModelText:          "",
			Outcome:            "",
			Availability:       "",
		}
		//nolint:exhaustive // The fixture keeps only lifecycle fields used by the semantic consumer.
		switch item.Type {
		case uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_START:
			record.Type = "agent_start"
		case uipb.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
			record.Type, record.ModelText = "message_end", item.ModelText
			finalText = item.ModelText
		case uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START:
			record.Type, record.ToolName = "tool_execution_start", item.ToolName
			toolStartName = item.ToolName
			toolStarted = true
		case uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END:
			record.Type, record.ToolName = "tool_execution_end", item.ToolName
			toolEndName = item.ToolName
			toolEnded = true
			if item.ToolStatus {
				record.ToolStatus = "ok"
			} else {
				record.ToolStatus = "error"
			}
			toolName, toolStatus = item.ToolName, record.ToolStatus
		case uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
			record.Type, record.ToolName = "tool_result", item.ToolName
			record.ToolResultContents = item.ToolResultContents
		case uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_END:
			record.Type, record.Outcome = "agent_end", item.Outcome
			agentCompleted = item.Outcome == "completed"
		case uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED:
			record.Type = "agent_settled"
			settled = true
		case uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED:
			if !settled || item.Availability != uipb.Availability_AVAILABILITY_IDLE {
				continue
			}
			record.Type, record.Availability = "availability", "idle"
		default:
			continue
		}
		records = append(records, record)
		if item.ToolName != "" {
			toolName = item.ToolName
		}
	}
	return uiObservation{
		Records: records,
		Shared: sharedOutcome{
			FinalText:        finalText,
			ToolName:         toolName,
			ToolStatus:       toolStatus,
			ToolStartName:    toolStartName,
			ToolEndName:      toolEndName,
			ToolStarted:      toolStarted,
			ToolEnded:        toolEnded,
			CommandSucceeded: agentCompleted && settled,
		},
	}
}

// loadSemanticLifecycle provides the exact sequence consumed by the TUI mapping test.
func loadSemanticLifecycle(t *testing.T) []semanticLifecycleRecord {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "semantic-ui-lifecycle.json"))
	require.NoError(t, err)
	var records []semanticLifecycleRecord
	require.NoError(t, json.Unmarshal(payload, &records))
	return records
}
