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

// ReasoningLevel is one configured model reasoning level.
type ReasoningLevel string

const (
	// ReasoningLevelNone disables model reasoning when the selected model supports it.
	ReasoningLevelNone ReasoningLevel = "none"
	// ReasoningLevelMinimal requests minimal model reasoning.
	ReasoningLevelMinimal ReasoningLevel = "minimal"
	// ReasoningLevelLow requests low model reasoning.
	ReasoningLevelLow ReasoningLevel = "low"
	// ReasoningLevelMedium requests medium model reasoning.
	ReasoningLevelMedium ReasoningLevel = "medium"
	// ReasoningLevelHigh requests high model reasoning.
	ReasoningLevelHigh ReasoningLevel = "high"
	// ReasoningLevelXHigh requests extra-high model reasoning.
	ReasoningLevelXHigh ReasoningLevel = "xhigh"
	// ReasoningLevelMax requests maximum model reasoning.
	ReasoningLevelMax ReasoningLevel = "max"
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
	Literal     *string
	Environment *string
	Credential  *string
}

// Model contains one configured model and its supported reasoning levels.
type Model struct {
	ID              string
	API             API
	ReasoningLevels []ReasoningLevel
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
	DefaultProvider       string
	DefaultModel          string
	DefaultReasoningLevel ReasoningLevel
	Providers             map[string]Provider
	ActiveUI              string
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
	DefaultProvider       string                  `yaml:"defaultProvider"`
	DefaultModel          string                  `yaml:"defaultModel"`
	DefaultReasoningLevel ReasoningLevel          `yaml:"defaultReasoningLevel"`
	Providers             map[string]providerFile `yaml:"providers"`
	ActiveUI              *string                 `yaml:"activeUI"`
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
	ID              string           `yaml:"id"`
	API             API              `yaml:"api"`
	ReasoningLevels []ReasoningLevel `yaml:"reasoningLevels"`
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
	defaultModel, found := findModel(defaultProvider.Models, decoded.DefaultModel)
	if !found {
		return Settings{}, errors.New("defaultModel must identify a model on defaultProvider")
	}
	if !supportsReasoningLevel(defaultModel.ReasoningLevels, decoded.DefaultReasoningLevel) {
		return Settings{}, errors.New("defaultReasoningLevel must be supported by the default model")
	}
	activeUI, err := validateActiveUI(decoded.ActiveUI)
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		DefaultProvider:       decoded.DefaultProvider,
		DefaultModel:          decoded.DefaultModel,
		DefaultReasoningLevel: decoded.DefaultReasoningLevel,
		Providers:             providers,
		ActiveUI:              activeUI,
	}, nil
}

func validateDefaults(decoded settingsFile) error {
	if err := validateIdentifier("defaultProvider", decoded.DefaultProvider); err != nil {
		return err
	}
	if err := validateIdentifier("defaultModel", decoded.DefaultModel); err != nil {
		return err
	}
	if !isReasoningLevelSupported(decoded.DefaultReasoningLevel) {
		return fmt.Errorf("defaultReasoningLevel %q is not supported", decoded.DefaultReasoningLevel)
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
	return Model{ID: "", API: "", ReasoningLevels: nil}, false
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
		model, err := validateModel(providerID, configured.Type, configuredModel)
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

func validateModel(providerID string, providerType ProviderType, configured modelFile) (Model, error) {
	if err := validateIdentifier("model ID", configured.ID); err != nil {
		return Model{}, fmt.Errorf("provider %q: %w", providerID, err)
	}
	if configured.API != "" {
		if providerType != ProviderTypeOpenAICompatible {
			return Model{}, fmt.Errorf("provider %q model %q cannot override API", providerID, configured.ID)
		}
		if !isAPISupported(configured.API) {
			return Model{}, fmt.Errorf("provider %q model %q has unsupported API %q", providerID, configured.ID, configured.API)
		}
	}
	if len(configured.ReasoningLevels) == 0 {
		return Model{}, fmt.Errorf("provider %q model %q must contain reasoning levels", providerID, configured.ID)
	}
	levels := make([]ReasoningLevel, len(configured.ReasoningLevels))
	seenLevels := make(map[ReasoningLevel]struct{}, len(configured.ReasoningLevels))
	for index, level := range configured.ReasoningLevels {
		if !isReasoningLevelSupported(level) {
			return Model{}, fmt.Errorf(
				"provider %q model %q has unsupported reasoning level %q",
				providerID, configured.ID, level,
			)
		}
		if _, duplicate := seenLevels[level]; duplicate {
			return Model{}, fmt.Errorf(
				"provider %q model %q has duplicate reasoning level %q",
				providerID, configured.ID, level,
			)
		}
		seenLevels[level] = struct{}{}
		levels[index] = level
	}
	return Model{ID: configured.ID, API: configured.API, ReasoningLevels: levels}, nil
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

func supportsReasoningLevel(levels []ReasoningLevel, target ReasoningLevel) bool {
	for _, level := range levels {
		if level == target {
			return true
		}
	}
	return false
}

// isReasoningLevelSupported recognizes the complete configured reasoning-level set.
func isReasoningLevelSupported(level ReasoningLevel) bool {
	switch level {
	case ReasoningLevelNone,
		ReasoningLevelMinimal,
		ReasoningLevelLow,
		ReasoningLevelMedium,
		ReasoningLevelHigh,
		ReasoningLevelXHigh,
		ReasoningLevelMax:
		return true
	default:
		return false
	}
}
