// Package providers owns the startup-selected provider catalog.
package providers

import (
	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// Catalog contains the one provider selected during Host startup.
type Catalog struct {
	descriptor model.Descriptor
	provider   agentrun.ModelProvider
}

// New creates the immutable startup provider catalog.
func New(descriptor model.Descriptor, provider agentrun.ModelProvider) *Catalog {
	return &Catalog{descriptor: descriptor, provider: provider}
}

// Models returns startup model descriptors for Host inspection.
func (c *Catalog) Models() []model.Descriptor { return []model.Descriptor{c.descriptor} }

// Provider returns the startup-selected provider.
func (c *Catalog) Provider() agentrun.ModelProvider { return c.provider }
