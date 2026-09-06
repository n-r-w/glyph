package ui

import (
	"sync"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
)

// Session coordinates prepared Host operations for one UI connection.
type Session struct {
	// output projects application state and binds operation-scoped progress.
	output Output
	// runner prepares and executes Agent Core runs.
	runner AgentRunner
	// authenticator manages provider authentication.
	authenticator Authenticator
	// modelCatalog owns configured models and the active selection.
	modelCatalog ModelCatalog
	// activeSessions owns active-session lifecycle operations.
	activeSessions ActiveSessions
	// navigator owns handler policy and navigation commit orchestration.
	navigator Navigator
	// gate owns admission against agent execution.
	gate Gate
	// runtime starts monitoring after initialized output owns its writer and failures.
	runtime RuntimeActivation
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
	activeSessions ActiveSessions,
	navigator Navigator,
	gate Gate,
	runtime RuntimeActivation,
) *Session {
	return &Session{
		output:         output,
		runner:         runner,
		authenticator:  authenticator,
		modelCatalog:   modelCatalog,
		gate:           gate,
		activeSessions: activeSessions, navigator: navigator,
		runtime:               runtime,
		operationMutex:        sync.Mutex{},
		operationAvailability: AvailabilityCheckingAuthentication,
		selectionActive:       false,
	}
}
