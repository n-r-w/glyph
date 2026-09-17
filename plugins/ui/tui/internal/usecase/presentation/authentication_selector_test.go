//go:build !integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
)

// TestAuthenticationSelectorChoosesEitherFlow checks that retry opens a menu before sending a method.
func TestAuthenticationSelectorChoosesEitherFlow(t *testing.T) {
	t.Parallel()
	for _, method := range []AuthenticationMethod{AuthenticationMethodBrowser, AuthenticationMethodDeviceCode} {
		t.Run(
			map[AuthenticationMethod]string{
				AuthenticationMethodBrowser:    "browser",
				AuthenticationMethodDeviceCode: "code",
			}[method],
			func(t *testing.T) {
				t.Parallel()
				// Arrange an authentication failure while preserving an unsent draft.
				initial := newEvent(eventAvailability)
				initial.Availability = mo.Some(AvailabilityAuthenticationFailed)
				model := newInteraction(initial)
				model.input, model.cursor = []rune("draft"), 5
				// Act by requesting sign-in without selecting a method yet.
				model, intent := model.updateKey(tuiinput.Key{Code: 'r', Mod: tuiinput.ModCtrl, Text: ""})
				// Assert opening the selector does not start network authentication.
				require.Nil(t, intent)
				require.True(t, model.selectorOpen)
				require.True(t, model.authenticationSelector)
				if method == AuthenticationMethodDeviceCode {
					model, intent = model.updateKey(tuiinput.Key{Code: tuiinput.KeyDown, Mod: 0, Text: ""})
					require.Nil(t, intent)
				}
				// Act by confirming the highlighted method.
				model, intent = model.updateKey(tuiinput.Key{Code: tuiinput.KeyEnter, Mod: 0, Text: ""})
				// Assert the selected flow is explicit and the editor is preserved.
				require.NotNil(t, intent)
				assert.Equal(t, CommandRetryAuthentication, intent.command.Kind)
				assert.Equal(t, method, intent.command.AuthenticationMethod)
				assert.False(t, model.selectorOpen)
				assert.False(t, model.authenticationSelector)
				assert.Equal(t, "draft", string(model.input))
				assert.Equal(t, 5, model.cursor)
			},
		)
	}
}

// TestAuthenticationSelectorCancellationKeepsDraft checks local cancellation without a Host operation.
func TestAuthenticationSelectorCancellationKeepsDraft(t *testing.T) {
	t.Parallel()
	// Arrange the authentication selector over a retained editor draft.
	initial := newEvent(eventAvailability)
	initial.Availability = mo.Some(AvailabilityAuthenticationFailed)
	model := newInteraction(initial)
	model.input, model.cursor = []rune("draft"), 5
	model, _ = model.updateKey(tuiinput.Key{Code: 'r', Mod: tuiinput.ModCtrl, Text: ""})
	// Act by canceling the selector locally.
	model, intent := model.updateKey(tuiinput.Key{Code: tuiinput.KeyEscape, Mod: 0, Text: ""})
	// Assert no operation is emitted and the request draft remains usable.
	require.Nil(t, intent)
	assert.False(t, model.selectorOpen)
	assert.False(t, model.authenticationSelector)
	assert.Equal(t, "draft", string(model.input))
}

// TestAuthenticationCanBeStopped checks that the normal stop key applies to pending sign-in too.
func TestAuthenticationCanBeStopped(t *testing.T) {
	t.Parallel()
	// Arrange an admitted asynchronous authentication attempt.
	initial := newEvent(eventAvailability)
	initial.Availability = mo.Some(AvailabilityAuthenticating)
	model := newInteraction(initial)
	// Act through the normal stop key.
	_, intent := model.updateKey(tuiinput.Key{Code: 'c', Mod: tuiinput.ModCtrl, Text: ""})
	// Assert cancellation targets the authentication operation rather than blocking input.
	require.NotNil(t, intent)
	assert.Equal(t, CommandStop, intent.command.Kind)
}
