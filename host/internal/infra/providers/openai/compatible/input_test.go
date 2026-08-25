package compatible

import (
	"encoding/json"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestChatUserContentPreservesAbsentPayloadBehavior verifies Option absence keeps prior zero-value mapping and validation.
func TestChatUserContentPreservesAbsentPayloadBehavior(t *testing.T) {
	t.Parallel()

	content, err := chatUserContent(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentText, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.NoError(t, err)
	payload, err := json.Marshal(content)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"type":"text","text":""}]`, string(payload))

	_, err = chatUserContent(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	assert.EqualError(t, err, "user image 0 requires media type and data")
}

// TestResponsesUserMessagePreservesAbsentPayloadBehavior verifies Option absence keeps prior zero-value mapping and validation.
func TestResponsesUserMessagePreservesAbsentPayloadBehavior(t *testing.T) {
	t.Parallel()

	content, err := responsesUserMessage(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentText, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.NoError(t, err)
	payload, err := json.Marshal(content)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"message","role":"user","content":[{"type":"input_text","text":""}]}`, string(payload))

	_, err = responsesUserMessage(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	assert.EqualError(t, err, "user image 0 requires media type and data")
}
