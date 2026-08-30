package codex

import (
	"encoding/json/v2"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestUserMessageInputRejectsMissingSelectedPayload verifies selected content requires its matching payload.
func TestUserMessageInputRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	_, err := userMessageInput(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentText, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.EqualError(t, err, "codex text content 0 has no text")

	_, err = userMessageInput(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.EqualError(t, err, "codex image media type and data are required")
}

// assertOutputContents verifies ordered encoding and selected-payload validation for one output format.
func assertOutputContents[T any](
	t *testing.T,
	convert func([]tool.ResultContent) (T, error),
	invalid tool.ResultContent,
	wantErr string,
) {
	t.Helper()
	contents, err := convert([]tool.ResultContent{
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

	_, err = convert([]tool.ResultContent{invalid})
	require.EqualError(t, err, wantErr)
}

// TestFunctionOutputContentsPreservesTextImageOrder verifies Codex typed output encoding.
func TestFunctionOutputContentsPreservesTextImageOrder(t *testing.T) {
	t.Parallel()
	assertOutputContents(
		t,
		functionOutputContents,
		tool.ResultContent{Kind: tool.ResultContentText, Text: mo.None[string](), Image: mo.None[tool.ResultImage]()},
		"tool result text 0 has no text",
	)
}

// TestCustomOutputContentsPreservesTextImageOrder verifies constrained-tool output encoding.
func TestCustomOutputContentsPreservesTextImageOrder(t *testing.T) {
	t.Parallel()
	assertOutputContents(
		t,
		customOutputContents,
		tool.ResultContent{Kind: tool.ResultContentImage, Text: mo.None[string](), Image: mo.None[tool.ResultImage]()},
		"tool result image 0 has no image",
	)
}
