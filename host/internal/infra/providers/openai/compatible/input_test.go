//go:build !integration

package compatible

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// assertMissingUserPayloadRejected verifies selected content requires its matching payload.
func assertMissingUserPayloadRejected[T any](t *testing.T, convert func(model.Message) (T, error)) {
	t.Helper()
	_, err := convert(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentText, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.EqualError(t, err, "user text 0 has no text")

	_, err = convert(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}})
	require.EqualError(t, err, "user image 0 requires media type and data")
}

// TestChatUserContentRejectsMissingSelectedPayload verifies chat-completion input validation.
func TestChatUserContentRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()
	assertMissingUserPayloadRejected(t, chatUserContent)
}

// TestResponsesUserMessageRejectsMissingSelectedPayload verifies Responses API input validation.
func TestResponsesUserMessageRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()
	assertMissingUserPayloadRejected(t, responsesUserMessage)
}
