// Package ui owns Host UI selection and session lifecycle policy.
package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/pluginid"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// SelectionRequest contains startup-only selection inputs.
type SelectionRequest struct {
	// Directory identifies the effective UI catalog directory.
	Directory domainui.Directory
	// ExplicitUI identifies an invocation-selected UI plugin.
	ExplicitUI string
	// ActiveUI identifies the preferred configured UI plugin.
	ActiveUI mo.Option[string]
}

// SelectionIssue describes one automatically excluded UI candidate.
type SelectionIssue struct {
	// Candidate identifies the excluded UI plugin.
	Candidate domainui.Candidate
	// Err contains the candidate startup failure.
	Err error
}

// Warning preserves one excluded candidate as user-visible startup content.
func (i SelectionIssue) Warning() domainui.StartupContent {
	return domainui.StartupContent{
		Severity: domainui.ContentSeverityWarning,
		Text: fmt.Sprintf(
			"excluded UI %s at %s: %v", i.Candidate.ID, i.Candidate.Path, i.Err,
		),
	}
}

// Selection contains the single selected connected runtime.
type Selection struct {
	// ID identifies the selected UI plugin.
	ID string
	// Capabilities contains immutable UI startup behavior.
	Capabilities domainui.Capabilities
	// Runtime owns the connected UI process.
	Runtime Runtime
	// Issues contains automatically excluded candidates.
	Issues []SelectionIssue
}

// Selector applies startup-only UI selection policy.
type Selector struct {
	// catalog discovers executable UI candidates.
	catalog Catalog
	// factory starts one selected UI runtime.
	factory RuntimeFactory
}

// NewSelector creates a UI selection service.
func NewSelector(catalog Catalog, factory RuntimeFactory) *Selector {
	return &Selector{catalog: catalog, factory: factory}
}

// Select discovers candidates and applies explicit, active, or sole-compatible priority.
func (s *Selector) Select(ctx context.Context, request SelectionRequest) (Selection, error) {
	explicitID := pluginid.Normalize(request.ExplicitUI)
	activeID := ""
	if configuredActiveID, present := request.ActiveUI.Get(); present {
		activeID = pluginid.Normalize(configuredActiveID)
	}
	slog.InfoContext(ctx, "loading UI plugins",
		"directory", request.Directory.Path,
		"explicit_ui", explicitID,
		"active_ui", activeID,
	)
	discovery, err := s.catalog.Discover(ctx, request.Directory)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load UI plugins",
			"directory", request.Directory.Path,
			"error", err,
		)
		return Selection{}, fmt.Errorf("discover UI plugins: %w", err)
	}
	selectedID := explicitID
	if selectedID == "" {
		selectedID = activeID
	}
	var selection Selection
	if selectedID != "" {
		selection, err = s.startSelected(ctx, discovery.Candidates, selectedID)
	} else {
		selection, err = s.selectCompatible(ctx, discovery.Candidates)
	}
	if err != nil {
		slog.ErrorContext(ctx, "failed to load UI plugin",
			"directory", request.Directory.Path,
			"candidate_count", len(discovery.Candidates),
			"error", err,
		)
		return selection, err
	}
	slog.InfoContext(ctx, "loaded UI plugin",
		"plugin_id", selection.ID,
		"directory", request.Directory.Path,
		"controls_terminal", selection.Capabilities.ControlsTerminal,
		"candidate_count", len(discovery.Candidates),
		"issue_count", len(selection.Issues),
	)
	return selection, nil
}

// startSelected starts only the requested candidate and returns its reusable connection.
func (s *Selector) startSelected(
	ctx context.Context,
	candidates []domainui.Candidate,
	selectedID string,
) (Selection, error) {
	for _, candidate := range candidates {
		if candidate.ID != selectedID {
			continue
		}
		runtime, err := s.factory.Start(ctx, candidate)
		if err != nil {
			return Selection{}, fmt.Errorf("start selected UI %q: %w", selectedID, err)
		}
		return Selection{
			ID:           candidate.ID,
			Capabilities: runtime.Capabilities(),
			Runtime:      runtime,
			Issues:       nil,
		}, nil
	}
	return Selection{}, fmt.Errorf("selected UI %q is absent", selectedID)
}

// selectCompatible probes every candidate, stops every probe, and restarts the sole compatible candidate.
func (s *Selector) selectCompatible(
	ctx context.Context,
	candidates []domainui.Candidate,
) (Selection, error) {
	compatible := make([]domainui.Candidate, 0, len(candidates))
	issues := make([]SelectionIssue, 0)
	for _, candidate := range candidates {
		runtime, err := s.factory.Start(ctx, candidate)
		if err != nil {
			issues = append(issues, SelectionIssue{Candidate: candidate, Err: err})
			continue
		}
		compatible = append(compatible, candidate)
		runtime.Close()
	}
	if len(compatible) == 0 {
		return Selection{
			ID: "", Capabilities: domainui.Capabilities{ControlsTerminal: false}, Runtime: nil, Issues: issues,
		}, errors.New("no compatible UI plugin is available")
	}
	if len(compatible) > 1 {
		return Selection{
			ID: "", Capabilities: domainui.Capabilities{ControlsTerminal: false}, Runtime: nil, Issues: issues,
		}, errors.New("multiple compatible UI plugins are available")
	}
	candidate := compatible[0]
	runtime, err := s.factory.Start(ctx, candidate)
	if err != nil {
		issues = append(issues, SelectionIssue{Candidate: candidate, Err: err})
		return Selection{
			ID: "", Capabilities: domainui.Capabilities{ControlsTerminal: false}, Runtime: nil, Issues: issues,
		}, fmt.Errorf("restart automatically selected UI %q: %w", candidate.ID, err)
	}
	return Selection{
		ID:           candidate.ID,
		Capabilities: runtime.Capabilities(),
		Runtime:      runtime,
		Issues:       issues,
	}, nil
}
