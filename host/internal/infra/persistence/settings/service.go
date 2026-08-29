// Package settings loads and validates Host settings.
package settings

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/mo"
	"go.yaml.in/yaml/v3"

	"github.com/n-r-w/glyph/host/internal/domain/model"
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

// APIKey identifies one configured API-key source.
type APIKey struct {
	// Literal contains an API key stored directly in settings.
	Literal mo.Option[string]
	// Environment contains the environment variable name that stores the API key.
	Environment mo.Option[string]
	// Credential contains the credential-store entry name that stores the API key.
	Credential mo.Option[string]
}

// Reasoning contains one validated model reasoning configuration.
type Reasoning struct {
	// Supported reports whether the model supports reasoning controls.
	Supported bool
	// Choices lists supported reasoning choices in configured order.
	Choices []ReasoningChoice
	// Default is the reasoning choice used without an explicit selection.
	Default ReasoningChoice
	// CompatibilityKey identifies the provider replay compatibility contract.
	CompatibilityKey mo.Option[string]
	// Format contains opaque provider-owned reasoning configuration.
	Format string
}

// Model contains one validated model configuration.
type Model struct {
	// ID identifies the provider model.
	ID string
	// API overrides the provider API when configured.
	API API
	// Input lists accepted modalities in configured order.
	Input []model.InputModality
	// ContextWindow is the combined input and generated-output token capacity.
	ContextWindow int64
	// MaxTokens is the maximum generated-output token count.
	MaxTokens int64
	// ToolCapabilities contains validated constrained tool support.
	ToolCapabilities model.ToolCapabilities
	// Reasoning contains validated reasoning configuration.
	Reasoning Reasoning
	// Pricing contains validated token rates when configured.
	Pricing mo.Option[model.Pricing]
}

// Provider contains one validated provider instance.
type Provider struct {
	// Type identifies the provider implementation.
	Type ProviderType
	// BaseURL is the provider API endpoint.
	BaseURL string
	// API identifies the default provider request contract.
	API API
	// APIKey contains the configured credential source when required.
	APIKey mo.Option[APIKey]
	// Models lists configured provider models.
	Models []Model
}

// Settings contains the validated startup model and UI selection.
type Settings struct {
	// DefaultProvider identifies the provider selected at startup.
	DefaultProvider string
	// DefaultModel identifies the provider model selected at startup.
	DefaultModel string
	// Providers contains validated providers by configured identifier.
	Providers map[string]Provider
	// ActiveUI identifies the preferred UI plugin when configured.
	ActiveUI mo.Option[string]
}

// Service loads one settings file.
type Service struct {
	// path is the settings file path.
	path string
}

// New creates a settings loader for one file.
func New(path string) *Service {
	return &Service{path: path}
}

// settingsFile is the strict YAML representation owned by Host persistence.
type settingsFile struct {
	// DefaultProvider contains the raw startup provider identifier.
	DefaultProvider string `yaml:"defaultProvider"`
	// DefaultModel contains the raw startup model identifier.
	DefaultModel string `yaml:"defaultModel"`
	// Providers contains raw provider mappings by configured identifier.
	Providers map[string]providerFile `yaml:"providers"`
	// ActiveUI contains the raw preferred UI plugin identifier.
	ActiveUI mo.Option[string] `yaml:"activeUI"`
}

type providerFile struct {
	// Type identifies the raw provider implementation.
	Type ProviderType `yaml:"type"`
	// BaseURL contains the raw provider API endpoint.
	BaseURL string `yaml:"baseURL"`
	// API identifies the raw default provider request contract.
	API API `yaml:"api"`
	// APIKey contains the raw credential source.
	APIKey mo.Option[apiKeyFile] `yaml:"apiKey"`
	// Models contains raw provider model mappings.
	Models []modelFile `yaml:"models"`
}

type apiKeyFile struct {
	// Literal contains a raw API key stored directly in settings.
	Literal mo.Option[string] `yaml:"literal"`
	// Environment contains a raw API key environment variable name.
	Environment mo.Option[string] `yaml:"environment"`
	// Credential contains a raw credential-store entry name.
	Credential mo.Option[string] `yaml:"credential"`
}

// modelFile contains one model mapping decoded from YAML.
type modelFile struct {
	// ID identifies the provider model.
	ID string `yaml:"id"`
	// API overrides the provider API when configured.
	API API `yaml:"api"`
	// Input contains raw modality names in configured order.
	Input []string `yaml:"input"`
	// ContextWindow contains the configured combined token capacity.
	ContextWindow int64 `yaml:"contextWindow"`
	// MaxTokens contains the configured generated-output limit.
	MaxTokens int64 `yaml:"maxTokens"`
	// ToolCapabilities contains raw constrained tool support when declared.
	ToolCapabilities mo.Option[toolCapabilitiesFile] `yaml:"toolCapabilities"`
	// Reasoning contains raw reasoning configuration.
	Reasoning reasoningFile `yaml:"reasoning"`
	// Pricing contains raw token rates when configured.
	Pricing mo.Option[pricingFile] `yaml:"pricing"`
}

// toolCapabilitiesFile contains raw constrained tool support.
type toolCapabilitiesFile struct {
	// StrictJSONSchema reports strict JSON Schema support.
	StrictJSONSchema bool `yaml:"strictJSONSchema"`
	// Grammar contains raw grammar support.
	Grammar grammarCapabilitiesFile `yaml:"grammar"`
}

// grammarCapabilitiesFile contains raw grammar support.
type grammarCapabilitiesFile struct {
	// Lark reports Lark grammar support.
	Lark bool `yaml:"lark"`
	// Regex reports regular-expression grammar support.
	Regex bool `yaml:"regex"`
}

type pricingFile struct {
	// Input contains the raw uncached input token rate.
	Input mo.Option[float64] `yaml:"input"`
	// Output contains the raw output token rate.
	Output mo.Option[float64] `yaml:"output"`
	// CacheRead contains the raw cached input token rate.
	CacheRead mo.Option[float64] `yaml:"cacheRead"`
	// CacheWrite contains the raw cache creation token rate.
	CacheWrite mo.Option[float64] `yaml:"cacheWrite"`
	// Tiers contains raw request-wide rate overrides.
	Tiers []pricingTierFile `yaml:"tiers"`
}

type pricingTierFile struct {
	// InputTokensAbove contains the raw exclusive input threshold.
	InputTokensAbove mo.Option[int64] `yaml:"inputTokensAbove"`
	// Input contains the raw uncached input token rate.
	Input mo.Option[float64] `yaml:"input"`
	// Output contains the raw output token rate.
	Output mo.Option[float64] `yaml:"output"`
	// CacheRead contains the raw cached input token rate.
	CacheRead mo.Option[float64] `yaml:"cacheRead"`
	// CacheWrite contains the raw cache creation token rate.
	CacheWrite mo.Option[float64] `yaml:"cacheWrite"`
}

type reasoningFile struct {
	// Supported contains the raw reasoning support flag.
	Supported mo.Option[bool] `yaml:"supported"`
	// Choices contains raw supported reasoning choices.
	Choices []ReasoningChoice `yaml:"choices"`
	// Default contains the raw default reasoning choice.
	Default ReasoningChoice `yaml:"default"`
	// CompatibilityKey contains the raw replay compatibility contract.
	CompatibilityKey mo.Option[string] `yaml:"compatibilityKey"`
	// Format contains the raw opaque provider reasoning format.
	Format string `yaml:"format"`
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
	fields, err := decodeYAMLMapping(
		node, "id", "api", "input", "contextWindow", "maxTokens", "toolCapabilities", "reasoning", "pricing",
	)
	if err != nil {
		return err
	}
	var decoded modelFile
	pricing, decodeErr := decodeYAMLOption[pricingFile](fields, "pricing")
	if decodeErr != nil {
		return decodeErr
	}
	decoded.Pricing = pricing
	toolCapabilities, decodeErr := decodeYAMLOption[toolCapabilitiesFile](fields, "toolCapabilities")
	if decodeErr != nil {
		return decodeErr
	}
	decoded.ToolCapabilities = toolCapabilities
	if fieldErr := decodeYAMLField(fields, "reasoning", &decoded.Reasoning); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := decodeYAMLField(fields, "maxTokens", &decoded.MaxTokens); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := decodeYAMLField(fields, "contextWindow", &decoded.ContextWindow); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := decodeYAMLField(fields, "input", &decoded.Input); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := decodeYAMLField(fields, "api", &decoded.API); fieldErr != nil {
		return fieldErr
	}
	if fieldErr := decodeYAMLField(fields, "id", &decoded.ID); fieldErr != nil {
		return fieldErr
	}
	*configured = decoded
	return nil
}

// UnmarshalYAML decodes strict tool capability fields.
func (configured *toolCapabilitiesFile) UnmarshalYAML(node *yaml.Node) error {
	fields, err := decodeYAMLMapping(node, "strictJSONSchema", "grammar")
	if err != nil {
		return err
	}
	var decoded toolCapabilitiesFile
	if decodeErr := decodeYAMLField(fields, "grammar", &decoded.Grammar); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "strictJSONSchema", &decoded.StrictJSONSchema); decodeErr != nil {
		return decodeErr
	}
	*configured = decoded
	return nil
}

// UnmarshalYAML decodes strict grammar capability fields.
func (configured *grammarCapabilitiesFile) UnmarshalYAML(node *yaml.Node) error {
	fields, err := decodeYAMLMapping(node, "lark", "regex")
	if err != nil {
		return err
	}
	var decoded grammarCapabilitiesFile
	if decodeErr := decodeYAMLField(fields, "regex", &decoded.Regex); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "lark", &decoded.Lark); decodeErr != nil {
		return decodeErr
	}
	*configured = decoded
	return nil
}

func (configured *pricingFile) UnmarshalYAML(node *yaml.Node) error {
	fields, err := decodeYAMLMapping(node, "input", "output", "cacheRead", "cacheWrite", "tiers")
	if err != nil {
		return err
	}
	var decoded pricingFile
	var decodeErr error
	if decoded.Input, decodeErr = decodeYAMLOption[float64](fields, "input"); decodeErr != nil {
		return decodeErr
	}
	if decoded.Output, decodeErr = decodeYAMLOption[float64](fields, "output"); decodeErr != nil {
		return decodeErr
	}
	if decoded.CacheRead, decodeErr = decodeYAMLOption[float64](fields, "cacheRead"); decodeErr != nil {
		return decodeErr
	}
	if decoded.CacheWrite, decodeErr = decodeYAMLOption[float64](fields, "cacheWrite"); decodeErr != nil {
		return decodeErr
	}
	if tiersDecodeErr := decodeYAMLField(fields, "tiers", &decoded.Tiers); tiersDecodeErr != nil {
		return tiersDecodeErr
	}
	*configured = decoded
	return nil
}

func (configured *pricingTierFile) UnmarshalYAML(node *yaml.Node) error {
	fields, err := decodeYAMLMapping(node, "inputTokensAbove", "input", "output", "cacheRead", "cacheWrite")
	if err != nil {
		return err
	}
	var decoded pricingTierFile
	var decodeErr error
	if decoded.InputTokensAbove, decodeErr = decodeYAMLOption[int64](fields, "inputTokensAbove"); decodeErr != nil {
		return decodeErr
	}
	if decoded.Input, decodeErr = decodeYAMLOption[float64](fields, "input"); decodeErr != nil {
		return decodeErr
	}
	if decoded.Output, decodeErr = decodeYAMLOption[float64](fields, "output"); decodeErr != nil {
		return decodeErr
	}
	if decoded.CacheRead, decodeErr = decodeYAMLOption[float64](fields, "cacheRead"); decodeErr != nil {
		return decodeErr
	}
	if decoded.CacheWrite, decodeErr = decodeYAMLOption[float64](fields, "cacheWrite"); decodeErr != nil {
		return decodeErr
	}
	*configured = decoded
	return nil
}

func (configured *reasoningFile) UnmarshalYAML(node *yaml.Node) error {
	fields, err := decodeYAMLMapping(node, "supported", "choices", "default", "compatibilityKey", "format")
	if err != nil {
		return err
	}
	var decoded reasoningFile
	supported, supportedDecodeErr := decodeYAMLOption[bool](fields, "supported")
	if supportedDecodeErr != nil {
		return supportedDecodeErr
	}
	decoded.Supported = supported
	if decodeErr := decodeYAMLField(fields, "choices", &decoded.Choices); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "default", &decoded.Default); decodeErr != nil {
		return decodeErr
	}
	if decodeErr := decodeYAMLField(fields, "format", &decoded.Format); decodeErr != nil {
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
		return Settings{}, fmt.Errorf("decode Glyph settings: %w", err)
	}
	var extra any
	trailingErr := decoder.Decode(&extra)
	if !errors.Is(trailingErr, io.EOF) {
		if trailingErr == nil {
			return Settings{}, errors.New("decode Glyph settings: multiple YAML documents are not allowed")
		}
		return Settings{}, fmt.Errorf("decode trailing Glyph settings: %w", trailingErr)
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
	for modelIndex := range configured.Models {
		validatedModel, err := validateModel(providerID, configured.Type, configured.Models[modelIndex])
		if err != nil {
			return Provider{}, err
		}
		if _, duplicate := seenModels[validatedModel.ID]; duplicate {
			return Provider{}, fmt.Errorf("provider %q has duplicate model ID %q", providerID, validatedModel.ID)
		}
		seenModels[validatedModel.ID] = struct{}{}
		provider.Models = append(provider.Models, validatedModel)
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

// validateModel validates one model and its provider-neutral execution capabilities.
func validateModel(providerID string, providerType ProviderType, configured modelFile) (Model, error) {
	if err := validateIdentifier("model ID", configured.ID); err != nil {
		return Model{}, fmt.Errorf("provider %q: %w", providerID, err)
	}
	// input preserves validated modalities for the runtime descriptor.
	input, err := validateModelInput(providerID, configured.ID, configured.Input)
	if err != nil {
		return Model{}, err
	}
	if configured.ContextWindow <= 0 {
		return Model{}, fmt.Errorf(
			"provider %q model %q: contextWindow must be greater than zero", providerID, configured.ID,
		)
	}
	if configured.MaxTokens <= 0 {
		return Model{}, fmt.Errorf(
			"provider %q model %q: maxTokens must be greater than zero", providerID, configured.ID,
		)
	}
	if configured.MaxTokens > configured.ContextWindow {
		return Model{}, fmt.Errorf(
			"provider %q model %q: maxTokens must not exceed contextWindow", providerID, configured.ID,
		)
	}
	if configured.API != "" {
		if providerType != ProviderTypeOpenAICompatible {
			return Model{}, fmt.Errorf("provider %q model %q cannot override API", providerID, configured.ID)
		}
		if !isAPISupported(configured.API) {
			return Model{}, fmt.Errorf("provider %q model %q has unsupported API %q", providerID, configured.ID, configured.API)
		}
	}
	toolCapabilities, err := validateToolCapabilities(providerID, configured.ID, configured.ToolCapabilities)
	if err != nil {
		return Model{}, err
	}
	reasoning, err := validateReasoning(providerID, configured.ID, configured.Reasoning)
	if err != nil {
		return Model{}, err
	}
	pricing, err := validatePricing(providerID, configured.ID, configured.Pricing)
	if err != nil {
		return Model{}, err
	}
	return Model{
		ID: configured.ID, API: configured.API, Input: input,
		ContextWindow: configured.ContextWindow, MaxTokens: configured.MaxTokens,
		ToolCapabilities: toolCapabilities, Reasoning: reasoning, Pricing: pricing,
	}, nil
}

// validateToolCapabilities requires and maps declarative constrained tool support.
func validateToolCapabilities(
	providerID string,
	modelID string,
	configured mo.Option[toolCapabilitiesFile],
) (model.ToolCapabilities, error) {
	capabilities, present := configured.Get()
	if !present {
		return model.ToolCapabilities{}, fmt.Errorf(
			"provider %q model %q: toolCapabilities is required", providerID, modelID,
		)
	}
	return model.ToolCapabilities{
		StrictJSONSchema: capabilities.StrictJSONSchema,
		Grammar: model.GrammarCapabilities{
			Lark: capabilities.Grammar.Lark, Regex: capabilities.Grammar.Regex,
		},
	}, nil
}

// validateModelInput converts the closed settings values while preserving their configured order.
func validateModelInput(providerID, modelID string, configured []string) ([]model.InputModality, error) {
	if len(configured) == 0 {
		return nil, fmt.Errorf("provider %q model %q: input must not be empty", providerID, modelID)
	}
	// input accumulates validated modalities in configured order.
	input := make([]model.InputModality, 0, len(configured))
	// seen rejects duplicate modalities without changing their order.
	seen := make(map[model.InputModality]struct{}, len(configured))
	for _, value := range configured {
		modality := model.InputModality(value)
		if modality != model.InputModalityText && modality != model.InputModalityImage {
			return nil, fmt.Errorf(
				"provider %q model %q: input contains unknown modality %q", providerID, modelID, value,
			)
		}
		if _, exists := seen[modality]; exists {
			return nil, fmt.Errorf(
				"provider %q model %q: input contains duplicate modality %q", providerID, modelID, value,
			)
		}
		seen[modality] = struct{}{}
		input = append(input, modality)
	}
	// The required text entry makes every model usable by the text-based agent path.
	if _, exists := seen[model.InputModalityText]; !exists {
		return nil, fmt.Errorf("provider %q model %q: input must contain %q", providerID, modelID, model.InputModalityText)
	}
	return input, nil
}

// validatePricing preserves configured zero rates while rejecting incomplete or invalid mappings.
func validatePricing(
	providerID string,
	modelID string,
	configured mo.Option[pricingFile],
) (mo.Option[model.Pricing], error) {
	pricing, present := configured.Get()
	if !present {
		return mo.None[model.Pricing](), nil
	}
	rates, err := validatePricingRates(pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite)
	if err != nil {
		return mo.None[model.Pricing](), fmt.Errorf("provider %q model %q pricing: %w", providerID, modelID, err)
	}
	var tiers []model.PricingTier
	if pricing.Tiers != nil {
		tiers = make([]model.PricingTier, len(pricing.Tiers))
	}
	var previousThreshold int64
	for tierIndex := range pricing.Tiers {
		tier := pricing.Tiers[tierIndex]
		threshold, thresholdPresent := tier.InputTokensAbove.Get()
		if !thresholdPresent || threshold <= 0 || threshold <= previousThreshold {
			return mo.None[model.Pricing](), fmt.Errorf(
				"provider %q model %q pricing tier thresholds must be positive and strictly increasing",
				providerID,
				modelID,
			)
		}
		tierRates, rateErr := validatePricingRates(tier.Input, tier.Output, tier.CacheRead, tier.CacheWrite)
		if rateErr != nil {
			return mo.None[model.Pricing](), fmt.Errorf(
				"provider %q model %q pricing tier: %w", providerID, modelID, rateErr,
			)
		}
		tiers[tierIndex] = model.PricingTier{
			InputTokensAbove: threshold,
			Input:            tierRates.input, Output: tierRates.output,
			CacheRead: tierRates.cacheRead, CacheWrite: tierRates.cacheWrite,
		}
		previousThreshold = threshold
	}
	return mo.Some(model.Pricing{
		Input: rates.input, Output: rates.output,
		CacheRead: rates.cacheRead, CacheWrite: rates.cacheWrite, Tiers: tiers,
	}), nil
}

type pricingRates struct {
	// input is the validated uncached input token rate.
	input float64
	// output is the validated output token rate.
	output float64
	// cacheRead is the validated cached input token rate.
	cacheRead float64
	// cacheWrite is the validated cache creation token rate.
	cacheWrite float64
}

// validatePricingRates returns all four required finite nonnegative rates without changing zero values.
func validatePricingRates(
	inputOption mo.Option[float64],
	outputOption mo.Option[float64],
	cacheReadOption mo.Option[float64],
	cacheWriteOption mo.Option[float64],
) (pricingRates, error) {
	input, inputPresent := inputOption.Get()
	output, outputPresent := outputOption.Get()
	cacheRead, cacheReadPresent := cacheReadOption.Get()
	cacheWrite, cacheWritePresent := cacheWriteOption.Get()
	if !inputPresent || !outputPresent || !cacheReadPresent || !cacheWritePresent {
		return pricingRates{}, errors.New("all rates are required")
	}
	for _, rate := range []float64{input, output, cacheRead, cacheWrite} {
		if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return pricingRates{}, errors.New("rates must be finite and nonnegative")
		}
	}
	return pricingRates{input: input, output: output, cacheRead: cacheRead, cacheWrite: cacheWrite}, nil
}

// validateReasoning validates one provider-neutral capability shape and opaque provider format.
//
//nolint:gocyclo // The flat validation mirrors the closed capability shapes.
func validateReasoning(providerID, modelID string, configured reasoningFile) (Reasoning, error) {
	supported, supportedPresent := configured.Supported.Get()
	if !supportedPresent {
		return Reasoning{}, fmt.Errorf("provider %q model %q reasoning requires supported", providerID, modelID)
	}
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
	if !supported {
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
	if err := validateReasoningShape(choices, configured.Default); err != nil {
		return Reasoning{}, fmt.Errorf("provider %q model %q: %w", providerID, modelID, err)
	}
	return Reasoning{
		Supported: true, Choices: choices, Default: configured.Default,
		CompatibilityKey: key, Format: configured.Format,
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
	if err != nil {
		return fmt.Errorf("parse baseURL: %w", err)
	}
	if !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
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
