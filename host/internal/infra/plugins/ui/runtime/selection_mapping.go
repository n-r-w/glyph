package runtime

import (
	"github.com/n-r-w/glyph/host/internal/domain/model"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapReasoningChoice converts a Host reasoning choice to the public contract.
func mapReasoningChoice(value model.ReasoningChoice) uiv1.ReasoningChoice {
	switch value {
	case model.ReasoningChoiceOff:
		return uiv1.ReasoningChoice_REASONING_CHOICE_OFF
	case model.ReasoningChoiceOn:
		return uiv1.ReasoningChoice_REASONING_CHOICE_ON
	case model.ReasoningChoiceMinimal:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL
	case model.ReasoningChoiceLow:
		return uiv1.ReasoningChoice_REASONING_CHOICE_LOW
	case model.ReasoningChoiceMedium:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM
	case model.ReasoningChoiceHigh:
		return uiv1.ReasoningChoice_REASONING_CHOICE_HIGH
	case model.ReasoningChoiceXHigh:
		return uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH
	case model.ReasoningChoiceMax:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MAX
	default:
		return uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED
	}
}

// mapSeverity converts startup severity to the public contract.
func mapSeverity(value ContentSeverity) uiv1.ContentSeverity {
	switch value {
	case ContentSeverityInformation:
		return uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION
	case ContentSeverityError:
		return uiv1.ContentSeverity_CONTENT_SEVERITY_ERROR
	case ContentSeverityWarning:
		return uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING
	default:
		return uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION
	}
}

// mapAvailability converts Host availability to the public contract.
func mapAvailability(value hostui.Availability) uiv1.Availability {
	return uiv1.Availability(value)
}
