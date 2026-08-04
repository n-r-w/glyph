package headless

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseAcceptsOneHeadlessRequest verifies the command and extension override shapes.
func TestParseAcceptsOneHeadlessRequest(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		arguments []string
		expected  Command
	}{
		"default extensions": {
			arguments: []string{"run", "fix the bug"},
			expected:  Command{UserText: "fix the bug", ExtensionDirectory: ""},
		},
		"separate override": {
			arguments: []string{"run", "--extension-dir", "/tmp/extensions", "fix the bug"},
			expected:  Command{UserText: "fix the bug", ExtensionDirectory: "/tmp/extensions"},
		},
		"inline override": {
			arguments: []string{"run", "--extension-dir=/tmp/extensions", "fix the bug"},
			expected:  Command{UserText: "fix the bug", ExtensionDirectory: "/tmp/extensions"},
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			command, err := Parse(testCase.arguments)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, command)
		})
	}
}

// TestParseRejectsInvalidHeadlessArguments verifies UI isolation and exact request cardinality.
func TestParseRejectsInvalidHeadlessArguments(t *testing.T) {
	t.Parallel()

	testCases := map[string][]string{
		"missing command":        nil,
		"unknown command":        {"chat", "request"},
		"missing request":        {"run"},
		"extra request":          {"run", "first", "second"},
		"UI selection":           {"run", "--ui", "glyph-tui", "request"},
		"inline UI selection":    {"run", "--ui=glyph-tui", "request"},
		"UI directory":           {"run", "--ui-dir", "/tmp/ui", "request"},
		"empty extension path":   {"run", "--extension-dir=", "request"},
		"missing extension path": {"run", "--extension-dir"},
		"unknown flag":           {"run", "--other", "request"},
	}
	for name, arguments := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(arguments)

			require.Error(t, err)
		})
	}
}
