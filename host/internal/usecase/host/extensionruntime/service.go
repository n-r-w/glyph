// Package extensionruntime owns extension process registration, invocation, and availability.
package extensionruntime

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// Service owns extension processes and their runtime availability.
type Service struct {
	// catalog discovers executable extension candidates.
	catalog Catalog
	// factory starts extension runtimes.
	factory RuntimeFactory
	// reporter publishes extension availability failures.
	reporter FailureReporter
	// mutex protects runtime state.
	mutex sync.RWMutex
	// runtimes contains extension runtime state by plugin ID.
	runtimes map[string]*runtimeState
	// monitoring reports whether runtime exit monitors are active.
	monitoring bool
	// monitorContext retains mode-specific diagnostics after runtime monitoring is activated.
	monitorContext context.Context
	// closing reports whether service shutdown has started.
	closing bool
}

var (
	_ startup.RuntimeLoader    = (*Service)(nil)
	_ hostui.RuntimeActivation = (*Service)(nil)
	_ toolservice.Runtime      = (*Service)(nil)
	_ sessiontree.Runtime      = (*Service)(nil)
	_ lifecycle.Runtime        = (*Service)(nil)
)

// runtimeState contains one extension process and its availability state.
type runtimeState struct {
	// runtime owns the extension process connection.
	runtime ExtensionRuntime
	// instanceID distinguishes process replacements under the same extension identifier.
	instanceID string
	// available reports whether the runtime accepts operations.
	available bool
	// activeExecutions counts in-flight tool and handler operations.
	activeExecutions int
	// exitPending reports a runtime exit awaiting active operations.
	exitPending bool
	// work joins every operation reservation in both stream directions.
	work sync.WaitGroup
	// closeOnce closes process transport once across failure, replacement, and shutdown.
	closeOnce sync.Once
	// observed records whether a process-exit monitor has been started for this instance.
	observed bool
	// monitorDone closes after the process-exit monitor joins its owned work.
	monitorDone chan struct{}
	// commit protects final state-owner commits from runtime invalidation.
	commit sync.RWMutex
	// invalidated closes when runtime invalidation begins.
	invalidated chan struct{}
	// invalidateOnce closes invalidated at most once across failure paths.
	invalidateOnce sync.Once
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
	reporter FailureReporter,
) *Service {
	return &Service{
		catalog:        catalog,
		factory:        factory,
		reporter:       reporter,
		mutex:          sync.RWMutex{},
		runtimes:       make(map[string]*runtimeState),
		monitoring:     false,
		monitorContext: nil,
		closing:        false,
	}
}

// Activate starts runtime observation after the selected client is ready.
func (s *Service) Activate(ctx context.Context) {
	monitorContext := context.WithoutCancel(ctx)
	s.mutex.Lock()
	if s.monitoring || s.closing {
		s.mutex.Unlock()
		return
	}
	s.monitoring = true
	s.monitorContext = monitorContext
	observed := make(map[string]*runtimeState, len(s.runtimes))
	for pluginID, state := range s.runtimes {
		if state.available && !state.observed {
			state.observed = true
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
	discovery, err := s.catalog.Discover(ctx, Directory{Path: directory.Path})
	if err == nil {
		discovery, err = s.acceptDiscovery(directory, discovery)
	}
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
	for _, observed := range discovery.Candidates {
		candidate := Candidate{ID: observed.ID, Path: observed.Path, InstanceID: s.replaceInstance(observed.ID)}
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
		pending := s.bindRegistration(candidate, registration)
		s.mutex.Lock()
		s.runtimes[candidate.ID] = &runtimeState{
			runtime:          runtime,
			instanceID:       candidate.InstanceID,
			available:        false,
			activeExecutions: 0,
			exitPending:      false,
			work:             sync.WaitGroup{},
			closeOnce:        sync.Once{},
			observed:         false,
			monitorDone:      make(chan struct{}),
			commit:           sync.RWMutex{},
			invalidated:      make(chan struct{}),
			invalidateOnce:   sync.Once{},
		}
		s.mutex.Unlock()
		registrations = append(registrations, pending)
	}
	return startup.PendingLoad{Issues: issues, Registrations: registrations}, nil
}

// replaceInstance invalidates and joins preceding work before its replacement process can start.
func (s *Service) replaceInstance(extensionID string) string {
	s.mutex.Lock()
	previous := s.runtimes[extensionID]
	if previous != nil {
		previous.invalidate()
	}
	s.mutex.Unlock()
	if previous != nil {
		previous.commit.Lock()
		s.mutex.Lock()
		if s.runtimes[extensionID] == previous {
			previous.available = false
			previous.exitPending = false
			delete(s.runtimes, extensionID)
		}
		s.mutex.Unlock()
		previous.commit.Unlock()
		previous.closeTransport()
		previous.join()
	}
	return rand.Text()
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
		state.closeTransport()
		state.join()
	}
}

// Accept marks fully validated pending runtimes available.
func (s *Service) Accept(registrations []startup.AcceptedRegistration) {
	s.mutex.Lock()
	if s.closing {
		s.mutex.Unlock()
		return
	}
	observed := make(map[string]*runtimeState)
	for _, registration := range registrations {
		if state, exists := s.runtimes[registration.ID]; exists && !state.isInvalidated() {
			state.available = true
			if s.monitoring && !state.observed {
				state.observed = true
				observed[registration.ID] = state
			}
		}
	}
	ctx := s.monitorContext
	s.mutex.Unlock()
	for extensionID, state := range observed {
		go s.monitor(ctx, extensionID, state, state.runtime.Done())
	}
}

// ToolRuntimeAvailable reports whether one accepted extension can execute tools.
func (s *Service) ToolRuntimeAvailable(extensionID string) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	state, exists := s.runtimes[extensionID]
	return exists && state.available && !state.isInvalidated()
}

// ExecuteTool invokes one tool while retaining runtime active-operation accounting.
func (s *Service) ExecuteTool(
	ctx context.Context,
	extensionID, name string,
	argumentsJSON []byte,
	handleProgress tool.ProgressHandler,
	binding extension.Context,
) (tool.Result, error) {
	owner, available := s.beginOperation(extensionID, binding.RuntimeInstanceID)
	if !available {
		return tool.Result{}, &runtimeContextError{
			cause: fmt.Errorf(
				"%w: extension tool %q runtime instance %q is unavailable",
				ErrExtensionUnavailable,
				name,
				binding.RuntimeInstanceID,
			),
		}
	}
	result, executeErr := owner.state.runtime.Execute(ctx, name, argumentsJSON, handleProgress, binding)
	s.finishAndReport(ctx, owner, executeErr)
	return result, executeErr
}

// HandlerRuntimeAvailable reports whether one accepted extension can handle operations.
func (s *Service) HandlerRuntimeAvailable(extensionID string) bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	state, exists := s.runtimes[extensionID]
	return exists && state.available && !state.isInvalidated()
}

// HandleHandler invokes one handler while retaining runtime active-operation accounting.
func (s *Service) HandleHandler(
	ctx context.Context,
	extensionID string,
	handlerID string,
	request sessiontree.HandlerRequest,
) (sessiontree.HandlerResponse, error) {
	owner, available := s.beginOperation(extensionID, request.Context.RuntimeInstanceID)
	if !available {
		return sessiontree.HandlerResponse{}, fmt.Errorf(
			"%w: extension handler %q is unavailable",
			ErrExtensionUnavailable,
			handlerID,
		)
	}
	payload, projectErr := s.projectHandler(request)
	if projectErr != nil {
		s.finishAndReport(ctx, owner, projectErr)
		return sessiontree.HandlerResponse{}, projectErr
	}
	payload.Context.ExtensionID = extensionID
	response, handleErr := owner.state.runtime.Handle(ctx, handlerID, payload)
	s.finishAndReport(ctx, owner, handleErr)
	if handleErr != nil {
		return sessiontree.HandlerResponse{}, handleErr
	}
	return s.capabilityAction(response), nil
}

// ObserveLifecycle invokes one observer while retaining runtime availability and operation accounting.
func (s *Service) ObserveLifecycle(
	ctx context.Context,
	extensionID string,
	handlerID string,
	binding extension.Context,
	event lifecycle.Event,
) (bool, error) {
	owner, available := s.beginOperation(extensionID, binding.RuntimeInstanceID)
	if !available {
		return true, fmt.Errorf("%w: lifecycle observer %q is unavailable", ErrExtensionUnavailable, handlerID)
	}
	binding.ExtensionID = extensionID
	observeErr := owner.state.runtime.ObserveLifecycle(ctx, handlerID, s.projectLifecycle(binding, event))
	s.finishAndReport(ctx, owner, observeErr)
	return errors.Is(observeErr, ErrExtensionUnavailable), observeErr
}

// beginOperation accounts for one invocation when its runtime is available.
func (s *Service) beginOperation(pluginID, runtimeID string) (operationOwner, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	state, exists := s.runtimes[pluginID]
	if !exists || !state.available || state.isInvalidated() || state.instanceID != runtimeID || s.closing {
		return operationOwner{pluginID: pluginID, state: state}, false
	}
	state.activeExecutions++
	state.work.Add(1)
	return operationOwner{pluginID: pluginID, state: state}, true
}

// Close stops every runtime without reporting planned shutdown.
func (s *Service) Close() {
	s.mutex.Lock()
	s.closing = true
	states := make([]*runtimeState, 0, len(s.runtimes))
	for _, state := range s.runtimes {
		state.invalidate()
		states = append(states, state)
	}
	s.mutex.Unlock()
	for _, state := range states {
		state.commit.Lock()
		s.mutex.Lock()
		state.exitPending = false
		s.disableLocked(state)
		s.mutex.Unlock()
		state.commit.Unlock()
		state.closeTransport()
		state.join()
	}
}

// invalidate announces that no later context commit can start.
func (state *runtimeState) invalidate() { state.invalidateOnce.Do(func() { close(state.invalidated) }) }

// isInvalidated reports whether commit invalidation has started without blocking.
func (state *runtimeState) isInvalidated() bool {
	select {
	case <-state.invalidated:
		return true
	default:
		return false
	}
}

// closeTransport joins the process connection once without waiting on its calling operation reservation.
func (state *runtimeState) closeTransport() { state.closeOnce.Do(state.runtime.Close) }

// join waits for operation accounting and the instance's optional process-exit monitor.
func (state *runtimeState) join() {
	state.work.Wait()
	if state.observed {
		<-state.monitorDone
	}
}

// monitor invalidates an exited runtime and joins active work before observation ends.
func (s *Service) monitor(ctx context.Context, pluginID string, state *runtimeState, done <-chan struct{}) {
	defer close(state.monitorDone)
	<-done
	state.invalidate()
	state.commit.Lock()
	s.mutex.Lock()
	if s.closing || !s.disableLocked(state) {
		s.mutex.Unlock()
		state.commit.Unlock()
		return
	}
	reportFailure := state.activeExecutions == 0
	state.exitPending = !reportFailure
	s.mutex.Unlock()
	state.commit.Unlock()
	if reportFailure {
		s.report(
			ctx,
			extension.RuntimeFailure{PluginID: pluginID, Condition: extension.RuntimeUnavailableProcessExited},
		)
	}
	state.closeTransport()
	state.work.Wait()
}

// finishAndReport settles active-operation accounting and runtime failure delivery.
func (s *Service) finishAndReport(ctx context.Context, owner operationOwner, executeErr error) {
	defer owner.state.work.Done()
	guardInvalidation := errors.Is(executeErr, ErrExtensionUnavailable)
	if guardInvalidation {
		owner.state.invalidate()
		owner.state.commit.Lock()
	}
	closeRuntime, failure, reportFailure := s.finishExecution(owner, executeErr)
	if guardInvalidation {
		owner.state.commit.Unlock()
	}
	if closeRuntime {
		owner.state.closeTransport()
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
		pending := !s.closing && (owner.state.exitPending || availabilityChanged)
		reportFailure := pending && owner.state.activeExecutions == 0
		owner.state.exitPending = pending && !reportFailure
		if reportFailure {
			return availabilityChanged, extension.RuntimeFailure{
				PluginID:  owner.pluginID,
				Condition: extension.RuntimeUnavailableProcessExited,
			}, true
		}
		return availabilityChanged, extension.RuntimeFailure{}, false
	}
	if owner.state.exitPending && !s.closing && owner.state.activeExecutions == 0 {
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
	state.invalidate()
	if !state.available {
		return false
	}
	state.available = false
	return true
}

// report forwards one classified runtime failure and logs delivery failure without retry.
func (s *Service) report(ctx context.Context, failure extension.RuntimeFailure) {
	if err := s.reporter.ReportRuntimeFailure(ctx, failure); err != nil {
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
