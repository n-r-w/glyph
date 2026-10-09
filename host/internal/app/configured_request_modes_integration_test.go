//go:build integration

package app

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
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

const (
	// configuredRequestModelID identifies the explicitly selected nested model.
	configuredRequestModelID = "gpt-test"
	// configuredResponseText is the provider text returned by the nested fixture request.
	configuredResponseText = "Configured response."
)

// TestPublicConfiguredRequestAcrossApplicationModes verifies the external SDK request through every Host assembly.
//
//nolint:paralleltest // The scenarios replace process-global provider HTTP transport.
func TestPublicConfiguredRequestAcrossApplicationModes(t *testing.T) {
	const (
		// configuredRequestMode selects the text-only fixture request.
		configuredRequestMode = "configured-request"
		// configuredInputMode selects the image fixture request.
		configuredInputMode = "configured-input"
		// configuredEmptyRequestMode selects the text-only request with empty instructions.
		configuredEmptyRequestMode = "configured-request-empty"
		// configuredEmptyInputMode selects the image request with empty instructions.
		configuredEmptyInputMode = "configured-input-empty"
		// configuredInstructions contains the exact caller instructions.
		configuredInstructions = "extension instructions"
		// configuredRequestPrompt invokes the fixture tool from the conversation.
		configuredRequestPrompt = "request configured model"
		// configuredProviderRejectionStatus identifies the remote HTTP failure in public errors.
		configuredProviderRejectionStatus = "400 Bad Request "
	)
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
				name: "text", fixtureMode: configuredRequestMode, instructions: configuredInstructions,
				images: false, rejected: false,
			},
			{
				name: "images", fixtureMode: configuredInputMode, instructions: configuredInstructions,
				images: true, rejected: false,
			},
			{
				name: "empty-text", fixtureMode: configuredEmptyRequestMode, instructions: "",
				images: false, rejected: false,
			},
			{
				name: "empty-images", fixtureMode: configuredEmptyInputMode, instructions: "",
				images: true, rejected: false,
			},
			{
				name: "rejected-empty-text", fixtureMode: configuredEmptyRequestMode, instructions: "",
				images: false, rejected: true,
			},
			{
				name: "rejected-empty-images", fixtureMode: configuredEmptyInputMode, instructions: "",
				images: true, rejected: true,
			},
		} {
			t.Run(scenario.name+"/"+input.name, func(t *testing.T) {
				// Arrange isolated application paths, credentials, and provider responses.
				paths := testPaths(t, configuredRequestSettings)
				writeProgrammaticCredentials(t, paths)
				transport, bodies := configuredRequestProviderTransport(t, input.fixtureMode, input.rejected)
				previous := http.DefaultTransport
				http.DefaultTransport = transport
				t.Cleanup(func() { http.DefaultTransport = previous })

				// Act by invoking the contextual fixture tool, which starts and awaits an explicit model request.
				runPublicExtensionMode(
					t, scenario.mode, paths, directory, input.fixtureMode, configuredRequestPrompt,
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
					assert.Contains(t, encoded, configuredProviderRejectionStatus+configuredProviderRejectionDetail)
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
				assert.Equal(t, configuredResponseText, result.GetContent()[0].GetText().GetText())
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
	const (
		// configuredProviderRejectionBody contains the complete remote validation error.
		configuredProviderRejectionBody = `{"error":` + configuredProviderRejectionDetail + `}`
		// configuredModelMatch identifies the nested provider request in serialized JSON.
		configuredModelMatch = `"model":"` + configuredRequestModelID + `"`
		// originalToolNameJSON identifies the tool in the source provider response.
		originalToolNameJSON = `"bash"`
		// configuredToolNameJSON identifies the public fixture tool in the provider response.
		configuredToolNameJSON = `"external"`
		// originalToolArgumentsJSON identifies the tool arguments in the source response.
		originalToolArgumentsJSON = `{\"command\":\"printf tool-ok\"}`
		// configuredToolArgumentsFormat inserts the selected fixture mode into tool arguments.
		configuredToolArgumentsFormat = `{\"mode\":\"%s\"}`
	)

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
				originalToolNameJSON, configuredToolNameJSON,
				originalToolArgumentsJSON, fmt.Sprintf(configuredToolArgumentsFormat, fixtureMode),
			).Replace(toolResponseSSE)
		default:
			if strings.Contains(string(body), configuredModelMatch) {
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
	const (
		// configuredReasoningEffort is the provider effort for the nested reasoning choice.
		configuredReasoningEffort = "high"
		// configuredTextUserContent contains the first user's text-only provider input.
		configuredTextUserContent = `[{"type":"input_text","text":"first user"}]`
		// configuredImageUserContent contains the first user's ordered text and image input.
		configuredImageUserContent = `[{"type":"input_text","text":"first user"},` +
			`{"type":"input_image","detail":"auto","image_url":"data:image/png;base64,AAEC/w=="},` +
			`{"type":"input_text","text":"after image"}]`
		// configuredProviderHistoryFormat inserts the first user's content into the complete history.
		configuredProviderHistoryFormat = `[{"type":"message","role":"user","content":%s},` +
			`{"type":"message","role":"assistant","content":"assistant reply"},` +
			`{"type":"message","role":"assistant","content":"assistant continuation"},` +
			`{"type":"message","role":"user","content":[{"type":"input_text","text":"second user"}]}]`
	)
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
	assert.Equal(t, configuredRequestModelID, request.Model)
	require.NotNil(t, request.Instructions)
	assert.Equal(t, instructions, *request.Instructions)
	assert.Empty(t, request.Tools)
	assert.Equal(t, configuredReasoningEffort, request.Reasoning.Effort)
	// firstContent is the expected first user input for the selected content scenario.
	firstContent := configuredTextUserContent
	if images {
		firstContent = configuredImageUserContent
	}
	// expected is the provider history required by the public request contract.
	expected := fmt.Sprintf(configuredProviderHistoryFormat, firstContent)
	assert.JSONEq(t, expected, string(request.Input))
}

// assertOuterRequestUnaffected verifies the independent request did not change conversation selection or history.
func assertOuterRequestUnaffected(t *testing.T, body []byte) {
	t.Helper()
	const (
		// configuredActiveModelID identifies the unchanged conversation model.
		configuredActiveModelID = "gpt-active"
		// configuredActiveReasoningEffort is the provider effort for disabled conversation reasoning.
		configuredActiveReasoningEffort = "none"
		// configuredFirstUserText identifies the first nested user block for isolation checks.
		configuredFirstUserText = "first user"
		// configuredAssistantReplyText identifies the first nested assistant block for isolation checks.
		configuredAssistantReplyText = "assistant reply"
		// configuredAssistantContinuationText identifies the second nested assistant block for isolation checks.
		configuredAssistantContinuationText = "assistant continuation"
		// configuredAfterImageText identifies the nested text block after the image for isolation checks.
		configuredAfterImageText = "after image"
		// configuredImageDataURL identifies the exact nested image bytes for isolation checks.
		configuredImageDataURL = "data:image/png;base64,AAEC/w=="
		// configuredSecondUserText identifies the last nested user block for isolation checks.
		configuredSecondUserText = "second user"
	)
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
	assert.Equal(t, configuredActiveModelID, request.Model)
	assert.Equal(t, configuredActiveReasoningEffort, request.Reasoning.Effort)
	assert.NotContains(t, string(body), configuredFirstUserText)
	assert.NotContains(t, string(body), configuredAssistantReplyText)
	assert.NotContains(t, string(body), configuredAssistantContinuationText)
	assert.NotContains(t, string(body), configuredAfterImageText)
	assert.NotContains(t, string(body), configuredImageDataURL)
	assert.NotContains(t, string(body), configuredSecondUserText)
}

// configuredRequestSettings defines distinct conversation and nested model capabilities.
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

// configuredModelResponseSSE contains the nested provider response and its usage observation.
const configuredModelResponseSSE = `data: {"type":"response.output_text.delta","output_index":0,` +
	`"content_index":0,"delta":"` + configuredResponseText + `"}` + "\n\n" +
	`data: {"type":"response.output_item.done","output_index":0,` +
	`"item":{"id":"msg-configured","type":"message","role":"assistant","status":"completed",` +
	`"content":[{"type":"output_text","text":"` + configuredResponseText +
	`","annotations":[],"logprobs":[]}]}}` + "\n\n" +
	`data: {"type":"response.completed","response":{"id":"resp-configured","model":"gpt-reported",` +
	`"status":"completed","usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11,` +
	`"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},` +
	`"output_tokens_details":{"reasoning_tokens":3}},"output":[]}}` + "\n\n" +
	"data: [DONE]\n\n"

// configuredProviderRejectionDetail contains the original provider SDK error cause.
const configuredProviderRejectionDetail = `{"message":"remote configured input rejected: exact detail",` +
	`"type":"invalid_request_error","code":"empty_instructions"}`
