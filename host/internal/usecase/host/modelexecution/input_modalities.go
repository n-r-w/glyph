package modelexecution

import (
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// validateInputModalities rejects user images for a model that declares no image input.
func validateInputModalities(descriptor model.Descriptor, history []agent.HistoryEntry) error {
	for messageIndex := range history {
		if user, present := history[messageIndex].User.Get(); present {
			if contentIndex, found := descriptor.FindUnsupportedImage(user); found {
				return fmt.Errorf(
					"model %s does not accept image input at message %d content %d",
					descriptor.Model, messageIndex, contentIndex,
				)
			}
		}
	}
	return nil
}
