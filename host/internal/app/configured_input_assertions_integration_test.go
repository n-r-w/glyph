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
	const (
		emptyConfiguredContent     = "empty content"
		missingConfiguredPayload   = "missing payload"
		emptyConfiguredText        = "empty text"
		missingConfiguredMediaType = "missing media type"
		emptyConfiguredImageData   = "empty image data"
		assistantConfiguredImage   = "assistant image"
		unknownConfiguredRole      = "unknown role"
		unsupportedConfiguredImage = "unsupported image"
	)
	expected := map[string]string{
		emptyConfiguredContent:     "configured model message 0 requires at least one content block",
		missingConfiguredPayload:   "configured model message 0 content 0: content payload is required",
		emptyConfiguredText:        "configured model message 0 content 0: nonempty text is required",
		missingConfiguredMediaType: "configured model message 0 content 1: image media type and data are required",
		emptyConfiguredImageData:   "configured model message 0 content 1: image media type and data are required",
		assistantConfiguredImage:   "configured model message 0 content 1: assistant content must be text",
		unknownConfiguredRole:      "configured model message 0 role 99 is unknown",
		unsupportedConfiguredImage: "model gpt-active does not accept image input at message 0 content 1",
	}
	require.Len(t, failures, len(expected))
	for name, cause := range expected {
		assert.Contains(t, failures[name], cause, name)
	}
}
