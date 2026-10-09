//go:build integration

package app

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestPublicConfiguredRequestAcrossApplicationModes verifies the external SDK request through every Host assembly.
//
//nolint:paralleltest // The scenarios replace process-global provider HTTP transport.
func TestPublicConfiguredRequestAcrossApplicationModes(t *testing.T) {
	// Arrange one public-only extension binary shared by each isolated application scenario.
	directory := buildPublicExtensionFixture(t)
	for _, scenario := range []struct {
		// name identifies the application assembly.
		name string
		// mode selects the connected Glyph client contract.
		mode cli.Mode
	}{
		{name: "headless", mode: cli.ModeHeadless},
		{name: "ui", mode: cli.ModeUI},
		{name: "programmatic", mode: cli.ModeRPC},
	} {
		for _, input := range []struct {
			// name identifies the public input scenario.
			name string
			// fixtureMode selects the extension request operation.
			fixtureMode string
			// instructions contains the caller's exact instructions.
			instructions string
			// images selects ordered image assertions.
			images bool
			// rejected selects a remote provider rejection.
			rejected bool
		}{
			{
				name: "text", fixtureMode: "configured-request", instructions: "extension instructions",
				images: false, rejected: false,
			},
			{
				name: "images", fixtureMode: "configured-input", instructions: "extension instructions",
				images: true, rejected: false,
			},
			{
				name: "empty-text", fixtureMode: "configured-request-empty", instructions: "",
				images: false, rejected: false,
			},
			{
				name: "empty-images", fixtureMode: "configured-input-empty", instructions: "",
				images: true, rejected: false,
			},
			{
				name: "rejected-empty-text", fixtureMode: "configured-request-empty", instructions: "",
				images: false, rejected: true,
			},
			{
				name: "rejected-empty-images", fixtureMode: "configured-input-empty", instructions: "",
				images: true, rejected: true,
			},
		} {
			t.Run(scenario.name+"/"+input.name, func(t *testing.T) {
				paths := testPaths(t, configuredRequestSettings)
				writeProgrammaticCredentials(t, paths)
				transport, bodies := configuredRequestProviderTransport(t, input.fixtureMode, input.rejected)
				previous := http.DefaultTransport
				http.DefaultTransport = transport
				t.Cleanup(func() { http.DefaultTransport = previous })

				// Act by invoking the contextual fixture tool, which starts and awaits an explicit model request.
				runPublicExtensionMode(
					t, scenario.mode, paths, directory, input.fixtureMode, "request configured model",
				)

				// Assert the nested request used its explicit model and history without changing the outer selection or history.
				actualBodies := bodies.snapshot()
				require.GreaterOrEqual(t, len(actualBodies), 3)
				assert.Len(t, actualBodies, 3, "invalid input must not reach the provider")
				assertConfiguredProviderRequest(t, actualBodies[1], input.instructions, input.images)
				outerBody := actualBodies[len(actualBodies)-1]
				assertOuterRequestUnaffected(t, outerBody)
				encoded := externalToolOutput(t, outerBody)
				if input.rejected {
					assert.Contains(t, encoded, "400 Bad Request "+configuredProviderRejectionDetail)
					return
				}
				if input.images {
					var report struct {
						// ResultJSON is the nested public response.
						ResultJSON string `json:"result_json"`
						// Errors contains complete public rejection causes.
						Errors map[string]string `json:"errors"`
					}
					require.NoError(t, json.Unmarshal([]byte(encoded), &report))
					assertConfiguredInputErrors(t, report.Errors)
					encoded = report.ResultJSON
				}
				result := new(extensionpb.ConfiguredModelResult)
				require.NoError(t, protojson.Unmarshal([]byte(encoded), result))
				require.Len(t, result.GetContent(), 1)
				assert.Equal(t, "Configured response.", result.GetContent()[0].GetText().GetText())
				assert.Equal(t, extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_STOP, result.GetOutcome())
				assert.Equal(t, int64(11), result.GetUsage().GetTotalTokens())
			})
		}
	}
}

// requestBodies stores provider request bytes in arrival order.
type requestBodies struct {
	// mutex protects bodies across nested provider requests.
	mutex sync.Mutex
	// bodies contains defensive copies in arrival order.
	bodies [][]byte
}

// append stores one defensive request copy and returns its one-based position.
func (b *requestBodies) append(body []byte) int {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.bodies = append(b.bodies, bytes.Clone(body))
	return len(b.bodies)
}

// snapshot returns defensive request copies for assertions.
func (b *requestBodies) snapshot() [][]byte {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	result := make([][]byte, len(b.bodies))
	for index := range b.bodies {
		result[index] = bytes.Clone(b.bodies[index])
	}
	return result
}

// configuredRequestProviderTransport returns one tool call, one nested response, and one outer response.
func configuredRequestProviderTransport(
	t *testing.T,
	fixtureMode string,
	rejected bool,
) (*MockHTTPRoundTripper, *requestBodies) {
	t.Helper()
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		position := bodies.append(body)
		response := finalResponseSSE
		statusCode := http.StatusOK
		switch position {
		case 1:
			response = strings.NewReplacer(
				`"bash"`, `"external"`,
				`{\"command\":\"printf tool-ok\"}`, `{\"mode\":\"`+fixtureMode+`\"}`,
			).Replace(toolResponseSSE)
		default:
			if strings.Contains(string(body), `"model":"gpt-test"`) {
				response = configuredModelResponseSSE
				if rejected {
					response = configuredProviderRejectionBody
					statusCode = http.StatusBadRequest
				}
			}
		}
		return &http.Response{
			StatusCode: statusCode, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header),
			Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	return transport, bodies
}

// assertConfiguredProviderRequest checks exact nested selection, instructions, roles, and ordered content.
func assertConfiguredProviderRequest(t *testing.T, body []byte, instructions string, images bool) {
	t.Helper()
	var request struct {
		// Model identifies the explicit configured model.
		Model string `json:"model"`
		// Instructions contains exact extension instructions.
		Instructions *string `json:"instructions"`
		// Input contains the complete ordered provider history.
		Input jsontext.Value `json:"input"`
		// Tools contains supplied provider tools.
		Tools []jsontext.Value `json:"tools"`
		// Reasoning contains the provider reasoning control.
		Reasoning struct {
			// Effort contains the mapped explicit reasoning choice.
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	require.NoError(t, json.Unmarshal(body, &request))
	assert.Equal(t, "gpt-test", request.Model)
	require.NotNil(t, request.Instructions)
	assert.Equal(t, instructions, *request.Instructions)
	assert.Empty(t, request.Tools)
	assert.Equal(t, "high", request.Reasoning.Effort)
	firstContent := `[{"type":"input_text","text":"first user"}]`
	if images {
		firstContent = `[{"type":"input_text","text":"first user"},` +
			`{"type":"input_image","detail":"auto","image_url":"data:image/png;base64,AAEC/w=="},` +
			`{"type":"input_text","text":"after image"}]`
	}
	expected := `[{"type":"message","role":"user","content":` + firstContent + `},` +
		`{"type":"message","role":"assistant","content":"assistant reply"},` +
		`{"type":"message","role":"assistant","content":"assistant continuation"},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"second user"}]}]`
	assert.JSONEq(t, expected, string(request.Input))
}

// assertOuterRequestUnaffected verifies the independent request did not change conversation selection or history.
func assertOuterRequestUnaffected(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		// Model identifies the active conversation model.
		Model string `json:"model"`
		// Reasoning contains the active conversation reasoning control.
		Reasoning struct {
			// Effort contains the mapped active reasoning choice.
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	require.NoError(t, json.Unmarshal(body, &request))
	assert.Equal(t, "gpt-active", request.Model)
	assert.Equal(t, "none", request.Reasoning.Effort)
	assert.NotContains(t, string(body), "first user")
	assert.NotContains(t, string(body), "assistant reply")
	assert.NotContains(t, string(body), "assistant continuation")
	assert.NotContains(t, string(body), "after image")
	assert.NotContains(t, string(body), "data:image/png;base64,AAEC/w==")
	assert.NotContains(t, string(body), "second user")
}

const configuredRequestSettings = `defaultProvider: openai-codex
defaultModel: gpt-active
providers:
  openai-codex:
    type: openai-codex
    models:
      - id: gpt-active
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        pricing: {input: 0, output: 0, cacheRead: 0, cacheWrite: 0}
        reasoning: {supported: false, choices: [off], default: off}
      - id: gpt-test
        input: [text, image]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        pricing: {input: 0, output: 0, cacheRead: 0, cacheWrite: 0}
        reasoning: {supported: true, choices: [off, high], default: off}
`

const configuredModelResponseSSE = `data: {"type":"response.output_text.delta","output_index":0,` +
	`"content_index":0,"delta":"Configured response."}` + "\n\n" +
	`data: {"type":"response.output_item.done","output_index":0,` +
	`"item":{"id":"msg-configured","type":"message","role":"assistant","status":"completed",` +
	`"content":[{"type":"output_text","text":"Configured response.","annotations":[],"logprobs":[]}]}}` + "\n\n" +
	`data: {"type":"response.completed","response":{"id":"resp-configured","model":"gpt-reported",` +
	`"status":"completed","usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11,` +
	`"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},` +
	`"output_tokens_details":{"reasoning_tokens":3}},"output":[]}}` + "\n\n" +
	"data: [DONE]\n\n"

// configuredProviderRejectionBody contains the complete remote validation error.
const configuredProviderRejectionBody = `{"error":` + configuredProviderRejectionDetail + `}`

// configuredProviderRejectionDetail contains the original provider SDK error cause.
const configuredProviderRejectionDetail = `{"message":"remote configured input rejected: exact detail",` +
	`"type":"invalid_request_error","code":"empty_instructions"}`
