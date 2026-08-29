package codex

import (
	"bytes"
	"context"
	"encoding/json"

	"fmt"

	"net/http"
	"net/http/httptest"

	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	internalhooks "github.com/n-r-w/glyph/host/internal/hooks"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// testConfig creates one provider-owned configuration fixture.
func testConfig() Config {
	return Config{
		ReasoningCompatibilityKeys: nil,
		Hooks:                      testProviderHookRunner(),
		Models: []model.ID{
			"gpt-request", "gpt-test", "gpt-selected", "gpt-unknown", "model", "gpt-5.6-luna",
		},
	}
}

// testModelDescriptor creates an explicitly capable model fixture for adapter tests.
func testModelDescriptor(modelID string) model.Descriptor {
	return model.Descriptor{
		ReasoningCapabilities: model.ReasoningCapabilities{},
		Provider:              ProviderID,
		Model:                 model.ID(modelID),
		Input:                 []model.InputModality{model.InputModalityText},
		ContextWindow:         131072,
		MaxTokens:             16384,
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: true,
			Grammar: model.GrammarCapabilities{
				Lark:  true,
				Regex: true,
			},
		}, Pricing: mo.None[model.Pricing](),
	}
}

func testProviderHookRunner() internalhooks.ProviderRunner {
	return hookrunner.New(nil, nil, nil)
}

// testProviderOptions points both SDK and token HTTP calls at one test server.
func testProviderOptions(server *httptest.Server) driverOptions {
	options := defaultDriverOptions()
	options.modelBaseURL = server.URL
	options.httpClient = server.Client()
	return options
}

// testCredentialPayload encodes one provider-owned credential fixture.
func testCredentialPayload(
	t *testing.T,
	accessToken, refreshToken, accountID string,
	expiresAt time.Time,
) []byte {
	t.Helper()
	payload, err := json.Marshal(oauthCredentials{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		AccountID:    accountID,
		ExpiresAt:    expiresAt,
	})
	require.NoError(t, err)
	return payload
}

// inputTypes returns the ordered Responses item discriminators from a captured request.
func inputTypes(input []any) []string {
	return lo.Map(input, func(item any, _ int) string {
		value, _ := item.(map[string]any)["type"].(string)
		return value
	})
}

// completedEvent constructs one successful terminal Responses SSE event.
func completedEvent(output string) string {
	return `{"type":"response.completed","response":{"id":"resp","status":"completed","output":` + output + `}}`
}

// writeSSE writes typed SDK events without a custom parser in production.
func writeSSE(writer http.ResponseWriter, events ...string) {
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.WriteHeader(http.StatusOK)
	for _, event := range events {
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(event)); err != nil {
			panic("invalid SSE test fixture: " + event)
		}
		_, _ = fmt.Fprintf(writer, "data: %s\n\n", compact.String())
	}
	_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
}

// streamEventKinds returns semantic event identities in delivery order.
func streamEventKinds(events []run.StreamEvent) []run.StreamEventKind {
	return lo.Map(events, func(event run.StreamEvent, _ int) run.StreamEventKind {
		return event.Kind
	})
}

// collectStreamEvents returns every provider event in delivery order.
func collectStreamEvents(
	service *Driver,
	ctx context.Context,
	request run.ModelRequest,
	handleEvent func(run.StreamEvent) error,
) ([]run.StreamEvent, error) {
	events := make([]run.StreamEvent, 0)
	err := service.Stream(ctx, request, func(event run.StreamEvent) error {
		events = append(events, event)
		if handleEvent != nil {
			return handleEvent(event)
		}
		return nil
	})
	return events, err
}

func terminalResponse(events []run.StreamEvent) model.Response {
	if len(events) == 0 {
		return model.Response{}
	}
	return events[len(events)-1].Response.OrEmpty()
}
