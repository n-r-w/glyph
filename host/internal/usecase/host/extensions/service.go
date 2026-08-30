// Package extensions owns extension process registration, dispatch, and availability.
package extensions

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// Service owns extension processes, globally unique tools, and ordered handlers.
type Service struct {
	// catalog discovers executable extension candidates.
	catalog Catalog
	// factory starts extension runtimes.
	factory RuntimeFactory
	// reportFailure publishes extension availability failures.
	reportFailure func(context.Context, tool.RuntimeFailure) error

	// mutex protects runtime and tool ownership state.
	mutex sync.RWMutex
	// runtimes contains extension runtime state by plugin ID.
	runtimes map[string]*runtimeState
	// owners contains the owning extension for each tool name.
	owners map[string]toolOwner
	// handlers contains available handlers in activation and registration order.
	handlers []RegisteredHandler
	// monitoring reports whether runtime exit monitors are active.
	monitoring bool
	// closing reports whether service shutdown has started.
	closing bool
}

var (
	_ run.ToolRuntime           = (*Service)(nil)
	_ sessiontree.HandlerRunner = (*Service)(nil)
)

// runtimeState contains one process and its complete catalog.
type runtimeState struct {
	// path is the extension executable path.
	path string
	// runtime owns the extension process connection.
	runtime ExtensionRuntime
	// tools contains the complete extension tool catalog.
	tools []tool.Descriptor
	// handlers contains the complete ordered extension handler catalog.
	handlers []HandlerDescriptor
	// available reports whether the runtime accepts executions.
	available bool
	// activeExecutions counts in-flight tool calls.
	activeExecutions int
	// exitPending reports a runtime exit awaiting active calls.
	exitPending bool
}

// toolOwner preserves the normalized plugin identity for one registered tool.
type toolOwner struct {
	// pluginID identifies the extension that owns the tool.
	pluginID string
	// state points to the owning extension runtime state.
	state *runtimeState
}

// New creates the Host extension service.
func New(
	catalog Catalog,
	factory RuntimeFactory,
	reportFailure func(context.Context, tool.RuntimeFailure) error,
) *Service {
	return &Service{
		catalog:       catalog,
		factory:       factory,
		reportFailure: reportFailure,
		mutex:         sync.RWMutex{},
		runtimes:      make(map[string]*runtimeState),
		owners:        make(map[string]toolOwner),
		handlers:      nil,
		monitoring:    false,
		closing:       false,
	}
}

// Activate starts post-start runtime observation after the user surface is ready.
func (s *Service) Activate(ctx context.Context) {
	// Runtime observation keeps application logging values after caller cancellation.
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

// Load discovers, starts, and registers the effective extension catalog.
func (s *Service) Load(ctx context.Context, directory Directory) (LoadReport, error) {
	discovery, err := s.catalog.Discover(ctx, directory)
	if err != nil {
		return LoadReport{}, fmt.Errorf("discover extensions: %w", err)
	}
	issues := slices.Clone(discovery.Issues)
	states := make(map[string]*runtimeState, len(discovery.Candidates))
	for _, candidate := range discovery.Candidates {
		state, startErr := s.startCandidate(ctx, candidate)
		if startErr != nil {
			issues = append(issues, Issue{
				PluginIDs: []string{candidate.ID}, Path: candidate.Path, Err: startErr,
			})
			continue
		}
		states[candidate.ID] = state
	}

	issues = append(issues, rejectToolConflicts(states)...)

	extensions := make([]LoadedExtension, 0, len(states))
	for id, state := range states {
		if state.available {
			extensions = append(extensions, LoadedExtension{
				ID: id, Path: state.path, Tools: slices.Clone(state.tools),
				Handlers: slices.Clone(state.handlers),
			})
		}
	}
	slices.SortFunc(extensions, func(left, right LoadedExtension) int {
		return cmp.Compare(left.ID, right.ID)
	})

	s.mutex.Lock()
	for id, state := range states {
		s.runtimes[id] = state
		if state.available {
			for index := range state.tools {
				s.owners[state.tools[index].Name] = toolOwner{pluginID: id, state: state}
			}
		}
	}
	for _, candidate := range discovery.Candidates {
		state, exists := states[candidate.ID]
		if !exists || !state.available {
			continue
		}
		for _, handler := range state.handlers {
			s.handlers = append(s.handlers, RegisteredHandler{
				ExtensionID: candidate.ID,
				ID:          handler.ID,
				Kind:        handler.Kind,
			})
		}
	}
	s.mutex.Unlock()
	for _, state := range states {
		if !state.available {
			state.runtime.Close()
		}
	}
	sortIssues(issues)
	return LoadReport{Issues: issues, Extensions: extensions}, nil
}

// startCandidate starts one process and accepts its complete validated registration.
func (s *Service) startCandidate(ctx context.Context, candidate Candidate) (*runtimeState, error) {
	runtime, err := s.factory.Start(ctx, candidate)
	if err != nil {
		return nil, err
	}
	registration, err := runtime.Register(ctx)
	if err != nil {
		runtime.Close()
		return nil, err
	}
	if err = validateHandlerRegistration(registration.Handlers); err != nil {
		runtime.Close()
		return nil, err
	}
	return &runtimeState{
		path: candidate.Path, runtime: runtime, tools: registration.Tools,
		handlers: registration.Handlers, available: true,
		activeExecutions: 0, exitPending: false,
	}, nil
}

// registeredHandlers returns available runtime handlers in registration order.
func (s *Service) registeredHandlers(kind HandlerKind) []RegisteredHandler {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	result := make([]RegisteredHandler, 0, len(s.handlers))
	for _, handler := range s.handlers {
		if handler.Kind == kind {
			result = append(result, handler)
		}
	}
	return result
}

// handle invokes one registered runtime handler and validates its typed response.
func (s *Service) handle(
	ctx context.Context,
	handler RegisteredHandler,
	request HandlerRequest,
) (HandlerResponse, error) {
	requestKind, validRequest := request.Kind()
	if !validRequest || requestKind != handler.Kind {
		return HandlerResponse{}, fmt.Errorf("handler %q request kind does not match registration", handler.ID)
	}

	s.mutex.Lock()
	state, exists := s.runtimes[handler.ExtensionID]
	registered := exists && state.available && slices.ContainsFunc(state.handlers, func(candidate HandlerDescriptor) bool {
		return candidate.ID == handler.ID && candidate.Kind == handler.Kind
	})
	if registered {
		state.activeExecutions++
	}
	s.mutex.Unlock()
	if !registered {
		return HandlerResponse{}, fmt.Errorf(
			"%w: extension handler %q is unavailable",
			ErrExtensionUnavailable,
			handler.ID,
		)
	}

	response, err := state.runtime.Handle(ctx, handler.ID, request)
	responseKind, validResponse := response.Kind()
	if err == nil && (!validResponse || responseKind != handler.Kind) {
		response = HandlerResponse{}
		err = fmt.Errorf("%w: extension handler %q returned an action for another kind", ErrExtensionUnavailable, handler.ID)
	}
	closeRuntime, failure, reportFailure := s.finishExecution(toolOwner{
		pluginID: handler.ExtensionID,
		state:    state,
	}, err)
	if closeRuntime {
		state.runtime.Close()
	}
	if reportFailure {
		s.report(ctx, failure)
	}
	return response, err
}

// Tools returns the currently available global catalog.
func (s *Service) Tools() []tool.Descriptor {
	s.mutex.RLock()
	result := make([]tool.Descriptor, 0, len(s.owners))
	for _, state := range s.runtimes {
		if state.available {
			result = append(result, state.tools...)
		}
	}
	s.mutex.RUnlock()
	slices.SortFunc(result, func(left, right tool.Descriptor) int {
		return cmp.Compare(left.Name, right.Name)
	})
	return result
}

// Execute routes one call or returns a terminal unavailable-tool result.
func (s *Service) Execute(
	ctx context.Context,
	call model.ToolCall,
	handleProgress tool.ProgressHandler,
) (agent.ToolResult, error) {
	argumentsJSON, err := json.Marshal(call.Arguments, json.Deterministic(true))
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("encode tool %q arguments: %w", call.Name, err)
	}

	s.mutex.Lock()
	owner, exists := s.owners[call.Name]
	if exists {
		owner.state.activeExecutions++
	}
	s.mutex.Unlock()
	if !exists {
		return agent.ToolResult{
			CallID: call.ID, ToolName: call.Name,
			Contents: tool.TextContents(fmt.Sprintf("tool %q is unavailable", call.Name)), IsError: true,
		}, nil
	}

	result, executeErr := owner.state.runtime.Execute(ctx, call.Name, argumentsJSON, handleProgress)
	closeRuntime, failure, reportFailure := s.finishExecution(owner, executeErr)
	if closeRuntime {
		owner.state.runtime.Close()
	}
	if reportFailure {
		s.report(ctx, failure)
	}
	return agent.ToolResult{
		CallID: call.ID, ToolName: call.Name,
		Contents: slices.Clone(result.Contents), IsError: result.IsError,
	}, executeErr
}

// Close stops every available extension runtime without reporting planned shutdown.
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
	s.owners = make(map[string]toolOwner)
	s.mutex.Unlock()
	for _, state := range states {
		state.runtime.Close()
	}
}

// monitor removes every owned tool and reports an idle process exit once.
func (s *Service) monitor(
	ctx context.Context,
	pluginID string,
	state *runtimeState,
	done <-chan struct{},
) {
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
		s.report(ctx, tool.RuntimeFailure{
			PluginID: pluginID, Condition: tool.RuntimeUnavailableProcessExited,
		})
	}
	state.runtime.Close()
}

// finishExecution assigns process-exit presentation to either the tool result or runtime reporter.
func (s *Service) finishExecution(
	owner toolOwner,
	executeErr error,
) (bool, tool.RuntimeFailure, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var noFailure tool.RuntimeFailure
	owner.state.activeExecutions--
	if errors.Is(executeErr, ErrExtensionUnavailable) {
		availabilityChanged := s.disableLocked(owner.state)
		reportFailure := !s.closing && (owner.state.exitPending || availabilityChanged)
		owner.state.exitPending = false
		if reportFailure {
			return availabilityChanged, tool.RuntimeFailure{
				PluginID: owner.pluginID, Condition: tool.RuntimeUnavailableProcessExited,
			}, true
		}
		return availabilityChanged, noFailure, false
	}
	if owner.state.exitPending && !s.closing {
		owner.state.exitPending = false
		return false, tool.RuntimeFailure{
			PluginID: owner.pluginID, Condition: tool.RuntimeUnavailableProcessExited,
		}, true
	}
	return false, noFailure, false
}

// disableLocked removes all tools for one runtime and reports whether availability changed.
func (s *Service) disableLocked(state *runtimeState) bool {
	if !state.available {
		return false
	}
	state.available = false
	for index := range state.tools {
		delete(s.owners, state.tools[index].Name)
	}
	s.handlers = slices.DeleteFunc(s.handlers, func(handler RegisteredHandler) bool {
		return s.runtimes[handler.ExtensionID] == state
	})
	return true
}

// report forwards one classified runtime failure and logs delivery failure without retry.
func (s *Service) report(ctx context.Context, failure tool.RuntimeFailure) {
	if err := s.reportFailure(ctx, failure); err != nil {
		slog.ErrorContext(ctx, "report extension runtime failure",
			"plugin_id", failure.PluginID,
			"condition", failure.Condition,
			"error", err,
		)
	}
}

// rejectToolConflicts disables every extension in a tool conflict and returns isolated issues.
func rejectToolConflicts(states map[string]*runtimeState) []Issue {
	conflicts := findConflicts(states)
	issues := make([]Issue, 0, len(conflicts))
	for name, ids := range conflicts {
		issues = append(issues, Issue{
			PluginIDs: ids, Path: "", Err: fmt.Errorf("tool name %q conflicts", name),
		})
		for _, id := range ids {
			states[id].available = false
		}
	}
	return issues
}

// findConflicts returns every duplicated name and all extension owners.
func findConflicts(states map[string]*runtimeState) map[string][]string {
	owners := make(map[string][]string)
	for id, state := range states {
		for index := range state.tools {
			name := state.tools[index].Name
			owners[name] = append(owners[name], id)
		}
	}
	conflicts := make(map[string][]string)
	for name, ids := range owners {
		if len(ids) > 1 {
			slices.Sort(ids)
			conflicts[name] = ids
		}
	}
	return conflicts
}

// validateHandlerRegistration rejects incomplete, unknown, and duplicate handler descriptors.
func validateHandlerRegistration(handlers []HandlerDescriptor) error {
	ids := make(map[string]struct{}, len(handlers))
	for _, handler := range handlers {
		if strings.TrimSpace(handler.ID) == "" {
			return errors.New("handler ID is empty")
		}
		switch handler.Kind {
		case HandlerKindSessionBeforeTreeRequest, HandlerKindSessionBeforeTreeResult, HandlerKindSessionTree:
		default:
			return fmt.Errorf("handler %q has unknown kind %d", handler.ID, handler.Kind)
		}
		if _, exists := ids[handler.ID]; exists {
			return fmt.Errorf("handler ID %q is duplicated", handler.ID)
		}
		ids[handler.ID] = struct{}{}
	}
	return nil
}

// sortIssues makes startup diagnostics deterministic.
func sortIssues(issues []Issue) {
	slices.SortFunc(issues, func(left, right Issue) int {
		return cmp.Compare(
			fmt.Sprint(left.PluginIDs, left.Path),
			fmt.Sprint(right.PluginIDs, right.Path),
		)
	})
}
