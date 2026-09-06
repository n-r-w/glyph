//go:build integration

package codex

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestSignInWithoutInteractionPreservesSetupAndCleanup rejects presentation after creating the OAuth listener.
func TestSignInWithoutInteractionPreservesSetupAndCleanup(t *testing.T) {
	t.Parallel()
	// Arrange an ephemeral callback listener and no interaction implementation.
	credentials := NewMockCredentials(gomock.NewController(t))
	var callbackListener *net.TCPListener
	options := defaultDriverOptions()
	options.listen = func(network, _ string) (net.Listener, error) {
		listener, err := net.ListenTCP(network, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0, Zone: ""})
		if err == nil {
			callbackListener = listener
		}
		return listener, err
	}
	driver := newDriver(testConfig(), credentials, nil, options)
	// Act through the real OAuth setup and presentation sequence.
	err := driver.SignIn(t.Context())
	// Assert the baseline presentation cause and that cleanup released the callback port.
	require.ErrorIs(t, err, ErrInteractionUnavailable)
	require.EqualError(t, err, "present OpenAI Codex authorization URL: glyph client interaction is unavailable")
	require.NotNil(t, callbackListener)
	replacement, listenErr := net.ListenTCP("tcp4", callbackListener.Addr().(*net.TCPAddr))
	require.NoError(t, listenErr)
	require.NoError(t, replacement.Close())
}
