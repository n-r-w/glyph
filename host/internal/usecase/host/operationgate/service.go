// Package operationgate serializes agent runs with active-session replacement.
package operationgate

import (
	"sync/atomic"

	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// Service is one process-local nonblocking operation gate.
type Service struct {
	// occupied has one owner at a time and never blocks a caller waiting for release.
	occupied atomic.Bool
}

var (
	_ runcontrol.Gate   = (*Service)(nil)
	_ ui.Gate           = (*Service)(nil)
	_ programmatic.Gate = (*Service)(nil)
)

// TryAcquire reserves the gate and returns an idempotent release function.
func (s *Service) TryAcquire() (func(), bool) {
	if !s.occupied.CompareAndSwap(false, true) {
		return nil, false
	}
	// Each acquisition owns a separate release guard, so duplicate cleanup cannot release a later owner.
	var released atomic.Bool
	return func() {
		if released.CompareAndSwap(false, true) {
			s.occupied.Store(false)
		}
	}, true
}

// New creates an idle operation gate.
func New() *Service { return &Service{} }
