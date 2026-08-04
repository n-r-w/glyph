package pluginid

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeAppliesSharedPluginIDRules verifies catalog and configured IDs use one comparison form.
func TestNormalizeAppliesSharedPluginIDRules(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "case and separators", input: "  My_UI--Plugin  ", expected: "my-ui-plugin"},
		{name: "unicode letters", input: "Плагин_UI", expected: "плагин-ui"},
		{name: "only separators", input: " _- ", expected: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.expected, Normalize(testCase.input))
		})
	}
}
