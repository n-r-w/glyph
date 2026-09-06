package ui

import (
	"context"
	"sync"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
)

// Session coordinates prepared Host operations for one UI connection.
type Session struct {
	// output projects application state and binds operation-scoped progress.
	output Output
	// initialization is the assembled startup state for this session.
	initialization Initialization
	// runner prepares and executes Agent Core runs.
	runner AgentRunner
	// authenticator manages provider authentication.
	authenticator Authenticator
	// modelCatalog owns configured models and the active selection.
	modelCatalog ModelCatalog
	// sessionControl owns active-session lifecycle operations.
	sessionControl SessionControl
	// afterInitialization starts work that requires a connected UI.
	afterInitialization func(context.Context)
	// operationMutex protects readiness and operation-specific reservations.
	operationMutex sync.Mutex
	// operationAvailability controls operation admission.
	operationAvailability Availability
	// selectionActive serializes model-selection commits.
	selectionActive bool
}

var _ controllerui.Session = (*Session)(nil)

// NewSession creates one prepared Host UI session.
func NewSession(
	output Output,
	runner AgentRunner,
	authenticator Authenticator,
	modelCatalog ModelCatalog,
	sessionControl SessionControl,
	afterInitialization func(context.Context),
	initialization Initialization,
) *Session {
	return &Session{
		output:                output,
		initialization:        initialization,
		runner:                runner,
		authenticator:         authenticator,
		modelCatalog:          modelCatalog,
		sessionControl:        sessionControl,
		afterInitialization:   afterInitialization,
		operationMutex:        sync.Mutex{},
		operationAvailability: AvailabilityCheckingAuthentication,
		selectionActive:       false,
	}
}
