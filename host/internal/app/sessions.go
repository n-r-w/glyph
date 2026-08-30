package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	"github.com/n-r-w/glyph/host/internal/infra/persistence/sessionfilesystem"
	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
	"github.com/n-r-w/glyph/host/internal/infra/sessionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/operationgate"
	"github.com/n-r-w/glyph/host/internal/usecase/host/providers"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// sessionComposition keeps one active-session service and one shared operation gate per Host process.
type sessionComposition struct {
	// active owns the session snapshot initialized before any client starts.
	active *hostsessions.Service
	// control exposes lifecycle operations to client transports.
	control *sessioncontrol.Service
	// gate serializes session replacement with agent execution across all client paths.
	gate *operationgate.Service
	// pricing binds the provider catalog after storage initialization and before client execution.
	pricing *pricingCatalogBinding
}

// pricingCatalogBinding preserves storage-first startup while binding the UI-dependent provider catalog once.
type pricingCatalogBinding struct {
	// catalog is the provider catalog bound after storage initialization.
	catalog *providers.Catalog
}

var _ hostsessions.PricingCatalog = (*pricingCatalogBinding)(nil)

// Bind completes application assembly before Agent Core or any client can append a model response.
func (b *pricingCatalogBinding) Bind(catalog *providers.Catalog) {
	if b.catalog != nil || catalog == nil {
		panic("pricing catalog binding must be completed exactly once")
	}
	b.catalog = catalog
}

// Pricing delegates exact provider-model lookup after application assembly is complete.
func (b *pricingCatalogBinding) Pricing(providerID model.ProviderID, modelID model.ID) mo.Option[model.Pricing] {
	if b.catalog == nil {
		panic("pricing catalog lookup before application assembly")
	}
	return b.catalog.Pricing(providerID, modelID)
}

// newSessionComposition prepares project storage before providers, clients, or agent runs start.
func newSessionComposition(ctx context.Context, paths persistence.Paths) (sessionComposition, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return sessionComposition{}, fmt.Errorf("get working directory: %w", err)
	}
	canonical, err := sessionstore.CanonicalWorkingDirectory(workingDirectory)
	if err != nil {
		return sessionComposition{}, err
	}
	repository := sessionstore.New(
		filepath.Join(paths.Directory, "sessions"), canonical, sessionfilesystem.New(),
	)
	pricing := &pricingCatalogBinding{catalog: nil}
	active := hostsessions.New(
		repository, sessionruntime.CryptoIDGenerator{}, sessionruntime.SystemClock{}, pricing, canonical,
	)
	if initializeErr := active.Initialize(ctx); initializeErr != nil {
		return sessionComposition{}, initializeErr
	}
	gate := operationgate.New()
	return sessionComposition{
		active:  active,
		control: sessioncontrol.New(active, sessiontree.New(active), gate),
		gate:    gate,
		pricing: pricing,
	}, nil
}
