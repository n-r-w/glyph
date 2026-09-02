// Package operationgate serializes agent runs with active-session replacement.
package operationgate

import "sync/atomic"

// Service is one process-local nonblocking operation gate.
type Service struct {
	// occupied has one owner at a time and never blocks a caller waiting for release.
	occupied atomic.Bool
}

// New creates an idle operation gate.
func New() *Service { return &Service{} }

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
