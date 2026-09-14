package providers

import (
	"context"
	"slices"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// ResolveBinding resolves one exact selection without changing active selection or checking credentials.
func (c *Catalog) ResolveBinding(selection model.Selection) (modelexecution.CatalogBinding, error) {
	// entry is the immutable raw binding for the exact logical selection.
	entry, err := c.resolveEntry(selection)
	if err != nil {
		return modelexecution.CatalogBinding{}, err
	}
	return modelexecution.CatalogBinding{
		Model:           entry.Descriptor.Clone(),
		ReasoningChoice: selection.ReasoningChoice,
		Provider:        entry.Provider,
	}, nil
}

// ResolveConfiguredBinding resolves one exact selection and checks its request credentials.
func (c *Catalog) ResolveConfiguredBinding(
	ctx context.Context,
	selection model.Selection,
) (modelexecution.CatalogBinding, error) {
	if err := ctx.Err(); err != nil {
		return modelexecution.CatalogBinding{}, err
	}
	// entry is the immutable raw binding for the exact configured selection.
	entry, err := c.resolveEntry(selection)
	if err != nil {
		return modelexecution.CatalogBinding{}, err
	}
	if credentialErr := checkRequestCredentials(ctx, entry); credentialErr != nil {
		return modelexecution.CatalogBinding{}, credentialErr
	}
	return modelexecution.CatalogBinding{
		Model:           entry.Descriptor.Clone(),
		ReasoningChoice: selection.ReasoningChoice,
		Provider:        entry.Provider,
	}, nil
}

// resolveEntry validates and returns one exact configured entry.
func (c *Catalog) resolveEntry(selection model.Selection) (Entry, error) {
	// entryIndex identifies the exact provider and model pair without active-state fallback.
	entryIndex, found := c.entryIndex(selection.Provider, selection.Model)
	if !found {
		return Entry{}, &SelectionError{Code: ErrorCodeNotFound, cause: nil}
	}
	// entry contains the immutable configured descriptor and raw provider binding.
	entry := c.entries[entryIndex]
	if !slices.Contains(entry.Descriptor.ReasoningCapabilities.Choices, selection.ReasoningChoice) {
		return Entry{}, &SelectionError{Code: ErrorCodeReasoningUnsupported, cause: nil}
	}
	return entry, nil
}

// checkRequestCredentials checks credentials for one resolved request entry.
func checkRequestCredentials(ctx context.Context, entry Entry) error {
	if entry.CredentialChecker != nil {
		if err := entry.CredentialChecker.CheckCredentials(ctx); err != nil {
			return &SelectionError{Code: ErrorCodeCredentialUnavailable, cause: err}
		}
	}
	if entry.Authentication != nil {
		if err := entry.Authentication.CheckCredentials(ctx); err != nil {
			return &SelectionError{Code: ErrorCodeCredentialUnavailable, cause: err}
		}
	}
	return nil
}
