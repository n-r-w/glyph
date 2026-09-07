//go:build !integration

package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestControllerDecodesTerminalInput preserves Unicode text and exact modifier distinctions.
func TestControllerDecodesTerminalInput(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		// name identifies the decoding boundary.
		name string
		// raw is the framework key input.
		raw tea.Key
		// expected is the framework-neutral interaction input.
		expected Key
	}{
		{
			name: "unicode", raw: frameworkKey('界', "界", 0),
			expected: Key{Code: '界', Text: "界", Mod: 0},
		},
		{
			name: "arrow", raw: frameworkKey(tea.KeyLeft, "", tea.ModAlt),
			expected: Key{Code: KeyLeft, Text: "", Mod: ModAlt},
		},
		{
			name: "combined modifiers", raw: frameworkKey('p', "", tea.ModCtrl|tea.ModShift),
			expected: Key{Code: 'p', Text: "", Mod: ModCtrl | ModShift},
		},
		{
			name: "extra modifier", raw: frameworkKey('q', "", tea.ModCtrl|tea.ModSuper),
			expected: Key{Code: 'q', Text: "", Mod: ModCtrl | ModOther},
		},
		{
			name: "other special key", raw: frameworkKey(tea.KeyF12, "", 0),
			expected: Key{Code: KeyOther, Text: "", Mod: 0},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange the real consumer interface and its expected decoded value.
			interaction := NewMockInteraction(gomock.NewController(t))
			interaction.EXPECT().Key(testCase.expected).Return(nil)
			// Act through the input controller.
			work := New(interaction).Key(testCase.raw)
			// Assert decoding does not invent application work.
			require.Nil(t, work)
		})
	}
}

// TestControllerReturnsCommandOutcome preserves command identity and complete error for the application owner.
func TestControllerReturnsCommandOutcome(t *testing.T) {
	t.Parallel()
	// Arrange one command outcome returned by asynchronous work.
	result := Result{ID: "command", Err: errors.New("complete SDK failure")}
	interaction := NewMockInteraction(gomock.NewController(t))
	interaction.EXPECT().Complete(result).Return(true)
	// Act by returning the outcome through the input consumer.
	quit := New(interaction).Complete(result)
	// Assert the controller retains the application Quit decision.
	require.True(t, quit)
}

// frameworkKey creates one raw key with explicit framework metadata.
func frameworkKey(code rune, text string, mod tea.KeyMod) tea.Key {
	return tea.Key{Code: code, Text: text, Mod: mod, ShiftedCode: 0, BaseCode: 0, IsRepeat: false}
}
