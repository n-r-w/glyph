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

// CheckAvailability resolves one exact selection and its credentials without changing active selection.
func (c *Catalog) CheckAvailability(ctx context.Context, selection model.Selection) error {
	_, err := c.resolveRequestEntry(ctx, selection)
	return err
}

// Request executes one model request and returns its terminal response without changing active selection.
func (c *Catalog) Request(
	ctx context.Context,
	selection model.Selection,
	instructions string,
	history []agent.HistoryEntry,
) (model.Response, error) {
	entry, err := c.resolveRequestEntry(ctx, selection)
	if err != nil {
		return model.Response{}, err
	}
	ownedHistory := make([]agent.HistoryEntry, len(history))
	for index := range history {
		clone, cloneErr := history[index].ValidatedClone()
		if cloneErr != nil {
			return model.Response{}, fmt.Errorf("validate model request history: %w", cloneErr)
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
				return errors.New("model request terminal event has no response")
			}
			terminal = mo.Some(response.Clone())
		}
		return nil
	})
	if streamErr != nil {
		return model.Response{}, fmt.Errorf("execute model request: %w", streamErr)
	}
	response, present := terminal.Get()
	if !present {
		return model.Response{}, errors.New("model request ended without a terminal response")
	}
	return response, nil
}

// resolveRequestEntry resolves one exact selection and checks its request credentials.
func (c *Catalog) resolveRequestEntry(ctx context.Context, selection model.Selection) (Entry, error) {
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
	if err := checkRequestCredentials(ctx, entry); err != nil {
		return Entry{}, err
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
