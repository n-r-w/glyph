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

// TestPublicTerminateBatch verifies completion, mixed flags, and pending message delivery through real tools.
func TestPublicTerminateBatch(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name     string
		mixed    bool
		delivery programmaticv1.MessageDelivery
	}{
		{name: "all terminate", mixed: false, delivery: programmaticv1.MessageDelivery_MESSAGE_DELIVERY_UNSPECIFIED},
		{name: "mixed", mixed: true, delivery: programmaticv1.MessageDelivery_MESSAGE_DELIVERY_UNSPECIFIED},
		{name: "steer", mixed: false, delivery: programmaticv1.MessageDelivery_MESSAGE_DELIVERY_STEER},
		{name: "followUp", mixed: false, delivery: programmaticv1.MessageDelivery_MESSAGE_DELIVERY_FOLLOW_UP},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange two concurrent tools held by test-owned signals.
			started := make(chan string, 2)
			first, second := make(chan struct{}), make(chan struct{})
			signals := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id := strings.TrimPrefix(r.URL.Path, "/")
				started <- id
				gate := first
				if id == "second" {
					gate = second
				}
				select {
				case <-gate:
					_, _ = fmt.Fprint(w, id)
				case <-r.Context().Done():
				}
			}))
			defer signals.Close()
			var calls []string
			for _, id := range []string{"first", "second"} {
				arguments, err := json.Marshal(
					map[string]any{"url": signals.URL, "id": id, "terminate": !scenario.mixed || id == "second"},
				)
				require.NoError(t, err)
				quoted, err := json.Marshal(string(arguments))
				require.NoError(t, err)
				calls = append(calls, fmt.Sprintf(batchCallFormat, id, id, concurrentFixtureTool, quoted))
			}
			provider := startQueueProvider(
				t,
				1,
				fmt.Sprintf(batchResponseFormat, strings.Join(calls, ",")),
				finalResponseSSE,
			)
			directory := t.TempDir()
			buildExternalCommand(t, directory, "toolbatch", "batch-extension")
			buildExternalCommand(t, directory, "finalized", "finalized")
			fixture := startQueueFixture(t, provider, directory)
			defer fixture.closeOwner(t)
			require.NoError(t, fixture.stream.Send(userRequest(toolBatchRunID, "finish with tools")))
			<-provider.held[0]
			if scenario.delivery != programmaticv1.MessageDelivery_MESSAGE_DELIVERY_UNSPECIFIED {
				requireQueued(t, sendEnqueue(t, fixture, "pending", scenario.delivery, steerFirstText))
			}
			close(provider.release[0])
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			// Act by completing the second call while the first remains held.
			require.ElementsMatch(
				t,
				[]string{"first", "second"},
				[]string{waitBatchSignal(t, ctx, started), waitBatchSignal(t, ctx, started)},
			)
			close(second)
			for {
				frame, err := fixture.stream.Recv()
				require.NoError(t, err)
				event := frame.GetEvent()
				require.False(
					t,
					event.HasCompleted() || event.HasFailed() || event.HasCanceled(),
					"run settled while sibling was held",
				)
				update := event.GetProgress().GetAgentEvent()
				if update.GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END {
					require.Equal(t, "second", update.GetToolResult().GetCallId())
					require.True(t, update.GetToolResult().GetTerminate())
					break
				}
			}
			close(first)
			observation := observeFinalizedRequest(t, fixture, toolBatchRunID)
			// Assert every remaining event and summary retains the flags, and the provider sees only required requests.
			require.Empty(t, observation.code, observation.message)
			require.False(t, observation.canceled)
			require.Equal(t, programmaticv1.RunOutcome_RUN_OUTCOME_COMPLETED, observation.outcome)
			for _, kind := range []programmaticv1.AgentEventType{
				programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT,
				programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TURN_END,
			} {
				results := observation.toolResults[kind]
				require.Len(t, results, 2)
				assert.Equal(t, !scenario.mixed, results[0].GetTerminate())
				assert.True(t, results[1].GetTerminate())
				assert.Contains(t, toolResultText(results[0]), finalizedToolSuffix)
				assert.Contains(t, toolResultText(results[1]), finalizedToolSuffix)
			}
			expected := 1
			if scenario.mixed || scenario.delivery != programmaticv1.MessageDelivery_MESSAGE_DELIVERY_UNSPECIFIED {
				expected = 2
			}
			bodies := provider.snapshot()
			require.Len(t, bodies, expected)
			if expected > 1 {
				assert.Contains(t, bodies[1], finalizedToolSuffix)
			}
			if scenario.delivery != programmaticv1.MessageDelivery_MESSAGE_DELIVERY_UNSPECIFIED {
				assert.Contains(t, bodies[1], steerFirstText)
			}
		})
	}
}
