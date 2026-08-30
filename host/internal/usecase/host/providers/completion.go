package providers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// ValidateConfigured validates one configured selection without executing or changing active selection.
func (c *Catalog) ValidateConfigured(ctx context.Context, selection model.Selection) error {
	_, err := c.configuredEntry(ctx, selection)
	return err
}

// CompleteConfigured executes one configured model without changing the active catalog selection.
func (c *Catalog) CompleteConfigured(
	ctx context.Context,
	selection model.Selection,
	instructions string,
	history []agent.HistoryEntry,
) (model.Response, error) {
	entry, err := c.configuredEntry(ctx, selection)
	if err != nil {
		return model.Response{}, err
	}
	ownedHistory := make([]agent.HistoryEntry, len(history))
	for index := range history {
		clone, cloneErr := history[index].ValidatedClone()
		if cloneErr != nil {
			return model.Response{}, fmt.Errorf("validate configured completion history: %w", cloneErr)
		}
		ownedHistory[index] = clone
	}
	terminal := mo.None[model.Response]()
	streamErr := entry.Provider.Stream(ctx, agentrun.ModelRequest{
		Instructions: instructions, Model: entry.Descriptor.Clone(), ReasoningChoice: selection.ReasoningChoice,
		History: ownedHistory, Tools: nil,
	}, func(event agentrun.StreamEvent) error {
		if event.Kind == agentrun.StreamEventDone || event.Kind == agentrun.StreamEventError {
			response, present := event.Response.Get()
			if !present {
				return errors.New("configured completion terminal event has no response")
			}
			terminal = mo.Some(response.Clone())
		}
		return nil
	})
	if streamErr != nil {
		return model.Response{}, fmt.Errorf("execute configured model: %w", streamErr)
	}
	response, present := terminal.Get()
	if !present {
		return model.Response{}, errors.New("configured completion ended without a terminal response")
	}
	return response, nil
}

// configuredEntry resolves and validates one exact configured selection.
func (c *Catalog) configuredEntry(ctx context.Context, selection model.Selection) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	entryIndex, found := c.entryIndex(selection.Provider, selection.Model)
	if !found {
		return Entry{}, &SelectionError{Code: ErrorCodeNotFound, cause: nil}
	}
	entry := c.entries[entryIndex]
	if !slices.Contains(entry.Descriptor.ReasoningCapabilities.Choices, selection.ReasoningChoice) {
		return Entry{}, &SelectionError{Code: ErrorCodeReasoningUnsupported, cause: nil}
	}
	if err := validateCompletionCredentials(ctx, entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

// validateCompletionCredentials checks the exact configured entry without changing active selection.
func validateCompletionCredentials(ctx context.Context, entry Entry) error {
	if entry.SelectionCredentialValidator != nil {
		if err := entry.SelectionCredentialValidator.ValidateSelectionCredentials(ctx); err != nil {
			return &SelectionError{Code: ErrorCodeCredentialUnavailable, cause: err}
		}
	}
	if entry.Authentication != nil {
		if err := entry.Authentication.CheckProviderAuthentication(ctx); err != nil {
			return &SelectionError{Code: ErrorCodeCredentialUnavailable, cause: err}
		}
	}
	return nil
}
