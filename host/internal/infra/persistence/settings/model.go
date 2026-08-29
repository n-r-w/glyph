package settings

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
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
