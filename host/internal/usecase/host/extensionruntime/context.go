package extensionruntime

import (
	"context"
	"fmt"
	"sync"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

const (
	// staleRuntimeContextCode identifies references to replaced or unavailable process instances.
	staleRuntimeContextCode = "STALE_CONTEXT"
)

var (
	_ extensioncontext.RuntimeState         = (*Service)(nil)
	_ extensioncontroller.RuntimeOperations = (*Service)(nil)
)

// runtimeContextError classifies runtime admission failure without losing its cause.
type runtimeContextError struct {
	// cause identifies the unavailable runtime reference.
	cause error
}

var _ extensioncontroller.ContextFailure = (*runtimeContextError)(nil)

// Error preserves the complete runtime reference failure.
func (e *runtimeContextError) Error() string { return e.cause.Error() }

// Unwrap exposes the admission cause.
func (e *runtimeContextError) Unwrap() error { return e.cause }

// ContextCode exposes the closed stale-binding category.
func (e *runtimeContextError) ContextCode() string { return staleRuntimeContextCode }

// ContextRuntime returns the current process incarnation and its acceptance state.
func (s *Service) ContextRuntime(extensionID string) (string, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	state, found := s.runtimes[extensionID]
	if !found {
		return "", false
	}
	return state.instanceID, state.available && !s.closing
}

// BeginContextOperation accounts for one extension-initiated operation on the exact process instance.
func (s *Service) BeginContextOperation(ctx context.Context, extensionID, runtimeID string) (func(), error) {
	s.mutex.Lock()
	state, found := s.runtimes[extensionID]
	if !found || !state.available || s.closing || state.instanceID != runtimeID {
		s.mutex.Unlock()
		return nil, &runtimeContextError{
			cause: fmt.Errorf("extension %q runtime instance %q is stale or unavailable", extensionID, runtimeID),
		}
	}
	state.activeExecutions++
	state.work.Add(1)
	s.mutex.Unlock()
	owner := operationOwner{pluginID: extensionID, state: state}
	var once sync.Once
	return func() { once.Do(func() { s.finishAndReport(ctx, owner, nil) }) }, nil
}
