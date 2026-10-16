package compaction

import (
	"errors"

	"github.com/samber/mo"
)

// Model describes the configured summary or conversation model.
type Model struct {
	// Provider identifies the configured provider.
	Provider string
	// ID identifies the configured model.
	ID string
	// Window is the combined input and output capacity.
	Window int64
	// Reserve is the generated-output capacity.
	Reserve int64
	// Modalities lists accepted input kinds.
	Modalities []Modality
	// Reasoning contains the available reasoning controls.
	Reasoning mo.Option[Reasoning]
	// Tools contains constrained-generation capabilities.
	Tools mo.Option[ToolCapabilities]
	// Pricing contains configured token rates.
	Pricing mo.Option[Pricing]
}

// Validate checks model selection and capacity.
func (m Model) Validate() error {
	if m.Provider == "" || m.ID == "" || m.Window <= 0 || m.Reserve <= 0 || m.Reserve > m.Window {
		return errors.New("invalid compaction summary model")
	}
	return nil
}

// Reasoning describes model reasoning controls.
type Reasoning struct {
	// Supported indicates reasoning control availability.
	Supported bool
	// Choices contains the configured choices.
	Choices []string
	// Default is the configured default choice.
	Default string
}

// ToolCapabilities describes constrained generation.
type ToolCapabilities struct {
	// Strict indicates whether strict JSON schema is available.
	Strict bool
	// Lark indicates whether Lark grammars are available.
	Lark bool
	// Regex indicates whether regular-expression grammars are available.
	Regex bool
}

// Pricing contains model rates and request-wide tiers.
type Pricing struct {
	// Rates contains input, output, cache-read, and cache-write rates.
	Rates [4]float64
	// Tiers contains pricing overrides.
	Tiers []Tier
}

// Tier contains one request-wide pricing override.
type Tier struct {
	// Above is the exclusive input threshold.
	Above int64
	// Rates contains the replacement rates.
	Rates [4]float64
}
