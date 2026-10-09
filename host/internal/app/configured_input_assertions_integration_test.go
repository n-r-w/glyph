//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertConfiguredInputErrors checks complete validation causes returned through the public operation.
func assertConfiguredInputErrors(t *testing.T, failures map[string]string) {
	t.Helper()
	expected := map[string]string{
		"empty content":      "configured model message 0 requires at least one content block",
		"missing payload":    "configured model message 0 content 0: content payload is required",
		"empty text":         "configured model message 0 content 0: nonempty text is required",
		"missing media type": "configured model message 0 content 1: image media type and data are required",
		"empty image data":   "configured model message 0 content 1: image media type and data are required",
		"assistant image":    "configured model message 0 content 1: assistant content must be text",
		"unknown role":       "configured model message 0 role 99 is unknown",
		"unsupported image":  "configured model gpt-active does not accept image input at message 0 content 1",
	}
	require.Len(t, failures, len(expected))
	for name, cause := range expected {
		assert.Contains(t, failures[name], cause, name)
	}
}
