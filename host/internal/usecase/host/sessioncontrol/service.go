package sessioncontrol

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// Service orchestrates client session lifecycle operations.
type Service struct {
	// active performs storage work and commits the replacement snapshot.
	active ActiveSessions
	// gate is shared with agent execution and is held through active replacement.
	gate OperationGate
}

var (
	_ hostprogrammatic.SessionControl = (*Service)(nil)
	_ hostui.SessionControl           = (*Service)(nil)
)

// New creates a session-control orchestrator.
func New(active ActiveSessions, gate OperationGate) *Service {
	return &Service{active: active, gate: gate}
}

// Create replaces the active session while holding the operation gate.
func (s *Service) Create(ctx context.Context) (session.Replacement, error) {
	release, acquired := s.gate.TryAcquire()
	if !acquired {
		return session.Replacement{}, session.ErrBusy
	}
	// The orchestrator owns release until active replacement returns on every terminal path.
	defer release()
	return s.active.CreateActive(ctx)
}

// Resume loads and replaces the active session while holding the operation gate.
func (s *Service) Resume(ctx context.Context, id session.ID) (session.Replacement, error) {
	release, acquired := s.gate.TryAcquire()
	if !acquired {
		return session.Replacement{}, session.ErrBusy
	}
	// Loading and validation remain inside the reservation so a run cannot observe partial replacement.
	defer release()
	return s.active.ResumeActive(ctx, id)
}

// SetName updates the active session without replacing it.
func (s *Service) SetName(ctx context.Context, name string) (session.Info, error) {
	return s.active.SetActiveName(ctx, name)
}

// List returns stored session summaries.
func (s *Service) List(ctx context.Context) ([]session.Summary, error) {
	return s.active.ListStored(ctx)
}

// Info returns active session information.
func (s *Service) Info() session.Info { return s.active.ActiveInfo() }

// Entries returns immutable active-session records while runs continue.
func (s *Service) Entries() []session.Entry { return s.active.ActiveEntries() }

// Statistics returns active-session counts and complete token totals.
func (s *Service) Statistics() session.Statistics { return s.active.ActiveStatistics() }

// Information returns metadata and statistics from one locked active snapshot.
func (s *Service) Information() session.InformationSnapshot { return s.active.ActiveInformation() }
