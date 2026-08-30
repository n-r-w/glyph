package app

import (
	"fmt"
	"maps"
	"slices"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	internalhooks "github.com/n-r-w/glyph/host/internal/hooks"

	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	credentialstore "github.com/n-r-w/glyph/host/internal/infra/persistence/credentials"
	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"

	"github.com/n-r-w/glyph/host/internal/infra/providers/openai/codex"
	"github.com/n-r-w/glyph/host/internal/infra/providers/openai/compatible"

	"github.com/n-r-w/glyph/host/internal/usecase/host/providers"
)

// newProviderCatalog maps validated startup settings into the runtime catalog.
func newProviderCatalog(
	configured settingstore.Settings,
	paths persistence.Paths,
	interaction codex.Interaction,
	hookRunner internalhooks.ProviderRunner,
) (*providers.Catalog, error) {
	providerIDs := slices.Collect(maps.Keys(configured.Providers))
	slices.Sort(providerIDs)

	entries := make([]providers.Entry, 0)
	for _, providerID := range providerIDs {
		providerConfig := configured.Providers[providerID]
		switch providerConfig.Type {
		case settingstore.ProviderTypeOpenAICodex:
			credentials := credentialstore.New(paths.CredentialsFile, codex.ProviderID)
			modelIDs := make([]model.ID, len(providerConfig.Models))
			compatibilityKeys := make(map[model.ID]mo.Option[string], len(providerConfig.Models))
			for modelIndex := range providerConfig.Models {
				configuredModel := &providerConfig.Models[modelIndex]
				modelID := model.ID(configuredModel.ID)
				modelIDs[modelIndex] = modelID
				compatibilityKeys[modelID] = configuredModel.Reasoning.CompatibilityKey
			}
			provider := codex.New(codex.Config{
				Hooks: hookRunner, Models: modelIDs, ReasoningCompatibilityKeys: compatibilityKeys,
			}, credentials, interaction)
			for modelIndex := range providerConfig.Models {
				configuredModel := &providerConfig.Models[modelIndex]
				entries = append(entries, providers.Entry{
					Descriptor: model.Descriptor{
						Provider: codex.ProviderID, Model: model.ID(configuredModel.ID),
						Input: configuredModel.Input, ContextWindow: configuredModel.ContextWindow,
						MaxTokens:             configuredModel.MaxTokens,
						ReasoningCapabilities: reasoningCapabilities(configuredModel.Reasoning),
						ToolCapabilities:      configuredModel.ToolCapabilities,
						Pricing:               configuredModel.Pricing,
					},
					Provider: provider, SelectionCredentialValidator: nil, Authentication: provider,
				})
			}
		case settingstore.ProviderTypeOpenAICompatible:
			resolver := credentialstore.NewAPIKeyResolver(
				paths.CredentialsFile, apiKeySource(providerConfig.APIKey),
			)
			modelAPIs := make(map[model.ID]compatible.API, len(providerConfig.Models))
			formats := make(map[model.ID]string, len(providerConfig.Models))
			compatibilityKeys := make(map[model.ID]mo.Option[string], len(providerConfig.Models))
			for modelIndex := range providerConfig.Models {
				configuredModel := &providerConfig.Models[modelIndex]
				modelID := model.ID(configuredModel.ID)
				modelAPIs[modelID] = compatible.API(configuredModel.API)
				if configuredModel.Reasoning.Supported {
					formats[modelID] = configuredModel.Reasoning.Format
				}
				compatibilityKeys[modelID] = configuredModel.Reasoning.CompatibilityKey
			}
			provider, err := compatible.New(compatible.Config{
				ProviderID: model.ProviderID(providerID), BaseURL: providerConfig.BaseURL,
				API: compatible.API(providerConfig.API), Models: modelAPIs,
				ReasoningFormats: formats, ReasoningCompatibilityKeys: compatibilityKeys, APIKey: resolver,
			})
			if err != nil {
				return nil, fmt.Errorf("create provider %q: %w", providerID, err)
			}
			for modelIndex := range providerConfig.Models {
				configuredModel := &providerConfig.Models[modelIndex]
				entries = append(entries, providers.Entry{
					Descriptor: model.Descriptor{
						Provider: model.ProviderID(providerID), Model: model.ID(configuredModel.ID),
						Input: configuredModel.Input, ContextWindow: configuredModel.ContextWindow,
						MaxTokens:             configuredModel.MaxTokens,
						ReasoningCapabilities: reasoningCapabilities(configuredModel.Reasoning),
						ToolCapabilities:      configuredModel.ToolCapabilities,
						Pricing:               configuredModel.Pricing,
					},
					Provider: provider, SelectionCredentialValidator: resolver, Authentication: nil,
				})
			}
		default:
			return nil, fmt.Errorf("unsupported configured provider type %q", providerConfig.Type)
		}
	}
	defaultProvider := configured.Providers[configured.DefaultProvider]
	defaultModel := settingstore.Model{
		ID: "", API: "", Input: nil, ContextWindow: 0, MaxTokens: 0,
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: false,
			Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
		},
		Reasoning: settingstore.Reasoning{
			Supported: false, Choices: nil, Default: "", CompatibilityKey: mo.None[string](), Format: "",
		}, Pricing: mo.None[model.Pricing](),
	}
	defaultModelIndex := slices.IndexFunc(defaultProvider.Models, func(configuredModel settingstore.Model) bool {
		return configuredModel.ID == configured.DefaultModel
	})
	if defaultModelIndex >= 0 {
		defaultModel = defaultProvider.Models[defaultModelIndex]
	}
	return providers.New(entries, model.Selection{
		Provider: model.ProviderID(configured.DefaultProvider), Model: model.ID(configured.DefaultModel),
		ReasoningChoice: model.ReasoningChoice(defaultModel.Reasoning.Default),
	})
}

// reasoningCapabilities maps validated persistence values into the model domain.
func reasoningCapabilities(configured settingstore.Reasoning) model.ReasoningCapabilities {
	choices := lo.Map(configured.Choices, func(choice settingstore.ReasoningChoice, _ int) model.ReasoningChoice {
		return model.ReasoningChoice(choice)
	})
	return model.ReasoningCapabilities{
		Supported: configured.Supported, Choices: choices, Default: model.ReasoningChoice(configured.Default),
	}
}

// apiKeySource maps the validated settings union without resolving its secret.
func apiKeySource(configured mo.Option[settingstore.APIKey]) credentialstore.APIKeySource {
	apiKey, present := configured.Get()
	if !present {
		return credentialstore.APIKeySource{}
	}
	if literal, selected := apiKey.Literal.Get(); selected {
		return credentialstore.APIKeySource{
			Kind: credentialstore.APIKeySourceLiteral, Value: literal,
		}
	}
	if environment, selected := apiKey.Environment.Get(); selected {
		return credentialstore.APIKeySource{
			Kind: credentialstore.APIKeySourceEnvironment, Value: environment,
		}
	}
	credential, selected := apiKey.Credential.Get()
	if !selected {
		return credentialstore.APIKeySource{}
	}
	return credentialstore.APIKeySource{
		Kind: credentialstore.APIKeySourceCredential, Value: credential,
	}
}
