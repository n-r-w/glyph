//go:build integration

package app

import (
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newCountingFailureTransport returns a mock that records provider requests which must not start.
func newCountingFailureTransport(t *testing.T, requests *atomic.Int32) *MockHTTPRoundTripper {
	t.Helper()
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(
		func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return nil, errors.New("provider request must not start")
		},
	)
	return transport
}

// newDeterministicCodexTransport returns fixed provider responses without network I/O.
func newDeterministicCodexTransport(
	t *testing.T,
	requestCount *atomic.Int32,
	lastBody *atomic.Value,
) *MockHTTPRoundTripper {
	t.Helper()
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(
		func(request *http.Request) (*http.Response, error) {
			if lastBody != nil {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				lastBody.Store(body)
			}
			requestNumber := requestCount.Add(1)
			switch requestNumber {
			case 1:
				return &http.Response{
					StatusCode:       http.StatusOK,
					Body:             io.NopCloser(strings.NewReader(toolResponseSSE)),
					Header:           make(http.Header),
					Status:           "",
					Proto:            "",
					ProtoMajor:       0,
					ProtoMinor:       0,
					ContentLength:    0,
					TransferEncoding: nil,
					Close:            false,
					Uncompressed:     false,
					Trailer:          nil,
					Request:          nil,
					TLS:              nil,
				}, nil
			case 2, 3, 4, 5:
				return &http.Response{
					StatusCode:       http.StatusOK,
					Body:             io.NopCloser(strings.NewReader(finalResponseSSE)),
					Header:           make(http.Header),
					Status:           "",
					Proto:            "",
					ProtoMajor:       0,
					ProtoMinor:       0,
					ContentLength:    0,
					TransferEncoding: nil,
					Close:            false,
					Uncompressed:     false,
					Trailer:          nil,
					Request:          nil,
					TLS:              nil,
				}, nil
			default:
				return nil, errors.New("deterministic Codex transport received more than three requests")
			}
		},
	)
	return transport
}

const toolResponseSSE = `data: {"type":"response.output_item.done","output_index":0,` +
	`"item":{"id":"r-1","type":"reasoning","encrypted_content":"enc-restart","summary":[]}}` + "\n\n" +
	`data: {"type":"response.output_item.added","output_index":1,` +
	`"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"bash",` +
	`"arguments":"","status":"in_progress"}}` + "\n\n" +
	`data: {"type":"response.function_call_arguments.done","output_index":1,` +
	`"item_id":"fc-1","name":"bash","arguments":"{\"command\":\"printf tool-ok\"}"}` + "\n\n" +
	`data: {"type":"response.output_item.done","output_index":1,` +
	`"item":{"id":"fc-1","type":"function_call","call_id":"call-1","name":"bash",` +
	`"arguments":"{\"command\":\"printf tool-ok\"}","status":"completed"}}` + "\n\n" +
	`data: {"type":"response.completed",` +
	`"response":{"id":"resp-1","status":"completed","output":[]}}` + "\n\n" +
	"data: [DONE]\n\n"

const finalResponseSSE = `data: {"type":"response.output_text.delta","output_index":0,` +
	`"content_index":0,"delta":"Request complete."}` + "\n\n" +
	`data: {"type":"response.output_item.done","output_index":0,` +
	`"item":{"id":"msg-1","type":"message","role":"assistant","status":"completed",` +
	`"content":[{"type":"output_text","text":"Request complete.","annotations":[],"logprobs":[]}]}}` + "\n\n" +
	`data: {"type":"response.completed",` +
	`"response":{"id":"resp-2","status":"completed","output":[]}}` + "\n\n" +
	"data: [DONE]\n\n"

// semanticAccessToken creates credentials accepted by the local deterministic provider path.
func semanticAccessToken(t *testing.T, accountID string) string {
	t.Helper()
	payload, err := json.Marshal(
		map[string]any{"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": accountID}},
	)
	require.NoError(t, err)
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
