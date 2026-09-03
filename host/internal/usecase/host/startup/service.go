// Package startup coordinates extension registration and reports startup results.
package startup

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/samber/lo"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// Request identifies the Glyph data directory and optional invocation override.
type Request struct {
	// DataDirectory is the initialized Glyph data directory.
	DataDirectory string
	// ExtensionDirectory overrides the extension catalog directory.
	ExtensionDirectory string
}

// Service coordinates registration policy in its required validation order.
type Service struct {
	// runtimes owns pending extension process lifetime and availability.
	runtimes RuntimeLoader
	// tools owns tool descriptor validation, conflicts, and publication.
	tools ToolRegistrar
	// handlers owns handler validation and publication.
	handlers HandlerRegistrar
}

// New creates the Host extension startup service.
func New(runtimes RuntimeLoader, tools ToolRegistrar, handlers HandlerRegistrar) *Service {
	return &Service{runtimes: runtimes, tools: tools, handlers: handlers}
}

// Start loads extensions and reports the complete headless startup state.
func (s *Service) Start(ctx context.Context, request Request, reporter Reporter) (LoadReport, error) {
	report, err := s.Load(ctx, request)
	if err != nil {
		return LoadReport{}, err
	}
	for _, issue := range report.Issues {
		if reportErr := reporter.ReportIssue(ctx, issue); reportErr != nil {
			return LoadReport{}, fmt.Errorf("report extension startup failure: %w", reportErr)
		}
	}
	if summaryErr := reporter.ReportSummary(ctx, report); summaryErr != nil {
		return LoadReport{}, fmt.Errorf("report headless startup summary: %w", summaryErr)
	}
	return report, nil
}

// Load resolves the effective directory and accepts registrations after ordered validation.
func (s *Service) Load(ctx context.Context, request Request) (LoadReport, error) {
	directory := Directory{Path: filepath.Join(request.DataDirectory, "plugins", "extension"), Explicit: false}
	if request.ExtensionDirectory != "" {
		directory = Directory{Path: request.ExtensionDirectory, Explicit: true}
	}
	slog.InfoContext(ctx, "loading extensions", "directory", directory.Path, "explicit", directory.Explicit)
	pending, err := s.runtimes.LoadPending(ctx, directory)
	if err != nil {
		slog.ErrorContext(
			ctx,
			"failed to load extensions",
			"directory",
			directory.Path,
			"explicit",
			directory.Explicit,
			"error",
			err,
		)
		return LoadReport{}, fmt.Errorf("load extensions: %w", err)
	}

	issues := slices.Clone(pending.Issues)
	accepted := make([]AcceptedRegistration, 0, len(pending.Registrations))
	rejected := make(map[string]struct{})
	for _, registration := range pending.Registrations {
		descriptors, validationErr := s.tools.ValidateLocal(registration)
		if validationErr != nil {
			issues = append(
				issues,
				Issue{
					PluginIDs: []string{registration.ID},
					Path:      registration.Path,
					Err:       fmt.Errorf("validate extension registration: %w", validationErr),
				},
			)
			rejected[registration.ID] = struct{}{}
			continue
		}
		accepted = append(
			accepted,
			AcceptedRegistration{ID: registration.ID, Path: registration.Path, Tools: descriptors, Handlers: nil},
		)
	}

	handlerAccepted := accepted[:0]
	for _, registration := range accepted {
		raw := findPending(pending.Registrations, registration.ID)
		handlers, validationErr := s.handlers.ValidateHandlers(raw)
		if validationErr != nil {
			issues = append(
				issues,
				Issue{PluginIDs: []string{registration.ID}, Path: registration.Path, Err: validationErr},
			)
			rejected[registration.ID] = struct{}{}
			continue
		}
		registration.Handlers = handlers
		handlerAccepted = append(handlerAccepted, registration)
	}
	accepted = handlerAccepted

	conflictIssues := s.tools.Conflicts(accepted)
	issues = append(issues, conflictIssues...)
	for _, issue := range conflictIssues {
		for _, pluginID := range issue.PluginIDs {
			rejected[pluginID] = struct{}{}
		}
	}
	accepted = slices.DeleteFunc(accepted, func(registration AcceptedRegistration) bool {
		_, found := rejected[registration.ID]
		return found
	})

	rejectedIDs := make([]string, 0, len(rejected))
	for pluginID := range rejected {
		rejectedIDs = append(rejectedIDs, pluginID)
	}
	slices.Sort(rejectedIDs)
	s.runtimes.RejectPending(rejectedIDs)
	s.tools.Commit(accepted)
	s.handlers.CommitHandlers(accepted)
	s.runtimes.Accept(accepted)

	slices.SortFunc(accepted, func(left, right AcceptedRegistration) int { return cmp.Compare(left.ID, right.ID) })
	sortIssues(issues)
	report := LoadReport{Issues: issues, Extensions: accepted}
	logReport(ctx, directory, report)
	return report, nil
}

// findPending returns the raw registration retained for one locally accepted extension.
func findPending(registrations []PendingRegistration, pluginID string) PendingRegistration {
	for _, registration := range registrations {
		if registration.ID == pluginID {
			return registration
		}
	}
	return PendingRegistration{}
}

// sortIssues makes startup diagnostics deterministic.
func sortIssues(issues []Issue) {
	slices.SortFunc(issues, func(left, right Issue) int {
		return cmp.Compare(fmt.Sprint(left.PluginIDs, left.Path), fmt.Sprint(right.PluginIDs, right.Path))
	})
}

// logReport records accepted extension registrations without owning registration policy.
func logReport(ctx context.Context, directory Directory, report LoadReport) {
	slog.InfoContext(
		ctx,
		"loaded extensions",
		"directory",
		directory.Path,
		"explicit",
		directory.Explicit,
		"extension_count",
		len(report.Extensions),
		"issue_count",
		len(report.Issues),
	)
	for _, extension := range report.Extensions {
		toolNames := lo.Map(extension.Tools, func(descriptor tool.Descriptor, _ int) string { return descriptor.Name })
		slog.InfoContext(ctx, "loaded extension", "plugin_id", extension.ID, "path", extension.Path, "tools", toolNames)
	}
}
