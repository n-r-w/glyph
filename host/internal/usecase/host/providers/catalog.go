// Package providers owns configured model providers and the active runtime selection.
package providers

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// ErrorCode identifies a catalog selection failure.
type ErrorCode string

const (
	// ErrorCodeNotFound reports an unknown provider and model pair.
	ErrorCodeNotFound ErrorCode = "not_found"
	// ErrorCodeReasoningUnsupported reports a level not supported by the active model.
	ErrorCodeReasoningUnsupported ErrorCode = "reasoning_unsupported"
	// ErrorCodeCredentialUnavailable reports credentials that cannot resolve before selection.
	ErrorCodeCredentialUnavailable ErrorCode = "credential_unavailable" //nolint:gosec // This is an error code.
	// ErrorCodeInvalidConfiguration reports invalid catalog construction input.
	ErrorCodeInvalidConfiguration ErrorCode = "invalid_configuration"
)

const (
	reasoningRankNone = iota
	reasoningRankMinimal
	reasoningRankLow
	reasoningRankMedium
	reasoningRankHigh
	reasoningRankXHigh
	reasoningRankMax
)

// SelectionError reports a typed catalog failure.
type SelectionError struct {
	// Code classifies the catalog selection failure.
	Code ErrorCode
	// cause contains a secret-free validation failure.
	cause error
}

// Error implements error and includes only the validator's secret-free cause.
func (e *SelectionError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("provider catalog selection failed: %s: %v", e.Code, e.cause)
	}
	return fmt.Sprintf("provider catalog selection failed: %s", e.Code)
}

// SelectionCode returns the stable catalog failure code.
func (e *SelectionError) SelectionCode() string {
	return string(e.Code)
}

// Unwrap returns the secret-free credential validation cause when present.
func (e *SelectionError) Unwrap() error {
	return e.cause
}

// Entry binds one configured model descriptor to its provider runtime.
type Entry struct {
	// Descriptor contains configured model capabilities.
	Descriptor model.Descriptor
	// Provider executes requests for the configured model.
	Provider agentrun.ModelProvider
	// SelectionCredentialValidator validates credentials before selection.
	SelectionCredentialValidator SelectionCredentialValidator
	// Authentication handles provider sign-in and sign-out.
	Authentication ProviderAuthentication
}

// Catalog stores immutable configured entries and one active selection.
type Catalog struct {
	// entries contains immutable configured model bindings.
	entries []Entry

	// mutex protects selection and active.
	mutex sync.RWMutex
	// selection contains the active provider, model, and reasoning choice.
	selection model.Selection
	// active identifies the selected entry index.
	active int
}

var _ hostprogrammatic.ModelCatalog = (*Catalog)(nil)
var _ hostprogrammatic.SelectionFailure = (*SelectionError)(nil)
var (
	_ hostsessions.PricingCatalog = (*Catalog)(nil)
	_ sessiontree.ModelCompleter  = (*Catalog)(nil)
)

var (
	_ agentrun.ModelRuntime = (*Catalog)(nil)
	_ hostui.Authenticator  = (*Catalog)(nil)
	_ hostui.ModelCatalog   = (*Catalog)(nil)
)

// New creates a catalog from configured entries and a valid default selection.
func New(entries []Entry, selection model.Selection) (*Catalog, error) {
	configured := make([]Entry, len(entries))
	seen := make(map[model.ProviderID]map[model.ID]struct{})
	for index := range entries {
		entry := entries[index]
		entry.Descriptor = entry.Descriptor.Clone()
		if entry.Provider == nil {
			return nil, invalidConfigurationError()
		}
		if err := validateDescriptor(entry.Descriptor, seen); err != nil {
			return nil, err
		}
		configured[index] = entry
	}
	slices.SortStableFunc(configured, func(left, right Entry) int {
		return cmp.Compare(left.Descriptor.Provider, right.Descriptor.Provider)
	})
	catalog := &Catalog{entries: configured, mutex: sync.RWMutex{}, selection: selection, active: 0}
	active, found := catalog.entryIndex(selection.Provider, selection.Model)
	if !found {
		return nil, invalidConfigurationError()
	}
	if !slices.Contains(catalog.entries[active].Descriptor.ReasoningCapabilities.Choices, selection.ReasoningChoice) {
		return nil, invalidConfigurationError()
	}
	catalog.active = active
	return catalog, nil
}

// Models returns defensive configured model descriptors.
func (c *Catalog) Models() []model.Descriptor {
	models := make([]model.Descriptor, len(c.entries))
	for index := range c.entries {
		models[index] = c.entries[index].Descriptor.Clone()
	}
	return models
}

// Pricing returns a defensive price for one exact configured provider and requested model pair.
func (c *Catalog) Pricing(providerID model.ProviderID, modelID model.ID) mo.Option[model.Pricing] {
	entryIndex, found := c.entryIndex(providerID, modelID)
	if !found {
		return mo.None[model.Pricing]()
	}
	pricing, present := c.entries[entryIndex].Descriptor.Pricing.Get()
	if !present {
		return mo.None[model.Pricing]()
	}
	pricing.Tiers = slices.Clone(pricing.Tiers)
	return mo.Some(pricing)
}

// Selection returns the active immutable selection.
func (c *Catalog) Selection() model.Selection {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.selection
}

// Current returns a defensive request snapshot of the active runtime selection.
func (c *Catalog) Current() agentrun.RuntimeSelection {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	entry := c.entries[c.active]
	return agentrun.RuntimeSelection{
		Model:           entry.Descriptor.Clone(),
		ReasoningChoice: c.selection.ReasoningChoice,
		Provider:        entry.Provider,
	}
}

// SelectModel commits a configured model and resolves its reasoning fallback.
func (c *Catalog) SelectModel(
	ctx context.Context,
	provider model.ProviderID,
	modelID model.ID,
) (model.Selection, error) {
	target, found := c.entryIndex(provider, modelID)
	if !found {
		return model.Selection{}, &SelectionError{Code: ErrorCodeNotFound, cause: nil}
	}
	validator := c.entries[target].SelectionCredentialValidator
	if validator != nil {
		if err := validator.ValidateSelectionCredentials(ctx); err != nil {
			return model.Selection{}, &SelectionError{Code: ErrorCodeCredentialUnavailable, cause: err}
		}
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()
	level := fallbackReasoningChoice(
		c.selection.ReasoningChoice, c.entries[target].Descriptor.ReasoningCapabilities,
	)
	c.selection = model.Selection{Provider: provider, Model: modelID, ReasoningChoice: level}
	c.active = target
	return c.selection, nil
}

// CheckAuthentication checks authentication for the active provider.
func (c *Catalog) CheckAuthentication(ctx context.Context) error {
	authentication := c.activeAuthentication()
	if authentication == nil {
		return nil
	}
	return authentication.CheckProviderAuthentication(ctx)
}

// SignIn starts authentication for the active provider.
func (c *Catalog) SignIn(ctx context.Context) error {
	authentication := c.activeAuthentication()
	if authentication == nil {
		return nil
	}
	return authentication.SignInProvider(ctx)
}

// IsSignInRequired classifies an active-provider authentication error.
func (c *Catalog) IsSignInRequired(err error) bool {
	authentication := c.activeAuthentication()
	return authentication != nil && authentication.IsProviderSignInRequired(err)
}

func (c *Catalog) activeAuthentication() ProviderAuthentication {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.entries[c.active].Authentication
}

// SelectReasoningChoice commits a supported choice for the active model.
func (c *Catalog) SelectReasoningChoice(level model.ReasoningChoice) (model.Selection, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !slices.Contains(c.entries[c.active].Descriptor.ReasoningCapabilities.Choices, level) {
		return model.Selection{}, &SelectionError{Code: ErrorCodeReasoningUnsupported, cause: nil}
	}
	c.selection.ReasoningChoice = level
	return c.selection, nil
}

func (c *Catalog) entryIndex(provider model.ProviderID, modelID model.ID) (int, bool) {
	for index := range c.entries {
		entry := &c.entries[index]
		if entry.Descriptor.Provider == provider && entry.Descriptor.Model == modelID {
			return index, true
		}
	}
	return 0, false
}

// validateDescriptor rejects ambiguous entries before they can become selectable.
func validateDescriptor(
	descriptor model.Descriptor,
	seen map[model.ProviderID]map[model.ID]struct{},
) error {
	if !descriptor.Valid() {
		return invalidConfigurationError()
	}
	models, exists := seen[descriptor.Provider]
	if !exists {
		models = make(map[model.ID]struct{})
		seen[descriptor.Provider] = models
	}
	if _, exists = models[descriptor.Model]; exists {
		return invalidConfigurationError()
	}
	models[descriptor.Model] = struct{}{}
	return nil
}

// fallbackReasoningChoice preserves exact choices and applies the configured cross-model fallback.
func fallbackReasoningChoice(active model.ReasoningChoice, target model.ReasoningCapabilities) model.ReasoningChoice {
	if slices.Contains(target.Choices, active) {
		return active
	}
	if active == model.ReasoningChoiceOff {
		return target.Default
	}
	if active == model.ReasoningChoiceOn {
		return target.Default
	}
	if active.Effort() {
		if slices.Contains(target.Choices, model.ReasoningChoiceOn) {
			return model.ReasoningChoiceOn
		}
		activeRank, _ := reasoningRank(active)
		selected := target.Default
		bestDistance := int(^uint(0) >> 1)
		bestRank := bestDistance
		for _, choice := range target.Choices {
			rank, effort := reasoningRank(choice)
			if !effort {
				continue
			}
			distance := rank - activeRank
			if distance < 0 {
				distance = -distance
			}
			if distance < bestDistance || distance == bestDistance && rank < bestRank {
				selected, bestDistance, bestRank = choice, distance, rank
			}
		}
		return selected
	}
	return target.Default
}

// reasoningRank defines the product ordering used by model-selection fallback.
func reasoningRank(level model.ReasoningChoice) (int, bool) {
	switch level {
	case model.ReasoningChoiceOff, model.ReasoningChoiceOn:
		return 0, false
	case model.ReasoningChoiceMinimal:
		return reasoningRankMinimal, true
	case model.ReasoningChoiceLow:
		return reasoningRankLow, true
	case model.ReasoningChoiceMedium:
		return reasoningRankMedium, true
	case model.ReasoningChoiceHigh:
		return reasoningRankHigh, true
	case model.ReasoningChoiceXHigh:
		return reasoningRankXHigh, true
	case model.ReasoningChoiceMax:
		return reasoningRankMax, true
	default:
		return 0, false
	}
}

func invalidConfigurationError() *SelectionError {
	return &SelectionError{Code: ErrorCodeInvalidConfiguration, cause: nil}
}
