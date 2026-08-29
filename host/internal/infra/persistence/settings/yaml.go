package settings

import (
	"errors"
	"fmt"

	"github.com/samber/mo"
	"go.yaml.in/yaml/v3"
)

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
	grammar, strictJSONSchema, err := decodeYAMLPair[grammarCapabilitiesFile, bool](
		node, "grammar", "strictJSONSchema",
	)
	if err != nil {
		return err
	}
	*configured = toolCapabilitiesFile{StrictJSONSchema: strictJSONSchema, Grammar: grammar}
	return nil
}

// UnmarshalYAML decodes strict grammar capability fields.
func (configured *grammarCapabilitiesFile) UnmarshalYAML(node *yaml.Node) error {
	regex, lark, err := decodeYAMLPair[bool, bool](node, "regex", "lark")
	if err != nil {
		return err
	}
	*configured = grammarCapabilitiesFile{Lark: lark, Regex: regex}
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

// decodeYAMLPair decodes two fields without partially mutating the destination.
func decodeYAMLPair[A, B any](
	node *yaml.Node,
	firstName string,
	secondName string,
) (first A, second B, err error) {
	fields, err := decodeYAMLMapping(node, firstName, secondName)
	if err != nil {
		return first, second, err
	}
	if fieldErr := decodeYAMLField(fields, firstName, &first); fieldErr != nil {
		return first, second, fieldErr
	}
	if fieldErr := decodeYAMLField(fields, secondName, &second); fieldErr != nil {
		return first, second, fieldErr
	}
	return first, second, nil
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
