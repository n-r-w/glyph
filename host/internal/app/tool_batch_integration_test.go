//go:build integration

package app

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// toolBatchRunID identifies the request of the concurrent tool scenario.
	toolBatchRunID = "tool-batch"
	// concurrentFixtureTool identifies the declared concurrent tool.
	concurrentFixtureTool = "concurrent"
	// serialFixtureTool identifies the undeclared serial tool.
	serialFixtureTool = "serial"
	// batchResponseFormat wraps model-ordered calls in a terminal Responses event.
	batchResponseFormat = `data: {"type":"response.completed","response":{"id":"batch","model":"gpt-active",` +
		`"status":"completed","output":[%s]}}` + "\n\ndata: [DONE]\n\n"
	// batchCallFormat encodes one Responses tool call with JSON-quoted arguments.
	batchCallFormat = `{"id":%q,"type":"function_call","call_id":%q,"name":%q,"arguments":%s,"status":"completed"}`
)

// TestPublicConcurrentToolBatch verifies reversed completion and serial barriers through a real extension process.
func TestPublicConcurrentToolBatch(t *testing.T) {
	t.Parallel()
	// Arrange test-owned HTTP rendezvous signals and a public-only extension executable.
	ids := []string{"first", "second", "serial-one", "serial-two", "last"}
	started := make(chan string, len(ids))
	release := make(map[string]chan struct{}, len(ids))
	ended := make(map[string]chan struct{}, len(ids))
	for _, id := range ids {
		release[id] = make(chan struct{})
		ended[id] = make(chan struct{})
	}
	signals := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		id := strings.TrimPrefix(request.URL.Path, "/")
		gate, exists := release[id]
		if !exists {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		started <- id
		select {
		case <-gate:
			_, _ = fmt.Fprint(writer, id)
		case <-request.Context().Done():
		}
	}))
	t.Cleanup(signals.Close)
	outputs := make([]string, 0, len(ids))
	for index, id := range ids {
		name := concurrentFixtureTool
		if index == 2 || index == 3 {
			name = serialFixtureTool
		}
		arguments, err := json.Marshal(map[string]string{"url": signals.URL, "id": id})
		require.NoError(t, err)
		quoted, err := json.Marshal(string(arguments))
		require.NoError(t, err)
		outputs = append(outputs, fmt.Sprintf(batchCallFormat, id, id, name, quoted))
	}
	provider := startQueueProvider(t, 0, fmt.Sprintf(batchResponseFormat, strings.Join(outputs, ",")), finalResponseSSE)
	directory := t.TempDir()
	buildExternalCommand(t, directory, "toolbatch", "batch-extension")
	fixture := startQueueFixture(t, provider, directory)
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(toolBatchRunID, "execute the batch")))
	done := make(chan finalizedObservation, 1)
	go func() {
		observation := finalizedObservation{
			outcome:     programmaticv1.RunOutcome_RUN_OUTCOME_UNSPECIFIED,
			code:        "",
			message:     "",
			canceled:    false,
			events:      make(map[programmaticv1.AgentEventType][]string),
			arguments:   nil,
			toolResults: make(map[programmaticv1.AgentEventType][]*programmaticv1.ToolResult),
		}
		for {
			frame, err := fixture.stream.Recv()
			if err != nil {
				observation.code = err.Error()
				done <- observation
				return
			}
			if frame.GetOperationId() != toolBatchRunID || !frame.HasEvent() {
				continue
			}
			event := frame.GetEvent()
			if update := event.GetProgress().GetAgentEvent(); update != nil {
				observation.record(update)
				if update.GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END {
					close(ended[update.GetToolResult().GetCallId()])
				}
			}
			if event.HasCompleted() || event.HasFailed() || event.HasCanceled() {
				if event.HasFailed() {
					observation.code = event.GetFailed().GetFailure().GetCode()
					observation.message = event.GetFailed().GetFailure().GetMessage()
				}
				observation.canceled = event.HasCanceled()
				done <- observation
				return
			}
		}
	}()
	// Act with signals: both concurrent calls start before either may finish.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	initial := []string{waitBatchSignal(t, ctx, started), waitBatchSignal(t, ctx, started)}
	require.ElementsMatch(t, ids[:2], initial)
	close(release["second"])
	waitBatchCompletion(t, ctx, ended["second"])
	assert.Empty(t, started, "serial barrier started before the first group completed")
	close(release["first"])
	waitBatchCompletion(t, ctx, ended["first"])
	for _, id := range ids[2:] {
		require.Equal(t, id, waitBatchSignal(t, ctx, started))
		assert.Empty(t, started, "later call crossed a held serial barrier")
		close(release[id])
		waitBatchCompletion(t, ctx, ended[id])
	}
	var observation finalizedObservation
	select {
	case observation = <-done:
	case <-ctx.Done():
		t.Fatal("batch did not settle: ", ctx.Err())
	}
	// Assert execution events follow work, while durable history and provider replay keep call order.
	require.Empty(t, observation.code, observation.message)
	require.False(t, observation.canceled)
	ends := observation.toolResults[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END]
	require.Len(t, ends, len(ids))
	assert.Equal(t, "second", ends[0].GetCallId())
	assert.Equal(t, "first", ends[1].GetCallId())
	results := observation.toolResults[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT]
	require.Len(t, results, len(ids))
	for index, id := range ids {
		assert.Equal(t, id, results[index].GetCallId())
	}
	bodies := provider.snapshot()
	require.Len(t, bodies, 2)
	var next struct {
		Input []struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
		} `json:"input"`
	}
	require.NoError(t, json.Unmarshal([]byte(bodies[1]), &next))
	var replay []string
	for _, item := range next.Input {
		if item.Type == "function_call_output" {
			replay = append(replay, item.CallID)
		}
	}
	assert.Equal(t, ids, replay)
	history := sendProgrammaticOperation(t, fixture, "batch-history", func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetGetMessages(new(programmaticv1.GetMessages))
	}).GetMessages().GetEntries()
	var persisted []string
	for _, message := range history {
		if result := message.GetToolResult(); result != nil {
			persisted = append(persisted, result.GetCallId())
		}
	}
	assert.Equal(t, ids, persisted)
}

// waitBatchSignal waits for a real extension invocation without polling or fixed sleeps.
func waitBatchSignal(t *testing.T, ctx context.Context, signals <-chan string) string {
	t.Helper()
	select {
	case id := <-signals:
		return id
	case <-ctx.Done():
		t.Fatal("tool did not start: ", ctx.Err())
		return ""
	}
}

// waitBatchCompletion waits until the finalized execution-end event reaches the public client.
func waitBatchCompletion(t *testing.T, ctx context.Context, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal("tool did not finish: ", ctx.Err())
	}
}
