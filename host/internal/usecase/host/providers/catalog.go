// Package providers owns configured model providers and the active runtime selection.
package providers

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
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

// SelectionError reports a safe typed catalog failure.
type SelectionError struct {
	Code  ErrorCode
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
	Descriptor                   model.Descriptor
	Provider                     agentrun.ModelProvider
	SelectionCredentialValidator SelectionCredentialValidator
	Authentication               ProviderAuthentication
}

// Catalog stores immutable configured entries and one active selection.
type Catalog struct {
	entries []Entry

	mutex     sync.RWMutex
	selection model.Selection
	active    int
}

var _ hostprogrammatic.ModelCatalog = (*Catalog)(nil)
var _ hostprogrammatic.SelectionFailure = (*SelectionError)(nil)

var (
	_ agentrun.ModelRuntime = (*Catalog)(nil)
	_ hostui.Authenticator  = (*Catalog)(nil)
	_ hostui.ModelCatalog   = (*Catalog)(nil)
)

// New creates a catalog from configured entries and a valid default selection.
func New(entries []Entry, selection model.Selection) (*Catalog, error) {
	configured := make([]Entry, len(entries))
	seen := make(map[model.ProviderID]map[model.ID]struct{})
	for index, entry := range entries {
		entry.Descriptor = cloneDescriptor(entry.Descriptor)
		if entry.Provider == nil {
			return nil, invalidConfigurationError()
		}
		if err := validateDescriptor(entry.Descriptor, seen); err != nil {
			return nil, err
		}
		configured[index] = entry
	}
	sort.SliceStable(configured, func(left, right int) bool {
		return configured[left].Descriptor.Provider < configured[right].Descriptor.Provider
	})
	catalog := &Catalog{entries: configured, mutex: sync.RWMutex{}, selection: selection, active: 0}
	active, found := catalog.entryIndex(selection.Provider, selection.Model)
	if !found {
		return nil, invalidConfigurationError()
	}
	if !supports(catalog.entries[active].Descriptor.SupportedReasoningLevels, selection.ReasoningLevel) {
		return nil, invalidConfigurationError()
	}
	catalog.active = active
	return catalog, nil
}

// Models returns defensive configured model descriptors.
func (c *Catalog) Models() []model.Descriptor {
	models := make([]model.Descriptor, len(c.entries))
	for index, entry := range c.entries {
		models[index] = cloneDescriptor(entry.Descriptor)
	}
	return models
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
		Model:          cloneDescriptor(entry.Descriptor),
		ReasoningLevel: c.selection.ReasoningLevel,
		Provider:       entry.Provider,
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
	level := fallbackReasoningLevel(
		c.selection.ReasoningLevel, c.entries[target].Descriptor.SupportedReasoningLevels,
	)
	c.selection = model.Selection{Provider: provider, Model: modelID, ReasoningLevel: level}
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

// SelectReasoningLevel commits a supported level for the active model.
func (c *Catalog) SelectReasoningLevel(level model.ReasoningLevel) (model.Selection, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !supports(c.entries[c.active].Descriptor.SupportedReasoningLevels, level) {
		return model.Selection{}, &SelectionError{Code: ErrorCodeReasoningUnsupported, cause: nil}
	}
	c.selection.ReasoningLevel = level
	return c.selection, nil
}

func (c *Catalog) entryIndex(provider model.ProviderID, modelID model.ID) (int, bool) {
	for index, entry := range c.entries {
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
	if descriptor.Provider == "" || descriptor.Model == "" || len(descriptor.SupportedReasoningLevels) == 0 {
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
	levels := make(map[model.ReasoningLevel]struct{}, len(descriptor.SupportedReasoningLevels))
	for _, level := range descriptor.SupportedReasoningLevels {
		if _, valid := reasoningRank(level); !valid {
			return invalidConfigurationError()
		}
		if _, duplicate := levels[level]; duplicate {
			return invalidConfigurationError()
		}
		levels[level] = struct{}{}
	}
	return nil
}

// fallbackReasoningLevel preserves active effort or selects the nearest safe lower effort.
func fallbackReasoningLevel(active model.ReasoningLevel, supported []model.ReasoningLevel) model.ReasoningLevel {
	if supports(supported, active) {
		return active
	}
	activeRank, _ := reasoningRank(active)
	selected := supported[0]
	selectedRank, _ := reasoningRank(selected)
	foundLower := false
	for _, level := range supported {
		rank, _ := reasoningRank(level)
		if rank < activeRank && (!foundLower || rank > selectedRank) {
			selected = level
			selectedRank = rank
			foundLower = true
			continue
		}
		if !foundLower && rank < selectedRank {
			selected = level
			selectedRank = rank
		}
	}
	return selected
}

func supports(levels []model.ReasoningLevel, target model.ReasoningLevel) bool {
	for _, level := range levels {
		if level == target {
			return true
		}
	}
	return false
}

// reasoningRank defines the product ordering used by model-selection fallback.
func reasoningRank(level model.ReasoningLevel) (int, bool) {
	switch level {
	case model.ReasoningLevelNone:
		return reasoningRankNone, true
	case model.ReasoningLevelMinimal:
		return reasoningRankMinimal, true
	case model.ReasoningLevelLow:
		return reasoningRankLow, true
	case model.ReasoningLevelMedium:
		return reasoningRankMedium, true
	case model.ReasoningLevelHigh:
		return reasoningRankHigh, true
	case model.ReasoningLevelXHigh:
		return reasoningRankXHigh, true
	case model.ReasoningLevelMax:
		return reasoningRankMax, true
	default:
		return 0, false
	}
}

// cloneDescriptor keeps configured capability slices immutable to catalog callers.
func cloneDescriptor(descriptor model.Descriptor) model.Descriptor {
	descriptor.SupportedReasoningLevels = append(
		[]model.ReasoningLevel(nil), descriptor.SupportedReasoningLevels...,
	)
	return descriptor
}

func invalidConfigurationError() *SelectionError {
	return &SelectionError{Code: ErrorCodeInvalidConfiguration, cause: nil}
}
