package settings

import (
	"errors"
	"fmt"

	"math"
	"net/url"

	"slices"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/pluginid"
)

// validate applies the closed provider and startup selection rules.
func (decoded settingsFile) validate() (Settings, error) {
	if err := decoded.validateDefaults(); err != nil {
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

// validateDefaults validates required top-level settings fields.
func (decoded settingsFile) validateDefaults() error {
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
		provider, err := configured[providerID].validate(providerID)
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

// validate maps one provider file after checking its complete contract.
func (configured providerFile) validate(providerID string) (Provider, error) {
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
		apiKey, err := configured.validateCompatible(providerID)
		if err != nil {
			return Provider{}, err
		}
		provider.APIKey = apiKey
	}

	seenModels := make(map[string]struct{}, len(configured.Models))
	for modelIndex := range configured.Models {
		validatedModel, err := configured.Models[modelIndex].validate(providerID, configured.Type)
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

// validateCompatible validates fields specific to an OpenAI-compatible provider.
func (configured providerFile) validateCompatible(providerID string) (mo.Option[APIKey], error) {
	if err := validateBaseURL(configured.BaseURL); err != nil {
		return mo.None[APIKey](), fmt.Errorf("provider %q: %w", providerID, err)
	}
	if !configured.API.Supported() {
		return mo.None[APIKey](), fmt.Errorf("provider %q has unsupported API %q", providerID, configured.API)
	}
	return validateAPIKey(providerID, configured.APIKey)
}

// validate validates one model and its provider-neutral execution capabilities.
func (configured modelFile) validate(providerID string, providerType ProviderType) (Model, error) {
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
		if !configured.API.Supported() {
			return Model{}, fmt.Errorf(
				"provider %q model %q has unsupported API %q",
				providerID,
				configured.ID,
				configured.API,
			)
		}
	}
	toolCapabilities, err := validateToolCapabilities(providerID, configured.ID, configured.ToolCapabilities)
	if err != nil {
		return Model{}, err
	}
	reasoning, err := configured.Reasoning.validate(providerID, configured.ID)
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
		return nil, fmt.Errorf(
			"provider %q model %q: input must contain %q",
			providerID,
			modelID,
			model.InputModalityText,
		)
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

// Supported reports whether the API belongs to the closed settings contract.
func (api API) Supported() bool {
	return api == APIChatCompletions || api == APIResponses
}

// Supported reports whether the reasoning choice belongs to the closed settings contract.
func (level ReasoningChoice) Supported() bool {
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
