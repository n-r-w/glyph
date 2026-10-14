//go:build integration

package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestConfiguredRetryCancellationThroughExtension checks received retry diagnostics through ordinary configured calls.
func TestConfiguredRetryCancellationThroughExtension(t *testing.T) {
	const (
		fixtureMode = "configured-request-cancellation"
		requestID   = "configured-cancellation"
	)
	// Arrange the public extension, real model execution and a provider that fails before cancellation.
	directory := buildPublicExtensionFixture(t)
	paths := testPaths(t, retryModeSettings)
	writeProgrammaticCredentials(t, paths)
	t.Setenv("GLYPH_EXTERNAL_SIGNALS", t.TempDir())
	t.Setenv("GLYPH_EXTERNAL_RETRY_HANDLER", "1")
	bodies := new(requestBodies)
	var nestedAttempts atomic.Int64
	var outerAttempts atomic.Int64
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		bodies.append(body)
		var input struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &input); err != nil {
			return nil, err
		}
		response := finalResponseSSE
		if input.Model == configuredRequestModelID {
			switch nestedAttempts.Add(1) {
			case 1:
				response = retryPartialFailureSSE
			case 2:
				<-request.Context().Done()
				return nil, context.Cause(request.Context())
			default:
				return nil, errors.New("configured provider work continued after cancellation")
			}
		} else {
			switch outerAttempts.Add(1) {
			case 1:
				response = strings.NewReplacer(
					`"bash"`, `"external"`,
					`{\"command\":\"printf tool-ok\"}`, `{\"mode\":\"`+fixtureMode+`\"}`,
				).Replace(toolResponseSSE)
			case 2:
			default:
				return nil, fmt.Errorf("unexpected outer provider work: %s", input.Model)
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response)),
			Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	// Act through a tool invocation whose SDK callback cancels only its configured Start context after retry progress.
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest(requestID, "request configured cancellation"))
	fixture.closeOwner(t)

	// Assert the live SDK retained the actual provider retry cause and joined work before the outer conversation resumed.
	require.Equal(t, int64(2), outerAttempts.Load())
	require.GreaterOrEqual(t, nestedAttempts.Load(), int64(1))
	require.LessOrEqual(t, nestedAttempts.Load(), int64(2))
	requests := bodies.snapshot()
	require.Len(t, requests, int(outerAttempts.Load()+nestedAttempts.Load()))
	var report struct {
		LocalText     string `json:"local_text"`
		LocalCanceled bool   `json:"local_canceled"`
		Text          string `json:"text"`
		Canceled      bool   `json:"canceled"`
		Typed         bool   `json:"typed"`
		Rewrapped     bool   `json:"rewrapped"`
		Progress      []struct {
			CompletedAttempts int64  `json:"completed_attempts"`
			AttemptLimit      int64  `json:"attempt_limit"`
			Error             string `json:"error"`
		} `json:"progress"`
	}
	require.NoError(t, json.Unmarshal([]byte(externalToolOutput(t, requests[len(requests)-1])), &report))
	assert.Contains(t, report.LocalText, context.Canceled.Error())
	assert.NotContains(t, report.LocalText, "temporary provider failure")
	assert.True(t, report.LocalCanceled)
	require.True(t, report.Typed)
	assert.True(t, report.Canceled)
	assert.True(t, report.Rewrapped)
	require.Len(t, report.Progress, 1)
	assert.Equal(t, int64(1), report.Progress[0].CompletedAttempts)
	assert.Equal(t, int64(2), report.Progress[0].AttemptLimit)
	require.Contains(t, report.Progress[0].Error, "temporary provider failure")
	assert.Contains(t, report.Text, report.Progress[0].Error)
}
