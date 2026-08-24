// Package providers owns configured model providers and the active runtime selection.
package providers

import (
	"fmt"
	"sort"
	"sync"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// ErrorCode identifies a catalog selection failure.
type ErrorCode string

const (
	// ErrorCodeNotFound reports an unknown provider and model pair.
	ErrorCodeNotFound ErrorCode = "not_found"
	// ErrorCodeReasoningUnsupported reports a level not supported by the active model.
	ErrorCodeReasoningUnsupported ErrorCode = "reasoning_unsupported"
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
	Code           ErrorCode
	Provider       model.ProviderID
	Model          model.ID
	ReasoningLevel model.ReasoningLevel
}

// Error implements error.
func (e *SelectionError) Error() string {
	return fmt.Sprintf("provider catalog selection failed: %s", e.Code)
}

// Entry binds one configured model descriptor to its provider runtime.
type Entry struct {
	Descriptor model.Descriptor
	Provider   agentrun.ModelProvider
}

// Catalog stores immutable configured entries and one active selection.
type Catalog struct {
	entries []Entry

	mutex     sync.RWMutex
	selection model.Selection
	active    int
}

var _ agentrun.ModelRuntime = (*Catalog)(nil)

// New creates a catalog from configured entries and a valid default selection.
func New(entries []Entry, selection model.Selection) (*Catalog, error) {
	configured := make([]Entry, len(entries))
	seen := make(map[model.ProviderID]map[model.ID]struct{})
	for index, entry := range entries {
		entry.Descriptor = cloneDescriptor(entry.Descriptor)
		if entry.Provider == nil {
			return nil, invalidConfigurationError(model.Selection{
				Provider: entry.Descriptor.Provider, Model: entry.Descriptor.Model, ReasoningLevel: "",
			})
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
		return nil, invalidConfigurationError(selection)
	}
	if !supports(catalog.entries[active].Descriptor.SupportedReasoningLevels, selection.ReasoningLevel) {
		return nil, invalidConfigurationError(selection)
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
func (c *Catalog) SelectModel(provider model.ProviderID, modelID model.ID) (model.Selection, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	active, found := c.entryIndex(provider, modelID)
	if !found {
		return model.Selection{}, &SelectionError{
			Code: ErrorCodeNotFound, Provider: provider, Model: modelID, ReasoningLevel: "",
		}
	}
	level := fallbackReasoningLevel(
		c.selection.ReasoningLevel, c.entries[active].Descriptor.SupportedReasoningLevels,
	)
	c.selection = model.Selection{Provider: provider, Model: modelID, ReasoningLevel: level}
	c.active = active
	return c.selection, nil
}

// SelectReasoningLevel commits a supported level for the active model.
func (c *Catalog) SelectReasoningLevel(level model.ReasoningLevel) (model.Selection, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !supports(c.entries[c.active].Descriptor.SupportedReasoningLevels, level) {
		return model.Selection{}, &SelectionError{
			Code: ErrorCodeReasoningUnsupported, Provider: c.selection.Provider,
			Model: c.selection.Model, ReasoningLevel: level,
		}
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
	selection := model.Selection{Provider: descriptor.Provider, Model: descriptor.Model, ReasoningLevel: ""}
	if descriptor.Provider == "" || descriptor.Model == "" || len(descriptor.SupportedReasoningLevels) == 0 {
		return invalidConfigurationError(selection)
	}
	models, exists := seen[descriptor.Provider]
	if !exists {
		models = make(map[model.ID]struct{})
		seen[descriptor.Provider] = models
	}
	if _, exists = models[descriptor.Model]; exists {
		return invalidConfigurationError(selection)
	}
	models[descriptor.Model] = struct{}{}
	levels := make(map[model.ReasoningLevel]struct{}, len(descriptor.SupportedReasoningLevels))
	for _, level := range descriptor.SupportedReasoningLevels {
		if _, valid := reasoningRank(level); !valid {
			selection.ReasoningLevel = level
			return invalidConfigurationError(selection)
		}
		if _, duplicate := levels[level]; duplicate {
			selection.ReasoningLevel = level
			return invalidConfigurationError(selection)
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

func invalidConfigurationError(selection model.Selection) *SelectionError {
	return &SelectionError{
		Code: ErrorCodeInvalidConfiguration, Provider: selection.Provider, Model: selection.Model,
		ReasoningLevel: selection.ReasoningLevel,
	}
}
