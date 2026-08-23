// Package tools loads extension runtimes and owns the global tool registry.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// Service owns extension availability and globally unique tools.
type Service struct {
	catalog       Catalog
	factory       RuntimeFactory
	reportFailure func(context.Context, tool.RuntimeFailure) error

	mutex      sync.RWMutex
	runtimes   map[string]*runtimeState
	owners     map[string]toolOwner
	monitoring bool
	closing    bool
}

var _ run.ToolRuntime = (*Service)(nil)

// runtimeState contains one process and its complete catalog.
type runtimeState struct {
	path             string
	runtime          ExtensionRuntime
	tools            []tool.Descriptor
	available        bool
	activeExecutions int
	exitPending      bool
}

// toolOwner preserves the normalized plugin identity for one registered tool.
type toolOwner struct {
	pluginID string
	state    *runtimeState
}

// New creates a Host extension tool service.
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
	issues := append([]Issue(nil), discovery.Issues...)
	states := make(map[string]*runtimeState, len(discovery.Candidates))
	for _, candidate := range discovery.Candidates {
		runtime, startErr := s.factory.Start(ctx, candidate)
		if startErr != nil {
			issues = append(issues, Issue{PluginIDs: []string{candidate.ID}, Path: candidate.Path, Err: startErr})
			continue
		}
		descriptors, listErr := runtime.ListTools(ctx)
		if listErr != nil {
			runtime.Close()
			issues = append(issues, Issue{PluginIDs: []string{candidate.ID}, Path: candidate.Path, Err: listErr})
			continue
		}
		states[candidate.ID] = &runtimeState{
			path: candidate.Path, runtime: runtime, tools: descriptors, available: true,
			activeExecutions: 0, exitPending: false,
		}
	}

	conflicts := findConflicts(states)
	for name, ids := range conflicts {
		issues = append(issues, Issue{PluginIDs: ids, Path: "", Err: fmt.Errorf("tool name %q conflicts", name)})
		for _, id := range ids {
			states[id].available = false
		}
	}

	extensions := make([]LoadedExtension, 0, len(states))
	for id, state := range states {
		if state.available {
			extensions = append(extensions, LoadedExtension{
				ID: id, Path: state.path, Tools: append([]tool.Descriptor(nil), state.tools...),
			})
		}
	}
	sort.Slice(extensions, func(i, j int) bool { return extensions[i].ID < extensions[j].ID })

	s.mutex.Lock()
	for id, state := range states {
		s.runtimes[id] = state
		if state.available {
			for _, descriptor := range state.tools {
				s.owners[descriptor.Name] = toolOwner{pluginID: id, state: state}
			}
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
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Execute routes one call or returns a terminal unavailable-tool result.
func (s *Service) Execute(
	ctx context.Context,
	call model.ToolCall,
	handleProgress tool.ProgressHandler,
) (agent.ToolResult, error) {
	argumentsJSON, err := json.Marshal(call.Arguments)
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
		Contents: append([]tool.ResultContent(nil), result.Contents...), IsError: result.IsError,
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
	for _, descriptor := range state.tools {
		delete(s.owners, descriptor.Name)
	}
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

// findConflicts returns every duplicated name and all extension owners.
func findConflicts(states map[string]*runtimeState) map[string][]string {
	owners := make(map[string][]string)
	for id, state := range states {
		for _, descriptor := range state.tools {
			owners[descriptor.Name] = append(owners[descriptor.Name], id)
		}
	}
	conflicts := make(map[string][]string)
	for name, ids := range owners {
		if len(ids) > 1 {
			sort.Strings(ids)
			conflicts[name] = ids
		}
	}
	return conflicts
}

// sortIssues makes startup diagnostics deterministic.
func sortIssues(issues []Issue) {
	sort.Slice(issues, func(i, j int) bool {
		return fmt.Sprint(issues[i].PluginIDs, issues[i].Path) < fmt.Sprint(issues[j].PluginIDs, issues[j].Path)
	})
}
