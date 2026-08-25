package compatible

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestChatUserContentRejectsMissingSelectedPayload verifies selected content requires its matching payload.
func TestChatUserContentRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	_, err := chatUserContent(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentText, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.EqualError(t, err, "user text 0 has no text")

	_, err = chatUserContent(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.EqualError(t, err, "user image 0 requires media type and data")
}

// TestResponsesUserMessageRejectsMissingSelectedPayload verifies selected content requires its matching payload.
func TestResponsesUserMessageRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	_, err := responsesUserMessage(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentText, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.EqualError(t, err, "user text 0 has no text")

	_, err = responsesUserMessage(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.EqualError(t, err, "user image 0 requires media type and data")
}
