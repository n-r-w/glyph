//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestScenario checks owning cancellation of committed observer work.
func (s *programmaticManualCancellationStopsObserverModelSuite) TestScenario() {
	t := s.T()
	// Arrange a committed replacement followed by observer-owned model work.

	releaseProvider := make(chan struct{})
	defer close(releaseProvider)
	t.Setenv(manualFixtureGlyphReferencePolicyInput, filepath.Join(t.TempDir(), manualFixtureInputJson))
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	started := make(chan struct{})
	stopped := make(chan struct{})
	transport := newObserverCancellationTransport(t, started, stopped, releaseProvider)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest(manualFixtureOriginal, manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest(manualFixtureRetained, manualRetainedText))
	request := new(programmaticpb.OpenRequest)
	request.SetOperationId(manualFixtureCancelParent)
	programmaticRequest(
		request,
	).SetCompact(programmaticpb.Compact_builder{Instructions: new(manualFixtureObserveCancellation)}.Build())
	require.NoError(t, fixture.stream.Send(request))
	terminal, receiveErrors := receiveManualTerminal(fixture, manualFixtureCancelParent)

	// Act by canceling the public parent while its nested model request is pending.
	select {
	case <-started:
		require.NoError(t, fixture.stream.Send(cancelRequest(manualFixtureCancelCommand, manualFixtureCancelParent)))
	case event := <-terminal:
		require.Nil(
			t,
			event.GetCompleted().GetCompaction().GetCommitted(),
			"policy returned before nested work started",
		)
		require.FailNow(t, "policy did not start its nested model request")
	case err := <-receiveErrors:
		require.NoError(t, err)
	case <-t.Context().Done():
		require.FailNow(t, "application ended before nested work started")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "owning cancellation did not stop observer provider work")
	}
	event := <-terminal

	// Assert that cancellation retains the durable result and joins provider work.
	result := event.GetCompleted().GetCompaction()
	require.NotNil(t, result, event.GetFailed().GetMessage())
	require.NotNil(t, result.GetCommitted())
	require.Contains(t, result.GetError(), "context canceled")
	stored := sendProgrammaticOperation(
		t,
		fixture,
		manualFixtureCanceledOriginals,
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries().
		GetEntries()
	require.Len(t, stored, 5)
	markers := 0
	for _, entry := range stored {
		if entry.GetCompaction() != nil {
			markers++
		}
	}
	require.Equal(t, 1, markers)
	fixture.closeOwner(t)
}

// newObserverCancellationTransport holds actual observer model execution until cancellation or fixture cleanup.
func newObserverCancellationTransport(
	t *testing.T,
	started, stopped, releaseProvider chan struct{},
) *MockHTTPRoundTripper {
	t.Helper()
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().
		RoundTrip(gomock.Any()).
		AnyTimes().
		DoAndReturn(func(request *http.Request) (*http.Response, error) {
			encoded, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			if bytes.Contains(encoded, []byte(manualFixtureNestedCancellationCheckpoint)) {
				close(started)
				select {
				case <-request.Context().Done():
					close(stopped)
					return nil, request.Context().Err()
				case <-releaseProvider:
					return nil, errors.New("fixture released uncanceled observer model")
				}
			}
			return &http.Response{
				Status:     "",
				StatusCode: http.StatusOK,
				Proto:      "",
				ProtoMajor: 0,
				ProtoMinor: 0,
				Header: make(
					http.Header,
				),
				Body:             io.NopCloser(bytes.NewBufferString(finalResponseSSE)),
				ContentLength:    0,
				TransferEncoding: nil,
				Close:            false,
				Uncompressed:     false,
				Trailer:          nil,
				Request:          nil,
				TLS:              nil,
			}, nil
		})
	return transport
}
