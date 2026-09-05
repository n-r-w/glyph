//go:build integration

package app

import (
	"context"
	"fmt"

	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// readFixtureCatalogues exercises public SDK handles from inside a real extension process callback.
func readFixtureCatalogues(ctx context.Context) (string, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return "", err
	}
	models, err := binding.StartGetModels(ctx)
	if err != nil {
		return "", err
	}
	modelResult, err := models.Wait(ctx)
	if err != nil {
		return "", err
	}
	providers, err := binding.StartGetProviders(ctx)
	if err != nil {
		return "", err
	}
	providerResult, err := providers.Wait(ctx)
	if err != nil {
		return "", err
	}
	selection := modelResult.GetActiveSelection()
	for _, provider := range providerResult.GetProviders() {
		if provider.GetProviderId() != selection.GetProviderId() {
			continue
		}
		for _, modelID := range provider.GetModelIds() {
			if modelID == selection.GetModelId() {
				return provider.GetProviderId() + ":" + modelID, nil
			}
		}
	}
	return "", fmt.Errorf("active model selection is absent from provider catalog")
}
