//go:build !integration

package model

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
)

// descriptorWithInput creates one descriptor with the given input modalities.
func descriptorWithInput(input ...InputModality) Descriptor {
	return Descriptor{
		Provider: "provider", Model: "model", Input: input, ContextWindow: 1, MaxTokens: 1,
		ReasoningCapabilities: ReasoningCapabilities{Supported: false, Choices: nil, Default: ""},
		ToolCapabilities: ToolCapabilities{
			StrictJSONSchema: false, Grammar: GrammarCapabilities{Lark: false, Regex: false},
		},
		Pricing: mo.None[Pricing](),
	}
}

// TestMessageValidateShape verifies accepted ordered blocks and each rejected block shape.
func TestMessageValidateShape(t *testing.T) {
	t.Parallel()
	// Arrange valid and malformed messages.
	tests := map[string]struct {
		message Message
		err     string
	}{
		"text and image": {
			message: Message{Content: []InputContent{TextContent("a"), ImageContent("image/png", []byte{1})}},
			err:     "",
		},
		"no blocks": {message: Message{Content: nil}, err: "at least one content block is required"},
		"empty text": {
			message: Message{Content: []InputContent{TextContent("")}},
			err:     "content 0: nonempty text is required",
		},
		"no media type": {
			message: Message{Content: []InputContent{TextContent("a"), ImageContent("", []byte{1})}},
			err:     "content 1: image media type and data are required",
		},
		"no image data": {
			message: Message{Content: []InputContent{ImageContent("image/png", nil)}},
			err:     "content 0: image media type and data are required",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Act by validating the message shape.
			err := test.message.ValidateShape()

			// Assert the accepted shape or the complete rejection cause.
			if test.err == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, test.err)
		})
	}
}

// TestDescriptorFindUnsupportedImage verifies image support follows the declared input modalities.
func TestDescriptorFindUnsupportedImage(t *testing.T) {
	t.Parallel()
	// Arrange a message with an image after text and both descriptor kinds.
	message := Message{Content: []InputContent{TextContent("a"), ImageContent("image/png", []byte{1})}}
	textOnly := descriptorWithInput(InputModalityText)
	withImages := descriptorWithInput(InputModalityText, InputModalityImage)

	// Act by searching each descriptor for an unsupported image.
	textOnlyIndex, textOnlyFound := textOnly.FindUnsupportedImage(message)
	_, imageFound := withImages.FindUnsupportedImage(message)
	_, textFound := textOnly.FindUnsupportedImage(TextMessage("a"))

	// Assert only the text-only descriptor rejects the image block.
	assert.True(t, textOnlyFound)
	assert.Equal(t, 1, textOnlyIndex)
	assert.False(t, imageFound)
	assert.False(t, textFound)
}
