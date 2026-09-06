package ui

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// ExtensionLoadIssue contains one isolated extension startup failure.
type ExtensionLoadIssue struct {
	// PluginIDs identifies affected extension processes.
	PluginIDs []string
	// Path identifies the failed catalog entry.
	Path string
	// Err contains the isolated startup failure.
	Err error
}

// LoadedExtension contains extension availability shown during UI startup.
type LoadedExtension struct {
	// ID identifies the available extension.
	ID string
	// Path identifies the extension executable.
	Path string
	// Tools contains available tool names in registration order.
	Tools []string
}

// ExtensionLoadReport contains extension startup state needed by UI initialization.
type ExtensionLoadReport struct {
	// Issues contains isolated extension startup failures.
	Issues []ExtensionLoadIssue
	// Extensions contains available extensions in activation order.
	Extensions []LoadedExtension
}

// BuildInitialization creates the single startup frame from resolved Host availability.
func BuildInitialization(
	selectedUIID string,
	report ExtensionLoadReport,
	selectionIssues []SelectionIssue,
	modelCatalog ModelCatalog,
) Initialization {
	content := make([]StartupContent, 0, len(report.Issues)+len(selectionIssues)+1)
	for _, issue := range report.Issues {
		identity := "extension"
		if len(issue.PluginIDs) > 0 {
			identity += " " + strings.Join(issue.PluginIDs, ", ")
		}
		if issue.Path != "" {
			identity += " at " + issue.Path
		}
		content = append(content, StartupContent{
			Severity: ContentSeverityError,
			Text:     fmt.Sprintf("%s startup failure: %v", identity, issue.Err),
		})
	}
	for _, issue := range selectionIssues {
		content = append(content, issue.Warning())
	}
	extensions := make([]ExtensionAvailability, 0, len(report.Extensions))
	summaryParts := []string{"UI " + selectedUIID}
	if len(report.Extensions) == 0 {
		summaryParts = append(summaryParts, "extensions: none")
	}
	for _, extension := range report.Extensions {
		tools := extension.Tools
		extensions = append(extensions, ExtensionAvailability{
			PluginID: extension.ID, Path: extension.Path, Tools: tools,
		})
		toolSummary := "no tools"
		if len(tools) > 0 {
			toolSummary = strings.Join(tools, ", ")
		}
		summaryParts = append(summaryParts, "extension "+extension.ID+" at "+extension.Path+": "+toolSummary)
	}
	content = append(content, StartupContent{
		Severity: ContentSeverityInformation,
		Text:     strings.Join(summaryParts, "; "),
	})
	models := lo.Map(modelCatalog.Models(), func(descriptor model.Descriptor, _ int) model.Descriptor {
		return descriptor.Clone()
	})
	return Initialization{
		SelectedUIID:   selectedUIID,
		StartupContent: content,
		Extensions:     extensions,
		Availability:   AvailabilityCheckingAuthentication,
		Models:         models,
		ModelSelection: mo.Some(modelCatalog.ActiveSelection()),
		SessionInfo:    session.Info{},
	}
}
