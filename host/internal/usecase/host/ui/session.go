package ui

import (
	"errors"
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
	// modelCatalog supplies configured models and the active selection snapshot.
	modelCatalog ModelCatalog
	// modelSelection owns shared selection admission and commit.
	modelSelection ModelSelection
	// retryControl owns runtime retry enablement and policy projection.
	retryControl RetryControl
	// compactor owns manual active-conversation compaction.
	compactor Compactor
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
	modelSelection ModelSelection,
	retryControl RetryControl,
) *Session {
	return &Session{
		output:         output,
		runner:         runner,
		authenticator:  authenticator,
		modelCatalog:   modelCatalog,
		modelSelection: modelSelection,
		retryControl:   retryControl,
		compactor:      nil,
		gate:           gate,
		activeSessions: activeSessions, navigator: navigator,
		runtime:               runtime,
		operationMutex:        sync.Mutex{},
		operationAvailability: AvailabilityCheckingAuthentication,
	}
}

// BindCompactor connects manual compaction before the UI session accepts commands.
func (s *Session) BindCompactor(compactor Compactor) error {
	if s.compactor != nil {
		return errors.New("UI compactor is already bound")
	}
	if compactor == nil {
		return errors.New("UI compactor is required")
	}
	s.compactor = compactor
	return nil
}
