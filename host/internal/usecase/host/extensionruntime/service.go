// Package extensionruntime owns extension process registration, invocation, and availability.
package extensionruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// Service owns extension processes and their runtime availability.
type Service struct {
	// catalog discovers executable extension candidates.
	catalog Catalog
	// factory starts extension runtimes.
	factory RuntimeFactory
	// reportFailure publishes extension availability failures.
	reportFailure func(context.Context, extension.RuntimeFailure) error
	// mutex protects runtime state.
	mutex sync.RWMutex
	// runtimes contains extension runtime state by plugin ID.
	runtimes map[string]*runtimeState
	// monitoring reports whether runtime exit monitors are active.
	monitoring bool
	// closing reports whether service shutdown has started.
	closing bool
}

var (
	_ startup.RuntimeLoader = (*Service)(nil)
	_ toolservice.Runtime   = (*Service)(nil)
	_ sessiontree.Runtime   = (*Service)(nil)
)

// runtimeState contains one extension process and its availability state.
type runtimeState struct {
	// runtime owns the extension process connection.
	runtime ExtensionRuntime
	// available reports whether the runtime accepts operations.
	available bool
	// activeExecutions counts in-flight tool and handler operations.
	activeExecutions int
	// exitPending reports a runtime exit awaiting active operations.
	exitPending bool
}

// operationOwner identifies one runtime involved in an active operation.
type operationOwner struct {
	// pluginID identifies the extension.
	pluginID string
	// state points to the owning runtime state.
	state *runtimeState
}

// New creates the Host extension runtime service.
func New(
	catalog Catalog,
	factory RuntimeFactory,
	reportFailure func(context.Context, extension.RuntimeFailure) error,
) *Service {
	return &Service{
		catalog:       catalog,
		factory:       factory,
		reportFailure: reportFailure,
		mutex:         sync.RWMutex{},
		runtimes:      make(map[string]*runtimeState),
		monitoring:    false,
		closing:       false,
	}
}

// Activate starts post-start runtime observation after the user surface is ready.
func (s *Service) Activate(ctx context.Context) {
	monitorContext := context.WithoutCancel(ctx)
	s.mutex.Lock()
	if s.monitoring || s.closing {
		s.mutex.Unlock()
		return
	}
	s.monitoring = true
	observed := make(map[string]*runtimeState, len(s.runtimes))
	for pluginID, state := range s.runtimes {
		if state.available {
			observed[pluginID] = state
		}
	}
	s.mutex.Unlock()
	for pluginID, state := range observed {
		go s.monitor(monitorContext, pluginID, state, state.runtime.Done())
	}
}

// LoadPending discovers, starts, and registers runtimes without making them available.
func (s *Service) LoadPending(ctx context.Context, directory startup.Directory) (startup.PendingLoad, error) {
	discovery, err := s.catalog.Discover(ctx, Directory{Path: directory.Path, Explicit: directory.Explicit})
	if err != nil {
		return startup.PendingLoad{}, fmt.Errorf("discover extensions: %w", err)
	}
	issues := make([]startup.Issue, 0, len(discovery.Issues))
	for _, issue := range discovery.Issues {
		issues = append(
			issues,
			startup.Issue{PluginIDs: slices.Clone(issue.PluginIDs), Path: issue.Path, Err: issue.Err},
		)
	}
	registrations := make([]startup.PendingRegistration, 0, len(discovery.Candidates))
	for _, candidate := range discovery.Candidates {
		runtime, startErr := s.factory.Start(ctx, candidate)
		if startErr != nil {
			issues = append(
				issues,
				startup.Issue{PluginIDs: []string{candidate.ID}, Path: candidate.Path, Err: startErr},
			)
			continue
		}
		registration, registerErr := runtime.Register(ctx)
		if registerErr != nil {
			runtime.Close()
			issues = append(
				issues,
				startup.Issue{PluginIDs: []string{candidate.ID}, Path: candidate.Path, Err: registerErr},
			)
			continue
		}
		registration.ID = candidate.ID
		registration.Path = candidate.Path
		s.mutex.Lock()
		s.runtimes[candidate.ID] = &runtimeState{
			runtime:          runtime,
			available:        false,
			activeExecutions: 0,
			exitPending:      false,
		}
		s.mutex.Unlock()
		registrations = append(registrations, registration)
	}
	return startup.PendingLoad{Issues: issues, Registrations: registrations}, nil
}

// RejectPending closes rejected runtimes without reporting a post-start failure.
func (s *Service) RejectPending(pluginIDs []string) {
	states := make([]*runtimeState, 0, len(pluginIDs))
	s.mutex.Lock()
	for _, pluginID := range pluginIDs {
		if state, exists := s.runtimes[pluginID]; exists {
			delete(s.runtimes, pluginID)
			states = append(states, state)
		}
	}
	s.mutex.Unlock()
	for _, state := range states {
		state.runtime.Close()
	}
}

// Accept marks fully validated pending runtimes available.
func (s *Service) Accept(registrations []startup.AcceptedRegistration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, registration := range registrations {
		if state, exists := s.runtimes[registration.ID]; exists {
			state.available = true
		}
	}
}

// ToolRuntimeAvailable reports whether one accepted extension can execute tools.
func (s *Service) ToolRuntimeAvailable(extensionID string) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	state, exists := s.runtimes[extensionID]
	return exists && state.available
}

// ExecuteTool invokes one tool while retaining runtime active-operation accounting.
func (s *Service) ExecuteTool(
	ctx context.Context,
	extensionID, name string,
	argumentsJSON []byte,
	handleProgress tool.ProgressHandler,
) (tool.Result, error) {
	owner, available := s.beginOperation(extensionID)
	if !available {
		return tool.Result{}, fmt.Errorf("%w: extension tool %q is unavailable", ErrExtensionUnavailable, name)
	}
	result, executeErr := owner.state.runtime.Execute(ctx, name, argumentsJSON, handleProgress)
	s.finishAndReport(ctx, owner, executeErr)
	return result, executeErr
}

// HandlerRuntimeAvailable reports whether one accepted extension can handle operations.
func (s *Service) HandlerRuntimeAvailable(extensionID string) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	state, exists := s.runtimes[extensionID]
	return exists && state.available
}

// HandleHandler invokes one handler while retaining runtime active-operation accounting.
func (s *Service) HandleHandler(
	ctx context.Context,
	extensionID string,
	handlerID string,
	request sessiontree.HandlerRequest,
) (sessiontree.HandlerResponse, error) {
	owner, available := s.beginOperation(extensionID)
	if !available {
		return sessiontree.HandlerResponse{}, fmt.Errorf(
			"%w: extension handler %q is unavailable",
			ErrExtensionUnavailable,
			handlerID,
		)
	}
	response, handleErr := owner.state.runtime.Handle(ctx, handlerID, request)
	s.finishAndReport(ctx, owner, handleErr)
	return response, handleErr
}

// beginOperation accounts for one invocation when its runtime is available.
func (s *Service) beginOperation(pluginID string) (operationOwner, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	state, exists := s.runtimes[pluginID]
	if !exists || !state.available {
		return operationOwner{pluginID: pluginID, state: state}, false
	}
	state.activeExecutions++
	return operationOwner{pluginID: pluginID, state: state}, true
}

// Close stops every runtime without reporting planned shutdown.
func (s *Service) Close() {
	s.mutex.Lock()
	s.closing = true
	states := make([]*runtimeState, 0, len(s.runtimes))
	for _, state := range s.runtimes {
		state.exitPending = false
		if s.disableLocked(state) {
			states = append(states, state)
		}
	}
	s.mutex.Unlock()
	for _, state := range states {
		state.runtime.Close()
	}
}

// monitor marks an idle process exit unavailable and reports it once.
func (s *Service) monitor(ctx context.Context, pluginID string, state *runtimeState, done <-chan struct{}) {
	<-done
	s.mutex.Lock()
	if s.closing || !s.disableLocked(state) {
		s.mutex.Unlock()
		return
	}
	reportFailure := state.activeExecutions == 0
	state.exitPending = !reportFailure
	s.mutex.Unlock()
	if reportFailure {
		s.report(
			ctx,
			extension.RuntimeFailure{PluginID: pluginID, Condition: extension.RuntimeUnavailableProcessExited},
		)
	}
	state.runtime.Close()
}

// finishAndReport settles active-operation accounting and runtime failure delivery.
func (s *Service) finishAndReport(ctx context.Context, owner operationOwner, executeErr error) {
	closeRuntime, failure, reportFailure := s.finishExecution(owner, executeErr)
	if closeRuntime {
		owner.state.runtime.Close()
	}
	if reportFailure {
		s.report(ctx, failure)
	}
}

// finishExecution assigns process-exit presentation to the runtime reporter.
func (s *Service) finishExecution(owner operationOwner, executeErr error) (bool, extension.RuntimeFailure, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	owner.state.activeExecutions--
	if errors.Is(executeErr, ErrExtensionUnavailable) {
		availabilityChanged := s.disableLocked(owner.state)
		reportFailure := !s.closing && (owner.state.exitPending || availabilityChanged)
		owner.state.exitPending = false
		if reportFailure {
			return availabilityChanged, extension.RuntimeFailure{
				PluginID:  owner.pluginID,
				Condition: extension.RuntimeUnavailableProcessExited,
			}, true
		}
		return availabilityChanged, extension.RuntimeFailure{}, false
	}
	if owner.state.exitPending && !s.closing {
		owner.state.exitPending = false
		return false, extension.RuntimeFailure{
			PluginID:  owner.pluginID,
			Condition: extension.RuntimeUnavailableProcessExited,
		}, true
	}
	return false, extension.RuntimeFailure{}, false
}

// disableLocked removes one runtime from availability.
func (s *Service) disableLocked(state *runtimeState) bool {
	if !state.available {
		return false
	}
	state.available = false
	return true
}

// report forwards one classified runtime failure and logs delivery failure without retry.
func (s *Service) report(ctx context.Context, failure extension.RuntimeFailure) {
	if err := s.reportFailure(ctx, failure); err != nil {
		slog.ErrorContext(
			ctx,
			"report extension runtime failure",
			"plugin_id",
			failure.PluginID,
			"condition",
			failure.Condition,
			"error",
			err,
		)
	}
}
