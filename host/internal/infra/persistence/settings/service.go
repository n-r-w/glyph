// Package settings loads and validates Host settings.
package settings

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/mo"
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
	// ReasoningWireFormatOpenAIChatEffort uses Chat Completions effort and reasoning fields.
	ReasoningWireFormatOpenAIChatEffort ReasoningWireFormat = "openai-chat-effort"
	// ReasoningWireFormatOllamaOrnith uses fixed native Chat Completions reasoning without request control.
	ReasoningWireFormatOllamaOrnith ReasoningWireFormat = "ollama-ornith"
)

// APIKey identifies one configured API-key source.
type APIKey struct {
	Literal     mo.Option[string]
	Environment mo.Option[string]
	Credential  mo.Option[string]
}

// Reasoning contains one validated model reasoning configuration.
type Reasoning struct {
	Supported        bool
	Choices          []ReasoningChoice
	Default          ReasoningChoice
	CompatibilityKey mo.Option[string]
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
	APIKey  mo.Option[APIKey]
	Models  []Model
}

// Settings contains the validated startup model and UI selection.
type Settings struct {
	DefaultProvider string
	DefaultModel    string
	Providers       map[string]Provider
	ActiveUI        mo.Option[string]
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
	ActiveUI        mo.Option[string]       `yaml:"activeUI"`
}

type providerFile struct {
	Type    ProviderType          `yaml:"type"`
	BaseURL string                `yaml:"baseURL"`
	API     API                   `yaml:"api"`
	APIKey  mo.Option[apiKeyFile] `yaml:"apiKey"`
	Models  []modelFile           `yaml:"models"`
}

type apiKeyFile struct {
	Literal     mo.Option[string] `yaml:"literal"`
	Environment mo.Option[string] `yaml:"environment"`
	Credential  mo.Option[string] `yaml:"credential"`
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
	CompatibilityKey mo.Option[string]   `yaml:"compatibilityKey"`
	WireFormat       ReasoningWireFormat `yaml:"wireFormat"`
}

// The YAML methods preserve scalar presence before validation because mo.Option does not decode plain YAML scalars.
func (configured *settingsFile) UnmarshalYAML(node *yaml.Node) error {
	fields, err := decodeYAMLMapping(node, "defaultProvider", "defaultModel", "providers", "activeUI")
	if err != nil {
		return err
	}
	var decoded settingsFile
	if decodeErr := decodeYAMLField(fields, "defaultProvider", &decoded.DefaultProvider); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "defaultModel", &decoded.DefaultModel); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "providers", &decoded.Providers); decodeErr != nil {
		return decodeErr
	}
	activeUI, decodeErr := decodeYAMLOption[string](fields, "activeUI")
	if decodeErr != nil {
		return decodeErr
	}
	decoded.ActiveUI = activeUI
	*configured = decoded
	return nil
}

func (configured *providerFile) UnmarshalYAML(node *yaml.Node) error {
	fields, err := decodeYAMLMapping(node, "type", "baseURL", "api", "apiKey", "models")
	if err != nil {
		return err
	}
	var decoded providerFile
	apiKey, decodeErr := decodeYAMLOption[apiKeyFile](fields, "apiKey")
	if decodeErr != nil {
		return decodeErr
	}
	decoded.APIKey = apiKey
	if fieldErr := decodeYAMLField(fields, "type", &decoded.Type); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := decodeYAMLField(fields, "baseURL", &decoded.BaseURL); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := decodeYAMLField(fields, "api", &decoded.API); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := decodeYAMLField(fields, "models", &decoded.Models); fieldErr != nil {
		return fieldErr
	}
	*configured = decoded
	return nil
}

func (configured *apiKeyFile) UnmarshalYAML(node *yaml.Node) error {
	fields, err := decodeYAMLMapping(node, "literal", "environment", "credential")
	if err != nil {
		return err
	}
	var decoded apiKeyFile
	literal, decodeErr := decodeYAMLOption[string](fields, "literal")
	if decodeErr != nil {
		return decodeErr
	}
	environment, decodeErr := decodeYAMLOption[string](fields, "environment")
	if decodeErr != nil {
		return decodeErr
	}
	credential, decodeErr := decodeYAMLOption[string](fields, "credential")
	if decodeErr != nil {
		return decodeErr
	}
	decoded.Literal = literal
	decoded.Environment = environment
	decoded.Credential = credential
	*configured = decoded
	return nil
}

func (configured *modelFile) UnmarshalYAML(node *yaml.Node) error {
	fields, err := decodeYAMLMapping(node, "id", "api", "reasoning")
	if err != nil {
		return err
	}
	var decoded modelFile
	if decodeErr := decodeYAMLField(fields, "id", &decoded.ID); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "api", &decoded.API); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "reasoning", &decoded.Reasoning); decodeErr != nil {
		return decodeErr
	}
	*configured = decoded
	return nil
}

func (configured *reasoningFile) UnmarshalYAML(node *yaml.Node) error {
	fields, err := decodeYAMLMapping(node, "supported", "choices", "default", "compatibilityKey", "wireFormat")
	if err != nil {
		return err
	}
	var decoded reasoningFile
	if decodeErr := decodeYAMLField(fields, "supported", &decoded.Supported); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "choices", &decoded.Choices); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "default", &decoded.Default); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "wireFormat", &decoded.WireFormat); decodeErr != nil {
		return decodeErr
	}
	compatibilityKey, decodeErr := decodeYAMLOption[string](fields, "compatibilityKey")
	if decodeErr != nil {
		return decodeErr
	}
	decoded.CompatibilityKey = compatibilityKey
	*configured = decoded
	return nil
}

func decodeYAMLMapping(node *yaml.Node, fieldNames ...string) (map[string]yaml.Node, error) {
	const mappingPairSize = 2
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("expected YAML mapping")
	}
	known := make(map[string]struct{}, len(fieldNames))
	for _, field := range fieldNames {
		known[field] = struct{}{}
	}
	fields := make(map[string]yaml.Node, len(node.Content)/mappingPairSize)
	for index := 0; index < len(node.Content); index += mappingPairSize {
		field := node.Content[index].Value
		if _, found := known[field]; !found {
			return nil, fmt.Errorf("unknown YAML field %q", field)
		}
		if _, duplicate := fields[field]; duplicate {
			return nil, fmt.Errorf("duplicate YAML field %q", field)
		}
		fields[field] = *node.Content[index+1]
	}
	return fields, nil
}

func decodeYAMLField[T any](fields map[string]yaml.Node, field string, target *T) error {
	node, present := fields[field]
	if !present {
		return nil
	}
	return node.Decode(target)
}

func decodeYAMLOption[T any](fields map[string]yaml.Node, field string) (mo.Option[T], error) {
	node, present := fields[field]
	if !present || node.Tag == "!!null" {
		return mo.None[T](), nil
	}
	var value T
	if err := node.Decode(&value); err != nil {
		return mo.None[T](), err
	}
	return mo.Some(value), nil
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
	found = slices.ContainsFunc(defaultProvider.Models, func(configured Model) bool {
		return configured.ID == decoded.DefaultModel
	})
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
	for providerID := range configured {
		if err := validateIdentifier("provider ID", providerID); err != nil {
			return nil, err
		}
		provider, err := validateProvider(providerID, configured[providerID])
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

func validateActiveUI(configured mo.Option[string]) (mo.Option[string], error) {
	value, present := configured.Get()
	if !present {
		return mo.None[string](), nil
	}
	activeUI := pluginid.Normalize(value)
	if activeUI == "" {
		return mo.None[string](), errors.New("activeUI must have a nonempty normalized plugin ID")
	}
	return mo.Some(activeUI), nil
}

func validateProvider(providerID string, configured providerFile) (Provider, error) {
	if configured.Type != ProviderTypeOpenAICodex && configured.Type != ProviderTypeOpenAICompatible {
		return Provider{}, fmt.Errorf("provider %q has unsupported type %q", providerID, configured.Type)
	}
	if len(configured.Models) == 0 {
		return Provider{}, fmt.Errorf("provider %q must contain models", providerID)
	}

	provider := Provider{
		Type: configured.Type, BaseURL: configured.BaseURL, API: configured.API, APIKey: mo.None[APIKey](),
		Models: make([]Model, 0, len(configured.Models)),
	}
	switch configured.Type {
	case ProviderTypeOpenAICodex:
		if configured.BaseURL != "" || configured.API != "" || configured.APIKey.IsSome() {
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

func validateCompatibleProvider(providerID string, configured providerFile) (mo.Option[APIKey], error) {
	if err := validateBaseURL(configured.BaseURL); err != nil {
		return mo.None[APIKey](), fmt.Errorf("provider %q: %w", providerID, err)
	}
	if !isAPISupported(configured.API) {
		return mo.None[APIKey](), fmt.Errorf("provider %q has unsupported API %q", providerID, configured.API)
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
	choices := slices.Clone(configured.Choices)
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
	if len(choices) == 0 || !slices.Contains(choices, configured.Default) {
		return Reasoning{}, fmt.Errorf(
			"provider %q model %q reasoning default must be listed in choices", providerID, modelID,
		)
	}
	key := configured.CompatibilityKey
	if value, present := key.Get(); present {
		if value == "" || value != strings.TrimSpace(value) {
			return Reasoning{}, fmt.Errorf(
				"provider %q model %q reasoning compatibilityKey must be nonempty without surrounding whitespace",
				providerID, modelID,
			)
		}
	}
	if !configured.Supported {
		invalidShape := len(choices) != 1 || choices[0] != ReasoningChoiceOff ||
			configured.Default != ReasoningChoiceOff || key.IsSome() || configured.WireFormat != ""
		if invalidShape {
			return Reasoning{}, fmt.Errorf(
				"provider %q model %q has contradictory non-reasoning capabilities", providerID, modelID,
			)
		}
		return Reasoning{
			Supported: false, Choices: choices, Default: configured.Default,
			CompatibilityKey: mo.None[string](), WireFormat: "",
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
	if configured.WireFormat == ReasoningWireFormatOllamaOrnith &&
		(len(choices) != 1 || choices[0] != ReasoningChoiceOn || configured.Default != ReasoningChoiceOn) {
		return Reasoning{}, fmt.Errorf("provider %q model %q Ollama Ornith reasoning must be fixed on", providerID, modelID)
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

// wireFormatMatchesAPI checks the closed wire-format and API combinations.
func wireFormatMatchesAPI(format ReasoningWireFormat, api API) bool {
	switch format {
	case ReasoningWireFormatOpenAIResponses:
		return api == APIResponses
	case ReasoningWireFormatOpenAIChatEffort, ReasoningWireFormatOllamaOrnith:
		return api == APIChatCompletions
	default:
		return false
	}
}

func validateAPIKey(providerID string, configured mo.Option[apiKeyFile]) (mo.Option[APIKey], error) {
	configuredAPIKey, configuredPresent := configured.Get()
	if !configuredPresent {
		return mo.None[APIKey](), nil
	}
	apiKey := APIKey(configuredAPIKey)
	count := 0
	for _, present := range []bool{apiKey.Literal.IsSome(), apiKey.Environment.IsSome(), apiKey.Credential.IsSome()} {
		if present {
			count++
		}
	}
	if count != 1 {
		return mo.None[APIKey](), fmt.Errorf("provider %q apiKey must contain exactly one source", providerID)
	}
	if literal, sourcePresent := apiKey.Literal.Get(); sourcePresent && literal == "" {
		return mo.None[APIKey](), fmt.Errorf("provider %q apiKey literal must not be empty", providerID)
	}
	if environment, sourcePresent := apiKey.Environment.Get(); sourcePresent {
		if err := validateIdentifier("apiKey environment", environment); err != nil {
			return mo.None[APIKey](), fmt.Errorf("provider %q: %w", providerID, err)
		}
	}
	if credential, sourcePresent := apiKey.Credential.Get(); sourcePresent {
		if err := validateIdentifier("apiKey credential", credential); err != nil {
			return mo.None[APIKey](), fmt.Errorf("provider %q: %w", providerID, err)
		}
	}
	return mo.Some(apiKey), nil
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
