//go:build integration

package app

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/encoding/protojson"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestNavigationSummaryRetryProgressAcrossClients verifies operation-scoped summary retries and terminal navigation.
//
//nolint:paralleltest // Each client scenario replaces process-global provider transport.
func TestNavigationSummaryRetryProgressAcrossClients(t *testing.T) {
	for _, client := range []string{"ui", "programmatic"} {
		t.Run(client, func(t *testing.T) {
			// Arrange one stored abandoned branch and a summary provider that fails once before success.
			paths := testPaths(t, retryModeSettings)
			writeProgrammaticCredentials(t, paths)
			seedSummaryControlSession(t, paths)
			transport, bodies := summaryRetryProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			directory := t.TempDir()

			// Act through the selected assembled public client operation.
			switch client {
			case "ui":
				trace := filepath.Join(t.TempDir(), "summary-retry.json")
				runSummaryControlUI(t, paths, directory, trace, "summary-retry")
				progressData, err := os.ReadFile(trace + ".retry-progress")
				require.NoError(t, err)
				progress := new(uiv1.RetryProgress)
				require.NoError(t, protojson.Unmarshal(progressData, progress))
				assertSummaryRetryProgress(t, progress.GetCompletedAttempts(), progress.GetAttemptLimit(),
					progress.GetDelayMilliseconds(), progress.GetError())
				terminalData, err := os.ReadFile(trace)
				require.NoError(t, err)
				terminal := new(uiv1.SessionTreeNavigationResult)
				require.NoError(t, protojson.Unmarshal(terminalData, terminal))
				assert.Equal(t, uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED,
					terminal.GetStatus())
				assert.Equal(t, "Configured response.", terminal.GetCreatedSummary().GetBranchSummary().GetSummary())
			case "programmatic":
				fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
				sendProgrammaticOperation(t, fixture, "resume", func(request *programmaticv1.OpenRequest) {
					programmaticRequest(request).SetResumeSession(
						programmaticv1.ResumeSession_builder{SessionId: new("source")}.Build(),
					)
				})
				completed, progressEvents := sendProgrammaticOperationWithProgress(
					t, fixture, "summary-retry", func(request *programmaticv1.OpenRequest) {
						programmaticRequest(request).SetNavigateSessionTree(
							programmaticv1.NavigateSessionTree_builder{
								TargetEntryId: new("user"),
								SummaryMode:   new(programmaticv1.SummaryMode_SUMMARY_MODE_SUMMARIZE),
								CustomFocus:   nil,
							}.Build(),
						)
					}, nil,
				)
				var retry *programmaticv1.RetryProgress
				for _, progress := range progressEvents {
					if progress.HasSessionTreeRetry() {
						retry = progress.GetSessionTreeRetry()
					}
				}
				require.NotNil(t, retry)
				assertSummaryRetryProgress(t, retry.GetCompletedAttempts(), retry.GetAttemptLimit(),
					retry.GetDelayMilliseconds(), retry.GetError())
				terminal := completed.GetSessionTreeNavigation()
				require.NotNil(t, terminal)
				assert.Equal(t,
					programmaticv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED,
					terminal.GetStatus(),
				)
				assert.Equal(t, "Configured response.", terminal.GetCreatedSummary().GetBranchSummary().GetSummary())
				fixture.closeOwner(t)
			default:
				require.FailNow(t, "unknown navigation retry client")
			}
			require.Len(t, bodies.snapshot(), 2)
		})
	}
}

// TestNavigationSummaryTerminalFailureAcrossClients verifies identical retry exhaustion and complete causes.
//
//nolint:paralleltest // Each client scenario replaces process-global provider transport.
func TestNavigationSummaryTerminalFailureAcrossClients(t *testing.T) {
	for _, client := range []string{"ui", "programmatic"} {
		t.Run(client, func(t *testing.T) {
			// Arrange one branch-summary operation whose two configured attempts fail with distinct causes.
			paths := testPaths(t, retryModeSettings)
			writeProgrammaticCredentials(t, paths)
			seedSummaryControlSession(t, paths)
			transport, bodies := summaryFailureProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			directory := t.TempDir()
			var code, message string

			// Act through the selected assembled public navigation failure surface.
			switch client {
			case "ui":
				trace := filepath.Join(t.TempDir(), "summary-failure")
				runSummaryControlUI(t, paths, directory, trace, "summary-retry-failure")
				codeData, err := os.ReadFile(trace + ".failure-code")
				require.NoError(t, err)
				messageData, err := os.ReadFile(trace + ".failure-message")
				require.NoError(t, err)
				code, message = string(codeData), string(messageData)
			case "programmatic":
				fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
				sendProgrammaticOperation(t, fixture, "resume-failure", func(request *programmaticv1.OpenRequest) {
					programmaticRequest(request).SetResumeSession(
						programmaticv1.ResumeSession_builder{SessionId: new("source")}.Build(),
					)
				})
				request := testProgrammaticRequest("summary-failure", func(payload *programmaticv1.ControllerRequest) {
					payload.SetNavigateSessionTree(programmaticv1.NavigateSessionTree_builder{
						TargetEntryId: new("user"),
						SummaryMode:   new(programmaticv1.SummaryMode_SUMMARY_MODE_SUMMARIZE),
						CustomFocus:   nil,
					}.Build())
				})
				require.NoError(t, fixture.stream.Send(request))
				for code == "" {
					response, err := fixture.stream.Recv()
					require.NoError(t, err)
					if response.GetOperationId() != "summary-failure" || !response.HasEvent() {
						continue
					}
					if failed := response.GetEvent().GetFailed(); failed != nil {
						code, message = failed.GetCode(), failed.GetMessage()
					}
				}
				fixture.closeOwner(t)
			default:
				require.FailNow(t, "unknown navigation failure client")
			}

			// Assert both clients retain the logical category and every failed-attempt cause.
			assert.Equal(t, "RETRY_EXHAUSTED", code)
			assert.Contains(t, message, "first summary attempt failed")
			assert.Contains(t, message, "second summary attempt failed")
			require.Len(t, bodies.snapshot(), 2)
		})
	}
}

// summaryFailureProviderTransport returns two distinct transient branch-summary failures.
func summaryFailureProviderTransport(t *testing.T) (*MockHTTPRoundTripper, *requestBodies) {
	t.Helper()
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		position := bodies.append(body)
		var response string
		switch position {
		case 1:
			response = strings.Replace(
				retryPartialFailureSSE, "temporary provider failure", "first summary attempt failed", 1,
			)
		case 2:
			response = strings.Replace(
				retryPartialFailureSSE, "temporary provider failure", "second summary attempt failed", 1,
			)
		default:
			t.Fatalf("unexpected summary failure provider request %d", position)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response)),
			Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	return transport, bodies
}

// summaryRetryProviderTransport returns one transient summary failure and one terminal summary response.
func summaryRetryProviderTransport(t *testing.T) (*MockHTTPRoundTripper, *requestBodies) {
	t.Helper()
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		position := bodies.append(body)
		response := configuredModelResponseSSE
		if position == 1 {
			response = retryPartialFailureSSE
		} else if position != 2 {
			t.Fatalf("unexpected summary retry provider request %d", position)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(response)),
			Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
			Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	return transport, bodies
}

// assertSummaryRetryProgress checks exact retry accounting exposed by both public clients.
func assertSummaryRetryProgress(
	t *testing.T,
	completedAttempts int64,
	attemptLimit int64,
	delayMilliseconds int64,
	failure string,
) {
	t.Helper()
	assert.Equal(t, int64(1), completedAttempts)
	assert.Equal(t, int64(2), attemptLimit)
	assert.Zero(t, delayMilliseconds)
	assert.Contains(t, failure, "temporary provider failure")
}
