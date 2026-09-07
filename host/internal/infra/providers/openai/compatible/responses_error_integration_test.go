//go:build integration

package compatible

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestResponsesErrorAndContentDeliveryFailureRetainBoth verifies terminal normalization cannot replace callback causes.
func TestResponsesErrorAndContentDeliveryFailureRetainBoth(t *testing.T) {
	t.Parallel()
	// Arrange partial text, an explicit provider error and a rejected content-finalization callback.
	source := "complete provider source at content finalization"
	callbackErr := errors.New("content finalization output failed")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer,
			`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"partial"}`,
			fmt.Sprintf(`{"type":"error","code":"server_error","message":%q}`, source))
	}))
	t.Cleanup(server.Close)
	driver, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIResponses,
		Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1),
		ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)
	var kinds []run.StreamEventKind

	// Act through the real stream decoder and failed output callback.
	err = driver.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
		kinds = append(kinds, event.Kind)
		if event.Kind == run.StreamEventContentEnd {
			return callbackErr
		}
		return nil
	})

	// Assert the returned cause keeps both failures and delivery stops at the failed callback.
	require.ErrorIs(t, err, callbackErr)
	require.ErrorContains(t, err, source)
	require.Equal(t, run.StreamEventContentEnd, kinds[len(kinds)-1])
}

// TestResponsesErrorEventRetainsCompleteSource exercises supported top-level errors before clean EOF.
func TestResponsesErrorEventRetainsCompleteSource(t *testing.T) {
	t.Parallel()
	for _, partial := range []string{"", "partial text"} {
		t.Run(fmt.Sprintf("partial=%t", partial != ""), func(t *testing.T) {
			t.Parallel()
			// Arrange an authenticated-free endpoint with a complete long source and optional partial output.
			source := "  " + strings.Repeat("界", 4001) + " complete Responses error suffix...  "
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "text/event-stream")
				var frames []string
				if partial != "" {
					frames = append(frames, fmt.Sprintf(
						`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":%q}`, partial,
					))
				}
				frames = append(frames, fmt.Sprintf(
					`{"type":"error","code":"server_error","message":%q,"param":null,"sequence_number":1}`, source,
				))
				writeSSE(t, writer, frames...)
			}))
			t.Cleanup(server.Close)
			driver, err := New(Config{
				ProviderID: "local", BaseURL: server.URL, API: APIResponses,
				Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1),
				ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
			})
			require.NoError(t, err)
			var events []run.StreamEvent

			// Act through the real SDK decoder, adapter and terminal callback.
			err = driver.Stream(t.Context(), richRequest("local", "demo"), func(event run.StreamEvent) error {
				events = append(events, event)
				return nil
			})

			// Assert the source survives both completion boundaries and partial content closes before failure.
			require.Error(t, err)
			assert.Contains(t, err.Error(), source)
			require.NotEmpty(t, events)
			terminal := events[len(events)-1]
			require.Equal(t, run.StreamEventError, terminal.Kind)
			assert.Equal(t, model.OutcomeFailed, terminal.Response.OrEmpty().Outcome.OrEmpty())
			assert.Contains(t, terminal.Response.OrEmpty().ErrorMessage.OrEmpty(), source)
			if partial != "" {
				require.GreaterOrEqual(t, len(events), 2)
				assert.Equal(t, run.StreamEventContentEnd, events[len(events)-2].Kind)
			}
		})
	}
}
