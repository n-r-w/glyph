package modelexecution

import (
	"fmt"
	"slices"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// validateInputModalities rejects user images for a model that declares no image input.
func validateInputModalities(descriptor model.Descriptor, history []agent.HistoryEntry) error {
	if slices.Contains(descriptor.Input, model.InputModalityImage) {
		return nil
	}
	for messageIndex := range history {
		if user, present := history[messageIndex].User.Get(); present {
			for contentIndex, content := range user.Content {
				if content.Kind == model.InputContentImage {
					return fmt.Errorf(
						"model %s does not accept image input at message %d content %d",
						descriptor.Model, messageIndex, contentIndex,
					)
				}
			}
		}
	}
	return nil
}
