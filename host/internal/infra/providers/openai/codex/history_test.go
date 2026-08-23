package codex

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestFunctionOutputContentsPreservesTextImageOrder verifies Codex typed output encoding.
func TestFunctionOutputContentsPreservesTextImageOrder(t *testing.T) {
	t.Parallel()

	contents, err := functionOutputContents([]tool.ResultContent{
		{Kind: tool.ResultContentText, Text: "first", Image: tool.ResultImage{MediaType: "", Data: nil}},
		{Kind: tool.ResultContentImage, Text: "", Image: tool.ResultImage{MediaType: "image/png", Data: []byte{0, 1, 2}}},
		{Kind: tool.ResultContentText, Text: "last", Image: tool.ResultImage{MediaType: "", Data: nil}},
	})
	require.NoError(t, err)

	payload, err := json.Marshal(contents)
	require.NoError(t, err)
	assert.JSONEq(t, `[
		{"type":"input_text","text":"first"},
		{"type":"input_image","image_url":"data:image/png;base64,AAEC"},
		{"type":"input_text","text":"last"}
	]`, string(payload))
}

// TestCustomOutputContentsPreservesTextImageOrder verifies constrained-tool output encoding.
func TestCustomOutputContentsPreservesTextImageOrder(t *testing.T) {
	t.Parallel()

	contents, err := customOutputContents([]tool.ResultContent{
		{Kind: tool.ResultContentText, Text: "first", Image: tool.ResultImage{MediaType: "", Data: nil}},
		{Kind: tool.ResultContentImage, Text: "", Image: tool.ResultImage{MediaType: "image/png", Data: []byte{0, 1, 2}}},
		{Kind: tool.ResultContentText, Text: "last", Image: tool.ResultImage{MediaType: "", Data: nil}},
	})
	require.NoError(t, err)

	payload, err := json.Marshal(contents)
	require.NoError(t, err)
	assert.JSONEq(t, `[
		{"type":"input_text","text":"first"},
		{"type":"input_image","image_url":"data:image/png;base64,AAEC"},
		{"type":"input_text","text":"last"}
	]`, string(payload))
}
