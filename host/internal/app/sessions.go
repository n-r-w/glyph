package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
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
	// modelRequester binds model requests after provider construction.
	modelRequester *modelRequesterBinding
	// tree owns session-tree handler registrations and navigation policy.
	tree *sessiontree.Service
}

// modelRequesterBinding preserves storage-first startup while binding model requests once.
type modelRequesterBinding struct {
	// catalog is the provider catalog bound after storage initialization.
	catalog *providers.Catalog
}

// Bind completes model execution assembly before client commands start.
func (b *modelRequesterBinding) Bind(catalog *providers.Catalog) {
	if b.catalog != nil || catalog == nil {
		panic("model catalog binding must be completed exactly once")
	}
	b.catalog = catalog
}

// ActiveSelection returns the active model selection.
func (b *modelRequesterBinding) ActiveSelection() model.Selection {
	if b.catalog == nil {
		panic("active model selection before application assembly")
	}
	return b.catalog.ActiveSelection()
}

// CheckAvailability checks one exact selection without model execution.
func (b *modelRequesterBinding) CheckAvailability(ctx context.Context, selection model.Selection) error {
	if b.catalog == nil {
		panic("model availability check before application assembly")
	}
	return b.catalog.CheckAvailability(ctx, selection)
}

// Request executes one model request without changing the active selection.
func (b *modelRequesterBinding) Request(
	ctx context.Context,
	selection model.Selection,
	instructions string,
	history []agent.HistoryEntry,
) (model.Response, error) {
	if b.catalog == nil {
		panic("model request before application assembly")
	}
	return b.catalog.Request(ctx, selection, instructions, history)
}

// pricingCatalogBinding preserves storage-first startup while binding the UI-dependent provider catalog once.
type pricingCatalogBinding struct {
	// catalog is the provider catalog bound after storage initialization.
	catalog *providers.Catalog
}

var (
	_ sessiontree.ModelRequester  = (*modelRequesterBinding)(nil)
	_ hostsessions.PricingCatalog = (*pricingCatalogBinding)(nil)
)

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
func newSessionComposition(
	ctx context.Context,
	paths persistence.Paths,
	handlerRuntime sessiontree.Runtime,
) (sessionComposition, error) {
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
	modelRequester := &modelRequesterBinding{catalog: nil}
	active := hostsessions.New(
		repository, sessionruntime.CryptoIDGenerator{}, sessionruntime.SystemClock{}, pricing, canonical,
	)
	if initializeErr := active.Initialize(ctx); initializeErr != nil {
		return sessionComposition{}, initializeErr
	}
	gate := operationgate.New()
	tree := sessiontree.New(active, modelRequester, handlerRuntime)
	return sessionComposition{
		active:         active,
		control:        sessioncontrol.New(active, tree, gate.TryAcquire),
		gate:           gate,
		pricing:        pricing,
		modelRequester: modelRequester,
		tree:           tree,
	}, nil
}
