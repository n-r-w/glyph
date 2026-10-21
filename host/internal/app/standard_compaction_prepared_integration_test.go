//go:build integration

package app

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestStandardCompactionExcludesFailedSource preserves the journal while summarizing prepared content.
func TestStandardCompactionExcludesFailedSource(t *testing.T) {
	// Arrange a real bundled policy and a provider that persists a partial failed response.
	const discarded = "discarded-assistant-source"
	const failed = `data: {"type":"response.output_text.delta","output_index":0,"content_index":0,` +
		`"delta":"discarded-assistant-source"}` + "\n\n" +
		`data: {"type":"response.failed","response":{"id":"failed-source","model":"demo","status":"failed",` +
		`"error":{"code":"invalid_input","message":"prepared source failure"},"output":[]}}` + "\n\n"
	t.Setenv(standardRetainedEnvironment, "1")
	directory := buildStandardCompaction(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	bodies := &requestBodies{}
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().
		RoundTrip(gomock.Any()).
		AnyTimes().
		DoAndReturn(func(request *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			position := bodies.append(body)
			response := finalResponseSSE
			if position == 1 {
				response = failed
			}
			return &http.Response{
				Status:           "",
				StatusCode:       http.StatusOK,
				Proto:            "",
				ProtoMajor:       0,
				ProtoMinor:       0,
				Header:           make(http.Header),
				Body:             io.NopCloser(bytes.NewBufferString(response)),
				ContentLength:    0,
				TransferEncoding: nil,
				Close:            false,
				Uncompressed:     false,
				Trailer:          nil,
				Request:          nil,
				TLS:              nil,
			}, nil
		})
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	require.NoError(t, fixture.stream.Send(userRequest("prepared-failure", manualOriginalText)))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() == "prepared-failure" && response.GetEvent().HasFailed() {
			break
		}
	}
	completeProgrammaticRequest(t, fixture, userRequest("prepared-recent", manualRetainedText))
	// Act through the public compaction operation.
	compacted := requestProgrammaticCompaction(t, fixture, "prepared-compact", "")
	require.NotNil(t, compacted.GetCompleted(), "%v", compacted)
	stored := sendProgrammaticOperation(t, fixture, "prepared-stored", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	// Assert excluded text stays in original records and never reaches the summary generator.
	require.Contains(t, stored[1].String(), discarded)
	actual := bodies.snapshot()
	require.Len(t, actual, 3)
	require.NotContains(t, string(actual[1]), discarded)
	require.NotContains(t, string(actual[2]), discarded)
	require.Contains(t, string(actual[2]), manualOriginalText)
	fixture.closeOwner(t)
}
