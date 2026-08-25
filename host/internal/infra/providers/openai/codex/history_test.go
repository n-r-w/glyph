package codex

import (
	"encoding/json"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestUserMessageInputPreservesAbsentPayloadBehavior verifies Option absence keeps prior zero-value mapping and validation.
func TestUserMessageInputPreservesAbsentPayloadBehavior(t *testing.T) {
	t.Parallel()

	textInput, err := userMessageInput(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentText, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.NoError(t, err)
	payload, err := json.Marshal(textInput)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"message","role":"user","content":[{"type":"input_text","text":""}]}`, string(payload))

	_, err = userMessageInput(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	assert.EqualError(t, err, "codex image media type and data are required")
}

// TestFunctionOutputContentsPreservesTextImageOrder verifies Codex typed output encoding.
func TestFunctionOutputContentsPreservesTextImageOrder(t *testing.T) {
	t.Parallel()

	contents, err := functionOutputContents([]tool.ResultContent{
		{Kind: tool.ResultContentText, Text: mo.Some("first"), Image: mo.None[tool.ResultImage]()},
		{Kind: tool.ResultContentImage, Text: mo.None[string](), Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{0, 1, 2}})},
		{Kind: tool.ResultContentText, Text: mo.Some("last"), Image: mo.None[tool.ResultImage]()},
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
		{Kind: tool.ResultContentText, Text: mo.Some("first"), Image: mo.None[tool.ResultImage]()},
		{Kind: tool.ResultContentImage, Text: mo.None[string](), Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{0, 1, 2}})},
		{Kind: tool.ResultContentText, Text: mo.Some("last"), Image: mo.None[tool.ResultImage]()},
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
