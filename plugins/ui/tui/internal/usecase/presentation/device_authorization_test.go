//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
)

// TestDeviceAuthorizationCodeReachesDisplay checks decoded progress through the application snapshot.
func TestDeviceAuthorizationCodeReachesDisplay(t *testing.T) {
	t.Parallel()
	// Arrange an authenticating application and a decoded one-time-code challenge.
	service := newTestModel(t, AvailabilityAuthenticating, nil)
	payload := plugininput.TextPayload(plugininput.TextUpdate{
		Kind: plugininput.TextAuthorization, Text: "https://example.test/device",
		AuthorizationCode: mo.Some("ABCD-EFGH"), FailureCode: "",
	})
	// Act through the application input transition.
	require.NoError(t, service.applyInput(mo.Some(payload)))
	service.publish()
	// Assert the display receives the code separately from the URL and transcript.
	require.Equal(t, mo.Some("ABCD-EFGH"), service.body.AuthorizationCode)
	require.Equal(t, mo.Some("https://example.test/device"), service.body.AuthorizationURL)
	require.Empty(t, service.body.Transcript)
}

// TestAuthenticationChallengeClearsOnAvailabilityChange prevents completed or canceled codes from lingering.
func TestAuthenticationChallengeClearsOnAvailabilityChange(t *testing.T) {
	t.Parallel()
	for _, availability := range []Availability{
		AvailabilityIdle, AvailabilityAuthenticationFailed, AvailabilityAuthenticating,
	} {
		// Arrange a previously displayed authorization challenge.
		challenge := textEvent(eventAuthorization, "https://example.test/device")
		challenge.AuthorizationCode = mo.Some("OLD-CODE")
		state := (projection{}).Apply(challenge)
		update := newEvent(eventAvailability)
		update.Availability = mo.Some(availability)
		// Act when the attempt ends or a new attempt begins.
		state = state.Apply(update)
		// Assert no stale URL or one-time code remains visible.
		require.True(t, state.AuthorizationURL.IsNone())
		require.True(t, state.AuthorizationCode.IsNone())
	}
}
