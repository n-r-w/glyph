// Package settings loads and validates Host settings.
package settings

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/n-r-w/glyph/host/internal/domain/pluginid"
)

// ReasoningChoice is one configured model reasoning choice.
type ReasoningChoice string

const (
	// ReasoningChoiceOff disables model reasoning.
	ReasoningChoiceOff ReasoningChoice = "off"
	// ReasoningChoiceOn enables model reasoning with the provider default.
	ReasoningChoiceOn ReasoningChoice = "on"
	// ReasoningChoiceMinimal requests minimal model reasoning.
	ReasoningChoiceMinimal ReasoningChoice = "minimal"
	// ReasoningChoiceLow requests low model reasoning.
	ReasoningChoiceLow ReasoningChoice = "low"
	// ReasoningChoiceMedium requests medium model reasoning.
	ReasoningChoiceMedium ReasoningChoice = "medium"
	// ReasoningChoiceHigh requests high model reasoning.
	ReasoningChoiceHigh ReasoningChoice = "high"
	// ReasoningChoiceXHigh requests extra-high model reasoning.
	ReasoningChoiceXHigh ReasoningChoice = "xhigh"
	// ReasoningChoiceMax requests maximum model reasoning.
	ReasoningChoiceMax ReasoningChoice = "max"
)

// ProviderType identifies one configured provider protocol.
type ProviderType string

const (
	// ProviderTypeOpenAICodex uses Glyph's OpenAI Codex integration.
	ProviderTypeOpenAICodex ProviderType = "openai-codex"
	// ProviderTypeOpenAICompatible uses an OpenAI-compatible HTTP API.
	ProviderTypeOpenAICompatible ProviderType = "openai-compatible"
)

// API identifies an OpenAI-compatible wire API.
type API string

const (
	// APIChatCompletions uses the Chat Completions API.
	APIChatCompletions API = "chat-completions"
	// APIResponses uses the Responses API.
	APIResponses API = "responses"
)

// ReasoningWireFormat identifies a provider-driver reasoning representation.
type ReasoningWireFormat string

const (
	// ReasoningWireFormatOpenAIResponses uses OpenAI Responses reasoning fields.
	ReasoningWireFormatOpenAIResponses ReasoningWireFormat = "openai-responses"
)

// APIKey identifies one configured API-key source.
type APIKey struct {
	Literal     *string
	Environment *string
	Credential  *string
}

// Reasoning contains one validated model reasoning configuration.
type Reasoning struct {
	Supported        bool
	Choices          []ReasoningChoice
	Default          ReasoningChoice
	CompatibilityKey string
	WireFormat       ReasoningWireFormat
}

// Model contains one configured model and its reasoning configuration.
type Model struct {
	ID        string
	API       API
	Reasoning Reasoning
}

// Provider contains one validated provider instance.
type Provider struct {
	Type    ProviderType
	BaseURL string
	API     API
	APIKey  *APIKey
	Models  []Model
}

// Settings contains the validated startup model and UI selection.
type Settings struct {
	DefaultProvider string
	DefaultModel    string
	Providers       map[string]Provider
	ActiveUI        string
}

// Service loads one settings file.
type Service struct {
	path string
}

// New creates a settings loader for one file.
func New(path string) *Service {
	return &Service{path: path}
}

// settingsFile is the strict YAML representation owned by Host persistence.
type settingsFile struct {
	DefaultProvider string                  `yaml:"defaultProvider"`
	DefaultModel    string                  `yaml:"defaultModel"`
	Providers       map[string]providerFile `yaml:"providers"`
	ActiveUI        *string                 `yaml:"activeUI"`
}

type providerFile struct {
	Type    ProviderType `yaml:"type"`
	BaseURL string       `yaml:"baseURL"`
	API     API          `yaml:"api"`
	APIKey  *apiKeyFile  `yaml:"apiKey"`
	Models  []modelFile  `yaml:"models"`
}

type apiKeyFile struct {
	Literal     *string `yaml:"literal"`
	Environment *string `yaml:"environment"`
	Credential  *string `yaml:"credential"`
}

type modelFile struct {
	ID        string        `yaml:"id"`
	API       API           `yaml:"api"`
	Reasoning reasoningFile `yaml:"reasoning"`
}

type reasoningFile struct {
	Supported        bool                `yaml:"supported"`
	Choices          []ReasoningChoice   `yaml:"choices"`
	Default          ReasoningChoice     `yaml:"default"`
	CompatibilityKey *string             `yaml:"compatibilityKey"`
	WireFormat       ReasoningWireFormat `yaml:"wireFormat"`
}

// Load parses and validates the configured settings file.
func (s *Service) Load() (Settings, error) {
	file, err := os.Open(filepath.Clean(s.path))
	if err != nil {
		return Settings{}, fmt.Errorf("open Glyph settings: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var decoded settingsFile
	if err = decoder.Decode(&decoded); err != nil {
		return Settings{}, errors.New("decode Glyph settings: invalid YAML or unknown field")
	}
	var extra any
	trailingErr := decoder.Decode(&extra)
	if !errors.Is(trailingErr, io.EOF) {
		if trailingErr == nil {
			return Settings{}, errors.New("decode Glyph settings: multiple YAML documents are not allowed")
		}
		return Settings{}, errors.New("decode trailing Glyph settings: invalid YAML")
	}
	return validate(decoded)
}

// validate applies the closed provider and startup selection rules.
func validate(decoded settingsFile) (Settings, error) {
	if err := validateDefaults(decoded); err != nil {
		return Settings{}, err
	}
	providers, err := validateProviders(decoded.Providers)
	if err != nil {
		return Settings{}, err
	}
	defaultProvider, found := providers[decoded.DefaultProvider]
	if !found {
		return Settings{}, errors.New("defaultProvider must identify a configured provider")
	}
	_, found = findModel(defaultProvider.Models, decoded.DefaultModel)
	if !found {
		return Settings{}, errors.New("defaultModel must identify a model on defaultProvider")
	}
	activeUI, err := validateActiveUI(decoded.ActiveUI)
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		DefaultProvider: decoded.DefaultProvider,
		DefaultModel:    decoded.DefaultModel,
		Providers:       providers,
		ActiveUI:        activeUI,
	}, nil
}

func validateDefaults(decoded settingsFile) error {
	if err := validateIdentifier("defaultProvider", decoded.DefaultProvider); err != nil {
		return err
	}
	if err := validateIdentifier("defaultModel", decoded.DefaultModel); err != nil {
		return err
	}
	if len(decoded.Providers) == 0 {
		return errors.New("providers must contain configured provider instances")
	}
	return nil
}

func validateProviders(configured map[string]providerFile) (map[string]Provider, error) {
	providers := make(map[string]Provider, len(configured))
	codexCount := 0
	for providerID, providerFile := range configured {
		if err := validateIdentifier("provider ID", providerID); err != nil {
			return nil, err
		}
		provider, err := validateProvider(providerID, providerFile)
		if err != nil {
			return nil, err
		}
		if provider.Type == ProviderTypeOpenAICodex {
			codexCount++
			if providerID != "openai-codex" {
				return nil, errors.New("openai-codex provider must use provider ID openai-codex")
			}
		}
		providers[providerID] = provider
	}
	if codexCount != 1 {
		return nil, errors.New("providers must contain exactly one openai-codex instance")
	}
	return providers, nil
}

func findModel(models []Model, modelID string) (Model, bool) {
	for _, configured := range models {
		if configured.ID == modelID {
			return configured, true
		}
	}
	return Model{ID: "", API: "", Reasoning: Reasoning{
		Supported: false, Choices: nil, Default: "", CompatibilityKey: "", WireFormat: "",
	}}, false
}

func validateActiveUI(configured *string) (string, error) {
	if configured == nil {
		return "", nil
	}
	activeUI := pluginid.Normalize(*configured)
	if activeUI == "" {
		return "", errors.New("activeUI must have a nonempty normalized plugin ID")
	}
	return activeUI, nil
}

func validateProvider(providerID string, configured providerFile) (Provider, error) {
	if configured.Type != ProviderTypeOpenAICodex && configured.Type != ProviderTypeOpenAICompatible {
		return Provider{}, fmt.Errorf("provider %q has unsupported type %q", providerID, configured.Type)
	}
	if len(configured.Models) == 0 {
		return Provider{}, fmt.Errorf("provider %q must contain models", providerID)
	}

	provider := Provider{
		Type: configured.Type, BaseURL: configured.BaseURL, API: configured.API, APIKey: nil,
		Models: make([]Model, 0, len(configured.Models)),
	}
	switch configured.Type {
	case ProviderTypeOpenAICodex:
		if configured.BaseURL != "" || configured.API != "" || configured.APIKey != nil {
			return Provider{}, fmt.Errorf("provider %q has fields that are not valid for openai-codex", providerID)
		}
	case ProviderTypeOpenAICompatible:
		apiKey, err := validateCompatibleProvider(providerID, configured)
		if err != nil {
			return Provider{}, err
		}
		provider.APIKey = apiKey
	}

	seenModels := make(map[string]struct{}, len(configured.Models))
	for _, configuredModel := range configured.Models {
		model, err := validateModel(providerID, configured.Type, configured.API, configuredModel)
		if err != nil {
			return Provider{}, err
		}
		if _, duplicate := seenModels[model.ID]; duplicate {
			return Provider{}, fmt.Errorf("provider %q has duplicate model ID %q", providerID, model.ID)
		}
		seenModels[model.ID] = struct{}{}
		provider.Models = append(provider.Models, model)
	}
	return provider, nil
}

func validateCompatibleProvider(providerID string, configured providerFile) (*APIKey, error) {
	if err := validateBaseURL(configured.BaseURL); err != nil {
		return nil, fmt.Errorf("provider %q: %w", providerID, err)
	}
	if !isAPISupported(configured.API) {
		return nil, fmt.Errorf("provider %q has unsupported API %q", providerID, configured.API)
	}
	return validateAPIKey(providerID, configured.APIKey)
}

// validateModel validates one model and resolves its effective API for reasoning validation.
func validateModel(providerID string, providerType ProviderType, providerAPI API, configured modelFile) (Model, error) {
	if err := validateIdentifier("model ID", configured.ID); err != nil {
		return Model{}, fmt.Errorf("provider %q: %w", providerID, err)
	}
	api := providerAPI
	if providerType == ProviderTypeOpenAICodex {
		api = APIResponses
	}
	if configured.API != "" {
		if providerType != ProviderTypeOpenAICompatible {
			return Model{}, fmt.Errorf("provider %q model %q cannot override API", providerID, configured.ID)
		}
		if !isAPISupported(configured.API) {
			return Model{}, fmt.Errorf("provider %q model %q has unsupported API %q", providerID, configured.ID, configured.API)
		}
		api = configured.API
	}
	reasoning, err := validateReasoning(providerID, configured.ID, api, configured.Reasoning)
	if err != nil {
		return Model{}, err
	}
	return Model{ID: configured.ID, API: configured.API, Reasoning: reasoning}, nil
}

// validateReasoning validates one closed capability shape and its provider wire format.
//
//nolint:gocyclo // The flat validation mirrors the closed capability and wire-format combinations.
func validateReasoning(providerID, modelID string, api API, configured reasoningFile) (Reasoning, error) {
	choices := append([]ReasoningChoice(nil), configured.Choices...)
	seen := make(map[ReasoningChoice]struct{}, len(choices))
	for _, choice := range choices {
		if !isReasoningChoiceSupported(choice) {
			return Reasoning{}, fmt.Errorf(
				"provider %q model %q has unsupported reasoning choice %q", providerID, modelID, choice,
			)
		}
		if _, duplicate := seen[choice]; duplicate {
			return Reasoning{}, fmt.Errorf("provider %q model %q has duplicate reasoning choice %q", providerID, modelID, choice)
		}
		seen[choice] = struct{}{}
	}
	if len(choices) == 0 || !supportsReasoningChoice(choices, configured.Default) {
		return Reasoning{}, fmt.Errorf(
			"provider %q model %q reasoning default must be listed in choices", providerID, modelID,
		)
	}
	key := ""
	if configured.CompatibilityKey != nil {
		key = *configured.CompatibilityKey
		if key == "" || key != strings.TrimSpace(key) {
			return Reasoning{}, fmt.Errorf(
				"provider %q model %q reasoning compatibilityKey must be nonempty without surrounding whitespace",
				providerID, modelID,
			)
		}
	}
	if !configured.Supported {
		invalidShape := len(choices) != 1 || choices[0] != ReasoningChoiceOff ||
			configured.Default != ReasoningChoiceOff || key != "" || configured.WireFormat != ""
		if invalidShape {
			return Reasoning{}, fmt.Errorf(
				"provider %q model %q has contradictory non-reasoning capabilities", providerID, modelID,
			)
		}
		return Reasoning{
			Supported: false, Choices: choices, Default: configured.Default,
			CompatibilityKey: "", WireFormat: "",
		}, nil
	}
	if configured.WireFormat == "" {
		return Reasoning{}, fmt.Errorf("provider %q model %q reasoning requires wireFormat", providerID, modelID)
	}
	if err := validateReasoningShape(choices, configured.Default); err != nil {
		return Reasoning{}, fmt.Errorf("provider %q model %q: %w", providerID, modelID, err)
	}
	if !wireFormatMatchesAPI(configured.WireFormat, api) {
		return Reasoning{}, fmt.Errorf("provider %q model %q reasoning wireFormat does not match API", providerID, modelID)
	}
	return Reasoning{
		Supported: true, Choices: choices, Default: configured.Default,
		CompatibilityKey: key, WireFormat: configured.WireFormat,
	}, nil
}

// validateReasoningShape accepts fixed, toggle, and effort reasoning shapes.
func validateReasoningShape(choices []ReasoningChoice, defaultChoice ReasoningChoice) error {
	if len(choices) == 1 && choices[0] == ReasoningChoiceOn && defaultChoice == ReasoningChoiceOn {
		return nil
	}
	isToggle := len(choices) == 2 && supportsReasoningChoice(choices, ReasoningChoiceOff) &&
		supportsReasoningChoice(choices, ReasoningChoiceOn)
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

// wireFormatMatchesAPI checks the closed wire-format and API combinations.
func wireFormatMatchesAPI(format ReasoningWireFormat, api API) bool {
	switch format {
	case ReasoningWireFormatOpenAIResponses:
		return api == APIResponses
	default:
		return false
	}
}

//nolint:nilnil // A nil key and nil error represent an omitted optional API-key source.
func validateAPIKey(providerID string, configured *apiKeyFile) (*APIKey, error) {
	if configured == nil {
		return nil, nil
	}
	count := 0
	for _, value := range []*string{configured.Literal, configured.Environment, configured.Credential} {
		if value != nil {
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("provider %q apiKey must contain exactly one source", providerID)
	}
	if configured.Literal != nil && *configured.Literal == "" {
		return nil, fmt.Errorf("provider %q apiKey literal must not be empty", providerID)
	}
	if configured.Environment != nil {
		if err := validateIdentifier("apiKey environment", *configured.Environment); err != nil {
			return nil, fmt.Errorf("provider %q: %w", providerID, err)
		}
	}
	if configured.Credential != nil {
		if err := validateIdentifier("apiKey credential", *configured.Credential); err != nil {
			return nil, fmt.Errorf("provider %q: %w", providerID, err)
		}
	}
	return &APIKey{
		Literal: configured.Literal, Environment: configured.Environment, Credential: configured.Credential,
	}, nil
}

func validateBaseURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("baseURL must be an absolute HTTP or HTTPS URL")
	}
	return nil
}

func validateIdentifier(name, value string) error {
	if value == "" || value != strings.TrimSpace(value) {
		return fmt.Errorf("%s must be nonempty without surrounding whitespace", name)
	}
	return nil
}

func isAPISupported(api API) bool {
	return api == APIChatCompletions || api == APIResponses
}

func supportsReasoningChoice(levels []ReasoningChoice, target ReasoningChoice) bool {
	for _, level := range levels {
		if level == target {
			return true
		}
	}
	return false
}

// isReasoningChoiceSupported recognizes the complete configured reasoning-choice set.
func isReasoningChoiceSupported(level ReasoningChoice) bool {
	switch level {
	case ReasoningChoiceOff,
		ReasoningChoiceOn,
		ReasoningChoiceMinimal,
		ReasoningChoiceLow,
		ReasoningChoiceMedium,
		ReasoningChoiceHigh,
		ReasoningChoiceXHigh,
		ReasoningChoiceMax:
		return true
	default:
		return false
	}
}
