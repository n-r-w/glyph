// Package startup resolves headless extension startup and reports its results.
package startup

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/samber/lo"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
)

// Request identifies the Glyph data directory and optional invocation override.
type Request struct {
	// DataDirectory is the initialized Glyph data directory.
	DataDirectory string
	// ExtensionDirectory overrides the extension catalog directory.
	ExtensionDirectory string
}

// Service resolves and loads the effective extension catalog.
type Service struct {
	// load discovers and starts extensions from the effective directory.
	load func(context.Context, extensionservice.Directory) (extensionservice.LoadReport, error)
}

// New creates the Host extension startup service.
func New(load func(context.Context, extensionservice.Directory) (extensionservice.LoadReport, error)) *Service {
	return &Service{load: load}
}

// Start loads extensions and reports the complete headless startup state.
func (s *Service) Start(
	ctx context.Context,
	request Request,
	reporter Reporter,
) (extensionservice.LoadReport, error) {
	report, err := s.Load(ctx, request)
	if err != nil {
		return extensionservice.LoadReport{}, err
	}
	for _, issue := range report.Issues {
		reportErr := reporter.ReportIssue(ctx, issue)
		if reportErr != nil {
			return extensionservice.LoadReport{}, fmt.Errorf("report extension startup failure: %w", reportErr)
		}
	}
	summaryErr := reporter.ReportSummary(ctx, report)
	if summaryErr != nil {
		return extensionservice.LoadReport{}, fmt.Errorf("report headless startup summary: %w", summaryErr)
	}
	return report, nil
}

// Load resolves the effective directory and returns startup state without delivering it.
func (s *Service) Load(ctx context.Context, request Request) (extensionservice.LoadReport, error) {
	directory := extensionservice.Directory{
		Path:     filepath.Join(request.DataDirectory, "plugins", "extension"),
		Explicit: false,
	}
	if request.ExtensionDirectory != "" {
		directory = extensionservice.Directory{Path: request.ExtensionDirectory, Explicit: true}
	}
	slog.InfoContext(ctx, "loading extensions",
		"directory", directory.Path,
		"explicit", directory.Explicit,
	)
	report, err := s.load(ctx, directory)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load extensions",
			"directory", directory.Path,
			"explicit", directory.Explicit,
			"error", err,
		)
		return extensionservice.LoadReport{}, fmt.Errorf("load extensions: %w", err)
	}
	slog.InfoContext(ctx, "loaded extensions",
		"directory", directory.Path,
		"explicit", directory.Explicit,
		"extension_count", len(report.Extensions),
		"issue_count", len(report.Issues),
	)
	for _, extension := range report.Extensions {
		toolNames := lo.Map(extension.Tools, func(descriptor tool.Descriptor, _ int) string {
			return descriptor.Name
		})
		slog.InfoContext(ctx, "loaded extension",
			"plugin_id", extension.ID,
			"path", extension.Path,
			"tools", toolNames,
		)
	}
	return report, nil
}
