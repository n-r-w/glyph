//go:build integration

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestStandardOverflowRecovery exercises admitted standard generation through Programmatic Control.
func TestStandardOverflowRecovery(t *testing.T) {
	// Arrange a fitting conversation, the standard executable, and an independent outcome observer.
	t.Setenv(standardRetainedEnvironment, "1")
	directory, outcomePath := buildStandardOverflowFixture(t)
	bodies := installStandardOverflowTransport(t, newStandardOverflowRecoveryResponse)
	fixture := startStandardOverflowFixture(t, directory, "3\n  delays: [1ms, 1ms, 1ms]")
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("overflow-original", manualOriginalText))

	// Act through ordinary submission and the provider's context-overflow classification.
	require.NoError(t, fixture.stream.Send(userRequest("overflow-recent", manualRetainedText)))
	terminal := receiveAutomaticTerminal(t, fixture, "overflow-recent").GetEvent()

	// Assert one standard generation, one durable replacement, and the retried projection.
	require.NotNil(t, terminal.GetCompleted(), "failure: %v", terminal.GetFailed())
	marker := assertStandardOverflowHistory(t, fixture, true, true, 0)
	actual := bodies.snapshot()
	require.Len(t, actual, 4)
	require.Contains(t, string(actual[2]), manualOriginalText)
	assertStandardOverflowProjection(t, actual[3], marker)
	outcome := readStandardOverflowOutcome(t, outcomePath)
	assertStandardOverflowIntent(t, outcome.GetState().GetOriginalRequest())
	require.Equal(t, marker.GetId(), outcome.GetCommitted().GetId())
}

// TestStandardOverflowSuppliedResult exposes recovery intent to a participant that bypasses generation.
func TestStandardOverflowSuppliedResult(t *testing.T) {
	// Arrange the standard policy and an independent public request participant.
	t.Setenv(standardRetainedEnvironment, "1")
	directory, outcomePath := buildStandardOverflowFixture(t)
	requestInputs := t.TempDir()
	t.Setenv("GLYPH_REFERENCE_SEPARATE_REQUESTS", "0")
	t.Setenv(conversationProviderOverflowEnvironment, manualFixtureEnabled)
	t.Setenv("GLYPH_REFERENCE_REQUEST_INPUTS", requestInputs)
	bodies := installStandardOverflowTransport(t, newStandardOverflowRecoveryResponse)
	fixture := startStandardOverflowFixture(t, directory, "3\n  delays: [1ms, 1ms, 1ms]")
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("overflow-original", manualOriginalText))

	// Act through ordinary submission and admitted participant composition.
	require.NoError(t, fixture.stream.Send(userRequest("overflow-recent", manualRetainedText)))
	terminal := receiveAutomaticTerminal(t, fixture, "overflow-recent").GetEvent()

	// Assert supplied-result bypass, original/current retry intent, and one durable replacement.
	require.NotNil(t, terminal.GetCompleted(), "failure: %v", terminal.GetFailed())
	marker := assertStandardOverflowHistory(t, fixture, true, true, 0)
	actual := bodies.snapshot()
	require.Len(t, actual, 3)
	assertStandardOverflowProjection(t, actual[2], marker)
	captured, err := os.ReadDir(requestInputs)
	require.NoError(t, err)
	require.Len(t, captured, 1)
	encoded, err := os.ReadFile(filepath.Join(requestInputs, captured[0].Name()))
	require.NoError(t, err)
	invocation := new(extensionpb.CompactionRequestInvocation)
	require.NoError(t, protojson.Unmarshal(encoded, invocation))
	require.Equal(t, extensionpb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW, invocation.GetOriginal().GetTrigger())
	require.True(t, invocation.GetOriginal().GetRetryIntent())
	require.True(t, invocation.GetCurrent().GetRetryIntent())
	outcome := readStandardOverflowOutcome(t, outcomePath)
	assertStandardOverflowIntent(t, outcome.GetState().GetOriginalRequest())
	require.Equal(t, marker.GetId(), outcome.GetCommitted().GetId())
}

// TestStandardOverflowCompletedTool retains a completed tool while repeating only the failed model request.
func TestStandardOverflowCompletedTool(t *testing.T) {
	// Arrange a real public tool operation before provider overflow and standard generation.
	t.Setenv(standardRetainedEnvironment, "1")
	directory, outcomePath := buildStandardOverflowFixture(t)
	toolPath := filepath.Join(t.TempDir(), "completed-tool.json")
	t.Setenv(conversationToolEnvironment, toolPath)
	toolSnapshots := &requestBodies{}
	bodies := installStandardOverflowTransport(t, func(position int) (*http.Response, error) {
		switch position {
		case 2:
			return runtimeFailureHTTPResponse(conversationToolResponseSSE), nil
		case 3:
			toolBytes, err := os.ReadFile(toolPath)
			if err != nil {
				return nil, err
			}
			toolSnapshots.append(toolBytes)
			return newStandardOverflowResponse("provider context limit source"), nil
		default:
			return runtimeFailureHTTPResponse(finalResponseSSE), nil
		}
	})
	fixture := startStandardOverflowFixture(t, directory, "3\n  delays: [1ms, 1ms, 1ms]")
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("overflow-original", manualOriginalText))

	// Act through ordinary submission, completed tool execution, and admitted recovery.
	require.NoError(t, fixture.stream.Send(userRequest("overflow-recent", manualRetainedText)))
	terminal := receiveAutomaticTerminal(t, fixture, "overflow-recent").GetEvent()

	// Assert one tool execution, one generation, and preservation of the tool in the retried input.
	require.NotNil(t, terminal.GetCompleted(), "failure: %v", terminal.GetFailed())
	marker := assertStandardOverflowHistory(t, fixture, true, true, 1)
	actual := bodies.snapshot()
	require.Len(t, actual, 5)
	require.Contains(t, string(actual[3]), manualOriginalText)
	assertStandardOverflowProjection(t, actual[4], marker)
	assertSingleCompletedToolInRequest(t, actual[2])
	assertSingleCompletedToolInRequest(t, actual[4])
	encoded, err := os.ReadFile(toolPath)
	require.NoError(t, err)
	require.Equal(t, toolSnapshots.snapshot()[0], encoded)
	require.Equal(t, 1, strings.Count(string(encoded), "\n"))
	outcome := readStandardOverflowOutcome(t, outcomePath)
	assertStandardOverflowIntent(t, outcome.GetState().GetOriginalRequest())
	require.Equal(t, marker.GetId(), outcome.GetCommitted().GetId())
}
