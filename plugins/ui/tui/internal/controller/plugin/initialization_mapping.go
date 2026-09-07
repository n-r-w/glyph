package plugin

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapInitialization validates the complete first frame before the TUI takes terminal ownership.
func mapInitialization(initialization *uiv1.Initialization) (Initialization, error) {
	if !initialization.HasSelectedUiId() {
		return Initialization{}, errors.New("selected UI ID is required")
	}
	if !initialization.HasAvailability() {
		return Initialization{}, errors.New("availability is required")
	}
	availability, err := mapAvailability(initialization.GetAvailability())
	if err != nil {
		return Initialization{}, err
	}
	selection, err := mapModelSelection(initialization.GetModelSelection())
	if err != nil {
		return Initialization{}, err
	}
	startup, err := mapInitializationStartup(initialization.GetStartupContent())
	if err != nil {
		return Initialization{}, err
	}
	if extensionErr := validateInitializationExtensions(initialization.GetExtensions()); extensionErr != nil {
		return Initialization{}, extensionErr
	}
	models, err := mapInitializationModels(initialization.GetModels())
	if err != nil {
		return Initialization{}, err
	}
	sessionInfo, err := mapSessionInfo(initialization.GetSessionInfo())
	if err != nil {
		return Initialization{}, err
	}
	return Initialization{
		Availability: availability, Startup: startup,
		Models: models, Selection: selection, Session: sessionInfo,
	}, nil
}

// mapInitializationStartup validates and maps startup lines.
func mapInitializationStartup(contents []*uiv1.StartupContent) ([]Transcript, error) {
	return lo.MapErr(contents, func(content *uiv1.StartupContent, _ int) (Transcript, error) {
		if !content.HasSeverity() {
			return Transcript{}, errors.New("startup content severity is required")
		}
		if !content.HasText() {
			return Transcript{}, errors.New("startup content text is required")
		}
		var kind TranscriptKind
		switch content.GetSeverity() {
		case uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION:
			kind = TranscriptInformation
		case uiv1.ContentSeverity_CONTENT_SEVERITY_ERROR:
			kind = TranscriptError
		case uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING:
			kind = TranscriptWarning
		case uiv1.ContentSeverity_CONTENT_SEVERITY_UNSPECIFIED:
			return Transcript{}, errors.New("startup content severity is unspecified")
		default:
			return Transcript{}, fmt.Errorf("unknown startup content severity %d", content.GetSeverity())
		}
		return Transcript{
			Kind:     kind,
			Text:     mo.Some(content.GetText()),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		}, nil
	})
}

// validateInitializationExtensions checks required identity fields before startup admission.
func validateInitializationExtensions(extensions []*uiv1.ExtensionAvailability) error {
	for _, extension := range extensions {
		if !extension.HasPluginId() {
			return errors.New("extension plugin ID is required")
		}
		if !extension.HasPath() {
			return errors.New("extension path is required")
		}
	}
	return nil
}

// mapInitializationModels validates and maps configured models.
func mapInitializationModels(models []*uiv1.ConfiguredModel) ([]ConfiguredModel, error) {
	return lo.MapErr(models, func(configured *uiv1.ConfiguredModel, _ int) (ConfiguredModel, error) {
		if !configured.HasProviderId() {
			return ConfiguredModel{}, errors.New("configured model provider ID is required")
		}
		if !configured.HasModelId() {
			return ConfiguredModel{}, errors.New("configured model ID is required")
		}
		reasoning := configured.GetReasoning()
		if reasoning == nil {
			return ConfiguredModel{}, errors.New("model reasoning capabilities are missing")
		}
		if !reasoning.HasSupported() {
			return ConfiguredModel{}, errors.New("model reasoning support is required")
		}
		if !reasoning.HasDefaultChoice() {
			return ConfiguredModel{}, errors.New("model reasoning default choice is required")
		}
		choices, err := lo.MapErr(
			reasoning.GetChoices(),
			func(choice uiv1.ReasoningChoice, _ int) (ReasoningChoice, error) {
				return mapReasoningChoice(choice)
			},
		)
		if err != nil {
			return ConfiguredModel{}, err
		}
		defaultChoice, err := mapReasoningChoice(reasoning.GetDefaultChoice())
		if err != nil {
			return ConfiguredModel{}, err
		}
		return ConfiguredModel{
			ProviderID: configured.GetProviderId(),
			ModelID:    configured.GetModelId(),
			Reasoning: ReasoningCapabilities{
				Supported: reasoning.GetSupported(),
				Choices:   choices,
				Default:   defaultChoice,
			},
		}, nil
	})
}

// mapModelSelection validates one Host-confirmed selection.
func mapModelSelection(selection *uiv1.ModelSelection) (ModelSelection, error) {
	if selection == nil {
		return ModelSelection{}, errors.New("model selection is invalid")
	}
	if !selection.HasProviderId() {
		return ModelSelection{}, errors.New("model selection provider ID is required")
	}
	if !selection.HasModelId() {
		return ModelSelection{}, errors.New("model selection model ID is required")
	}
	if !selection.HasReasoningChoice() {
		return ModelSelection{}, errors.New("model selection reasoning choice is required")
	}
	if selection.GetProviderId() == "" || selection.GetModelId() == "" {
		return ModelSelection{}, errors.New("model selection is invalid")
	}
	level, err := mapReasoningChoice(selection.GetReasoningChoice())
	if err != nil {
		return ModelSelection{}, err
	}
	return ModelSelection{
		ProviderID:      selection.GetProviderId(),
		ModelID:         selection.GetModelId(),
		ReasoningChoice: level,
	}, nil
}

// mapReasoningChoice validates the complete public reasoning enum.
func mapReasoningChoice(level uiv1.ReasoningChoice) (ReasoningChoice, error) {
	switch level {
	case uiv1.ReasoningChoice_REASONING_CHOICE_OFF:
		return ReasoningChoiceOff, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_ON:
		return ReasoningChoiceOn, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL:
		return ReasoningChoiceMinimal, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_LOW:
		return ReasoningChoiceLow, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM:
		return ReasoningChoiceMedium, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_HIGH:
		return ReasoningChoiceHigh, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH:
		return ReasoningChoiceXHigh, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MAX:
		return ReasoningChoiceMax, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED:
		return ReasoningChoiceUnspecified, errors.New("reasoning choice is unspecified")
	default:
		return ReasoningChoiceUnspecified, fmt.Errorf("unknown reasoning choice %d", level)
	}
}
