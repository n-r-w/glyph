package programmatic

import (
	"fmt"

	"github.com/samber/lo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

func mapConfiguredModels(descriptors []model.Descriptor) ([]*programmaticv1.ConfiguredModel, error) {
	return lo.MapErr(descriptors, func(descriptor model.Descriptor, _ int) (*programmaticv1.ConfiguredModel, error) {
		// inputModalities preserves the descriptor order in the public contract.
		inputModalities, err := lo.MapErr(
			descriptor.Input,
			func(modality model.InputModality, _ int) (programmaticv1.InputModality, error) {
				return mapInputModality(modality)
			},
		)
		if err != nil {
			return nil, err
		}
		choices, err := lo.MapErr(
			descriptor.ReasoningCapabilities.Choices,
			func(choice model.ReasoningChoice, _ int) (programmaticv1.ReasoningChoice, error) {
				return mapReasoningChoice(choice)
			},
		)
		if err != nil {
			return nil, err
		}
		defaultChoice, err := mapReasoningChoice(descriptor.ReasoningCapabilities.Default)
		if err != nil {
			return nil, err
		}
		capabilities := new(programmaticv1.ReasoningCapabilities)
		capabilities.SetSupported(descriptor.ReasoningCapabilities.Supported)
		capabilities.SetChoices(choices)
		capabilities.SetDefaultChoice(defaultChoice)
		configured := new(programmaticv1.ConfiguredModel)
		configured.SetProviderId(string(descriptor.Provider))
		configured.SetModelId(string(descriptor.Model))
		configured.SetReasoning(capabilities)
		configured.SetInputModalities(inputModalities)
		configured.SetContextWindow(descriptor.ContextWindow)
		configured.SetMaxTokens(descriptor.MaxTokens)
		return configured, nil
	})
}

// mapInputModality keeps the closed domain enum explicit at the public boundary.
func mapInputModality(modality model.InputModality) (programmaticv1.InputModality, error) {
	switch modality {
	case model.InputModalityText:
		return programmaticv1.InputModality_INPUT_MODALITY_TEXT, nil
	case model.InputModalityImage:
		return programmaticv1.InputModality_INPUT_MODALITY_IMAGE, nil
	default:
		return programmaticv1.InputModality_INPUT_MODALITY_UNSPECIFIED,
			fmt.Errorf("unsupported model input modality %q", modality)
	}
}

func mapModelSelection(selection model.Selection) (*programmaticv1.ModelSelection, error) {
	level, err := mapReasoningChoice(selection.ReasoningChoice)
	if err != nil {
		return nil, err
	}
	mapped := new(programmaticv1.ModelSelection)
	mapped.SetProviderId(string(selection.Provider))
	mapped.SetModelId(string(selection.Model))
	mapped.SetReasoningChoice(level)
	return mapped, nil
}

func mapReasoningChoice(level model.ReasoningChoice) (programmaticv1.ReasoningChoice, error) {
	switch level {
	case model.ReasoningChoiceOff:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF, nil
	case model.ReasoningChoiceOn:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_ON, nil
	case model.ReasoningChoiceMinimal:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_MINIMAL, nil
	case model.ReasoningChoiceLow:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_LOW, nil
	case model.ReasoningChoiceMedium:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_MEDIUM, nil
	case model.ReasoningChoiceHigh:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH, nil
	case model.ReasoningChoiceXHigh:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_XHIGH, nil
	case model.ReasoningChoiceMax:
		return programmaticv1.ReasoningChoice_REASONING_CHOICE_MAX, nil
	default:
		return 0, fmt.Errorf("map reasoning choice: unknown value %q", level)
	}
}
