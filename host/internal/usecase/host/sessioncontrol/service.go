package sessioncontrol

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// Service orchestrates client session lifecycle operations.
type Service struct {
	// active performs storage work and commits the replacement snapshot.
	active ActiveSessions
	// navigator commits internal tree navigation and optional built-in summaries.
	navigator Navigator
	// tryAcquire reserves session mutation against agent execution.
	tryAcquire func() (release func(), acquired bool)
}

var (
	_ hostprogrammatic.SessionControl = (*Service)(nil)
	_ hostui.SessionControl           = (*Service)(nil)
)

// New creates a session-control orchestrator.
func New(
	active ActiveSessions,
	navigator Navigator,
	tryAcquire func() (release func(), acquired bool),
) *Service {
	return &Service{active: active, navigator: navigator, tryAcquire: tryAcquire}
}

// TryAcquire reserves session mutation ownership for any internal transport caller.
func (s *Service) TryAcquire() (func(), bool) {
	return s.tryAcquire()
}

// Create replaces the active session under the caller-owned reservation.
func (s *Service) Create(ctx context.Context) (session.Replacement, error) {
	return s.active.CreateActive(ctx)
}

// Resume loads and replaces the active session under the caller-owned reservation.
func (s *Service) Resume(ctx context.Context, id session.ID) (session.Replacement, error) {
	return s.active.ResumeActive(ctx, id)
}

// Fork persists a replacement under the caller-owned reservation.
func (s *Service) Fork(ctx context.Context, targetID string) (session.Replacement, string, error) {
	return s.active.ForkActive(ctx, targetID)
}

// Clone persists an active-branch copy under the caller-owned reservation.
func (s *Service) Clone(ctx context.Context) (session.Replacement, error) {
	return s.active.CloneActive(ctx)
}

// SetLabel persists one entry label under the caller-owned reservation.
func (s *Service) SetLabel(ctx context.Context, targetID, label string) (session.Tree, error) {
	return s.active.SetLabel(ctx, targetID, label)
}

// Navigate commits tree navigation under the caller-owned reservation.
func (s *Service) Navigate(
	ctx context.Context,
	request sessionnavigation.Request,
) (sessionnavigation.Result, error) {
	return s.navigator.NavigateTree(ctx, request)
}

// SetName updates the active session under the caller-owned reservation.
func (s *Service) SetName(ctx context.Context, name string) (session.Info, error) {
	return s.active.SetActiveName(ctx, name)
}

// List returns stored session summaries.
func (s *Service) List(ctx context.Context) ([]session.Summary, error) {
	return s.active.ListStored(ctx)
}

// Info returns active session information.
func (s *Service) Info() session.Info { return s.active.ActiveInfo() }

// Entries returns immutable active-branch records while runs continue.
func (s *Service) Entries() []session.Entry { return s.active.ActiveEntries() }

// Tree returns an independent active-session tree snapshot.
func (s *Service) Tree() session.Tree { return s.active.Tree() }

// Statistics returns active-session counts and complete token totals.
func (s *Service) Statistics() session.Statistics { return s.active.ActiveStatistics() }

// Information returns metadata and statistics from one locked active snapshot.
func (s *Service) Information() session.InformationSnapshot { return s.active.ActiveInformation() }
