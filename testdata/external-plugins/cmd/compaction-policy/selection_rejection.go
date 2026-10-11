// Package main provides the reference compaction policy fixture.
package main

import (
	"context"
	"errors"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// handleMissingModelSelection exercises a handled nested selection rejection.
func handleMissingModelSelection(ctx context.Context, binding *extensionsdk.ExtensionContext, providerID string) error {
	const missingModel = "reference-missing-selection-model"
	request := extensionpb.SelectModelRequest_builder{
		Context: nil, ProviderId: new(providerID), ModelId: new(missingModel),
	}.Build()
	selection, err := binding.StartModelSelection(ctx, request)
	if err != nil {
		return err
	}
	_, rejection := selection.Wait(ctx)
	if rejection == nil {
		return errors.New("reference policy expected an unknown-model selection rejection")
	}
	return nil
}
