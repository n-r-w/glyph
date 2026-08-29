package plugin

import (
	"errors"
	"fmt"

	"slices"

	"github.com/samber/lo"
	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// mapInitialization validates the complete first frame before the TUI takes terminal ownership.
func mapInitialization(initialization *uiv1.Initialization) (presentationdomain.Event, error) {
	if !initialization.HasSelectedUiId() {
		return presentationdomain.Event{}, errors.New("selected UI ID is required")
	}
	if !initialization.HasAvailability() {
		return presentationdomain.Event{}, errors.New("availability is required")
	}
	availability, err := mapAvailability(initialization.GetAvailability())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	selection, err := mapModelSelection(initialization.GetModelSelection())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	startup, err := mapInitializationStartup(initialization.GetStartupContent())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	extensions, err := mapInitializationExtensions(initialization.GetExtensions())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	models, err := mapInitializationModels(initialization.GetModels())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	sessionInfo, err := mapSessionInfo(initialization.GetSessionInfo())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	event := presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInitialization,
		Startup:              startup,
		Extensions:           extensions,
		Availability:         mo.Some(availability),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               models,
		ModelSelection:       mo.Some(selection),
		SessionInfo:          mo.Some(sessionInfo),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	}
	return event, nil
}

// mapInitializationStartup validates and maps startup lines.
func mapInitializationStartup(contents []*uiv1.StartupContent) ([]presentationdomain.Line, error) {
	return lo.MapErr(contents, func(content *uiv1.StartupContent, _ int) (presentationdomain.Line, error) {
		if !content.HasSeverity() {
			return presentationdomain.Line{}, errors.New("startup content severity is required")
		}
		if !content.HasText() {
			return presentationdomain.Line{}, errors.New("startup content text is required")
		}
		var kind presentationdomain.LineKind
		switch content.GetSeverity() {
		case uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION:
			kind = presentationdomain.LineInformation
		case uiv1.ContentSeverity_CONTENT_SEVERITY_ERROR:
			kind = presentationdomain.LineError
		case uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING:
			kind = presentationdomain.LineWarning
		case uiv1.ContentSeverity_CONTENT_SEVERITY_UNSPECIFIED:
			return presentationdomain.Line{}, errors.New("startup content severity is unspecified")
		default:
			return presentationdomain.Line{}, fmt.Errorf("unknown startup content severity %d", content.GetSeverity())
		}
		return presentationdomain.Line{
			Kind:     kind,
			Text:     mo.Some(content.GetText()),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		}, nil
	})
}

// mapInitializationExtensions validates and maps extension availability.
func mapInitializationExtensions(extensions []*uiv1.ExtensionAvailability) ([]presentationdomain.Extension, error) {
	return lo.MapErr(extensions, func(extension *uiv1.ExtensionAvailability, _ int) (presentationdomain.Extension, error) {
		if !extension.HasPluginId() {
			return presentationdomain.Extension{}, errors.New("extension plugin ID is required")
		}
		if !extension.HasPath() {
			return presentationdomain.Extension{}, errors.New("extension path is required")
		}
		return presentationdomain.Extension{
			ID:    extension.GetPluginId(),
			Path:  extension.GetPath(),
			Tools: slices.Clone(extension.GetTools()),
		}, nil
	})
}

// mapInitializationModels validates and maps configured models.
func mapInitializationModels(models []*uiv1.ConfiguredModel) ([]presentationdomain.ConfiguredModel, error) {
	return lo.MapErr(models, func(configured *uiv1.ConfiguredModel, _ int) (presentationdomain.ConfiguredModel, error) {
		if !configured.HasProviderId() {
			return presentationdomain.ConfiguredModel{}, errors.New("configured model provider ID is required")
		}
		if !configured.HasModelId() {
			return presentationdomain.ConfiguredModel{}, errors.New("configured model ID is required")
		}
		reasoning := configured.GetReasoning()
		if reasoning == nil {
			return presentationdomain.ConfiguredModel{}, errors.New("model reasoning capabilities are missing")
		}
		if !reasoning.HasSupported() {
			return presentationdomain.ConfiguredModel{}, errors.New("model reasoning support is required")
		}
		if !reasoning.HasDefaultChoice() {
			return presentationdomain.ConfiguredModel{}, errors.New("model reasoning default choice is required")
		}
		choices, err := lo.MapErr(
			reasoning.GetChoices(),
			func(choice uiv1.ReasoningChoice, _ int) (presentationdomain.ReasoningChoice, error) {
				return mapReasoningChoice(choice)
			},
		)
		if err != nil {
			return presentationdomain.ConfiguredModel{}, err
		}
		defaultChoice, err := mapReasoningChoice(reasoning.GetDefaultChoice())
		if err != nil {
			return presentationdomain.ConfiguredModel{}, err
		}
		return presentationdomain.ConfiguredModel{
			ProviderID: configured.GetProviderId(),
			ModelID:    configured.GetModelId(),
			Reasoning: presentationdomain.ReasoningCapabilities{
				Supported: reasoning.GetSupported(),
				Choices:   choices,
				Default:   defaultChoice,
			},
		}, nil
	})
}

// mapModelSelection validates one Host-confirmed selection.
func mapModelSelection(selection *uiv1.ModelSelection) (presentationdomain.ModelSelection, error) {
	if selection == nil {
		return presentationdomain.ModelSelection{}, errors.New("model selection is invalid")
	}
	if !selection.HasProviderId() {
		return presentationdomain.ModelSelection{}, errors.New("model selection provider ID is required")
	}
	if !selection.HasModelId() {
		return presentationdomain.ModelSelection{}, errors.New("model selection model ID is required")
	}
	if !selection.HasReasoningChoice() {
		return presentationdomain.ModelSelection{}, errors.New("model selection reasoning choice is required")
	}
	if selection.GetProviderId() == "" || selection.GetModelId() == "" {
		return presentationdomain.ModelSelection{}, errors.New("model selection is invalid")
	}
	level, err := mapReasoningChoice(selection.GetReasoningChoice())
	if err != nil {
		return presentationdomain.ModelSelection{}, err
	}
	return presentationdomain.ModelSelection{
		ProviderID:      selection.GetProviderId(),
		ModelID:         selection.GetModelId(),
		ReasoningChoice: level,
	}, nil
}

// mapReasoningChoice validates the complete public reasoning enum.
func mapReasoningChoice(level uiv1.ReasoningChoice) (presentationdomain.ReasoningChoice, error) {
	switch level {
	case uiv1.ReasoningChoice_REASONING_CHOICE_OFF:
		return presentationdomain.ReasoningChoiceOff, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_ON:
		return presentationdomain.ReasoningChoiceOn, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL:
		return presentationdomain.ReasoningChoiceMinimal, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_LOW:
		return presentationdomain.ReasoningChoiceLow, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM:
		return presentationdomain.ReasoningChoiceMedium, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_HIGH:
		return presentationdomain.ReasoningChoiceHigh, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH:
		return presentationdomain.ReasoningChoiceXHigh, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MAX:
		return presentationdomain.ReasoningChoiceMax, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED:
		return presentationdomain.ReasoningChoiceUnspecified, errors.New("reasoning choice is unspecified")
	default:
		return presentationdomain.ReasoningChoiceUnspecified, fmt.Errorf("unknown reasoning choice %d", level)
	}
}
