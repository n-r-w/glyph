//go:build integration

package app

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestStandardGenerationCancellation settles nested provider work before returning without persistence.
func TestStandardGenerationCancellation(t *testing.T) {
	// Arrange a standard generator whose provider waits for cancellation.
	t.Setenv(standardRetainedEnvironment, "1")
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	started, stopped := make(chan struct{}), make(chan struct{})
	transport := newManualNestedCancellationTransport(t, started, stopped, false, "", "")
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildStandardCompaction(t))
	completeProgrammaticRequest(t, fixture, userRequest("standard-cancel-original", manualOriginalText))
	request := new(programmaticpb.OpenRequest)
	request.SetOperationId("standard-cancel-parent")
	programmaticRequest(
		request,
	).SetCompact(programmaticpb.Compact_builder{Instructions: new(manualFixtureNestedCancellationCheckpoint)}.Build())
	require.NoError(t, fixture.stream.Send(request))
	terminal, failures := receiveManualTerminal(fixture, "standard-cancel-parent")
	// Act by canceling the public operation while actual configured-model work is pending.
	select {
	case <-started:
	case event := <-terminal:
		require.FailNow(t, "summary provider did not start", "terminal: %v", event)
	case err := <-failures:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "summary provider did not start")
	}
	require.NoError(t, fixture.stream.Send(cancelRequest("standard-cancel-command", "standard-cancel-parent")))
	var event *programmaticpb.HostEvent
	select {
	case event = <-terminal:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "manual cancellation did not settle")
	}
	// Assert the provider stopped, the caller sees cancellation, and originals remain intact.
	require.True(t, event.HasCanceled() || event.HasFailed())
	select {
	case <-stopped:
	default:
		require.Fail(t, "parent returned before provider stopped")
	}
	stored := sendProgrammaticOperation(
		t,
		fixture,
		"standard-cancel-stored",
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries().
		GetEntries()
	require.Len(t, stored, 2)
	for _, entry := range stored {
		require.Nil(t, entry.GetCompaction())
	}
	fixture.closeOwner(t)
}
