package settings

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/mo"
)

// validate validates one provider-neutral capability shape and opaque provider format.
func (configured reasoningFile) validate(providerID, modelID string) (Reasoning, error) {
	supported, supportedPresent := configured.Supported.Get()
	if !supportedPresent {
		return Reasoning{}, fmt.Errorf("provider %q model %q reasoning requires supported", providerID, modelID)
	}
	choices, err := configured.validatedChoices(providerID, modelID)
	if err != nil {
		return Reasoning{}, err
	}
	key, err := configured.validatedCompatibilityKey(providerID, modelID)
	if err != nil {
		return Reasoning{}, err
	}
	if !supported {
		return configured.disabledReasoning(providerID, modelID, choices, key)
	}
	if shapeErr := validateReasoningShape(choices, configured.Default); shapeErr != nil {
		return Reasoning{}, fmt.Errorf("provider %q model %q: %w", providerID, modelID, shapeErr)
	}
	return Reasoning{
		Supported: true, Choices: choices, Default: configured.Default,
		CompatibilityKey: key, Format: configured.Format,
	}, nil
}

// validatedChoices validates and copies the configured closed reasoning choice set.
func (configured reasoningFile) validatedChoices(providerID, modelID string) ([]ReasoningChoice, error) {
	choices := slices.Clone(configured.Choices)
	seen := make(map[ReasoningChoice]struct{}, len(choices))
	for _, choice := range choices {
		if !choice.Supported() {
			return nil, fmt.Errorf(
				"provider %q model %q has unsupported reasoning choice %q", providerID, modelID, choice,
			)
		}
		if _, duplicate := seen[choice]; duplicate {
			return nil, fmt.Errorf(
				"provider %q model %q has duplicate reasoning choice %q",
				providerID,
				modelID,
				choice,
			)
		}
		seen[choice] = struct{}{}
	}
	if len(choices) == 0 || !slices.Contains(choices, configured.Default) {
		return nil, fmt.Errorf(
			"provider %q model %q reasoning default must be listed in choices", providerID, modelID,
		)
	}
	return choices, nil
}

// validatedCompatibilityKey validates the optional replay compatibility key.
func (configured reasoningFile) validatedCompatibilityKey(
	providerID string,
	modelID string,
) (mo.Option[string], error) {
	key := configured.CompatibilityKey
	if value, present := key.Get(); present && (value == "" || value != strings.TrimSpace(value)) {
		return mo.None[string](), fmt.Errorf(
			"provider %q model %q reasoning compatibilityKey must be nonempty without surrounding whitespace",
			providerID, modelID,
		)
	}
	return key, nil
}

// disabledReasoning validates and maps one disabled reasoning contract.
func (configured reasoningFile) disabledReasoning(
	providerID string,
	modelID string,
	choices []ReasoningChoice,
	key mo.Option[string],
) (Reasoning, error) {
	invalidShape := len(choices) != 1 || choices[0] != ReasoningChoiceOff ||
		configured.Default != ReasoningChoiceOff || key.IsSome() || configured.Format != ""
	if invalidShape {
		return Reasoning{}, fmt.Errorf(
			"provider %q model %q has contradictory non-reasoning capabilities", providerID, modelID,
		)
	}
	return Reasoning{
		Supported: false, Choices: choices, Default: configured.Default,
		CompatibilityKey: mo.None[string](), Format: "",
	}, nil
}

// validateReasoningShape accepts fixed, toggle, and effort reasoning shapes.
func validateReasoningShape(choices []ReasoningChoice, defaultChoice ReasoningChoice) error {
	if len(choices) == 1 && choices[0] == ReasoningChoiceOn && defaultChoice == ReasoningChoiceOn {
		return nil
	}
	isToggle := len(choices) == 2 && slices.Contains(choices, ReasoningChoiceOff) &&
		slices.Contains(choices, ReasoningChoiceOn)
	if isToggle {
		return nil
	}
	hasEffort := false
	for _, choice := range choices {
		if choice == ReasoningChoiceOn {
			return errors.New("effort reasoning cannot contain on")
		}
		if choice != ReasoningChoiceOff {
			hasEffort = true
		}
	}
	if !hasEffort {
		return errors.New("reasoning choices have an invalid capability shape")
	}
	return nil
}
