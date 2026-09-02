package ui

import (
	"context"
	"sync"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// Session coordinates prepared Host operations for one UI connection.
type Session struct {
	// channel owns the UI operation stream.
	channel Channel
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
	operationAvailability domainui.Availability
	// selectionActive serializes model-selection commits.
	selectionActive bool
}

var _ controllerui.Session = (*Session)(nil)

// NewSession creates one prepared Host UI session.
func NewSession(
	channel Channel,
	runner AgentRunner,
	authenticator Authenticator,
	modelCatalog ModelCatalog,
	sessionControl SessionControl,
	afterInitialization func(context.Context),
) *Session {
	return &Session{
		channel: channel, runner: runner, authenticator: authenticator, modelCatalog: modelCatalog,
		sessionControl: sessionControl, afterInitialization: afterInitialization, operationMutex: sync.Mutex{},
		operationAvailability: domainui.AvailabilityCheckingAuthentication, selectionActive: false,
	}
}

// sendAvailability delivers one connection-level availability change.
func (s *Session) sendAvailability(availability domainui.Availability) error {
	return s.channel.Send(lifecycleFrame(availabilityLifecycle(availability)))
}
