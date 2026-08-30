package ui

import (
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
)

// BuildInitialization creates the single startup frame from resolved Host availability.
func BuildInitialization(
	selectedUIID string,
	report extensionservice.LoadReport,
	selectionIssues []SelectionIssue,
	modelCatalog ModelCatalog,
) domainui.Initialization {
	content := make([]domainui.StartupContent, 0, len(report.Issues)+len(selectionIssues)+1)
	for _, issue := range report.Issues {
		identity := "extension"
		if len(issue.PluginIDs) > 0 {
			identity += " " + strings.Join(issue.PluginIDs, ", ")
		}
		if issue.Path != "" {
			identity += " at " + issue.Path
		}
		content = append(content, domainui.StartupContent{
			Severity: domainui.ContentSeverityError,
			Text:     fmt.Sprintf("%s startup failure: %v", identity, issue.Err),
		})
	}
	for _, issue := range selectionIssues {
		content = append(content, issue.Warning())
	}
	extensions := make([]domainui.ExtensionAvailability, 0, len(report.Extensions))
	summaryParts := []string{"UI " + selectedUIID}
	if len(report.Extensions) == 0 {
		summaryParts = append(summaryParts, "extensions: none")
	}
	for _, extension := range report.Extensions {
		tools := lo.Map(extension.Tools, func(descriptor tool.Descriptor, _ int) string {
			return descriptor.Name
		})
		extensions = append(extensions, domainui.ExtensionAvailability{
			PluginID: extension.ID, Path: extension.Path, Tools: tools,
		})
		toolSummary := "no tools"
		if len(tools) > 0 {
			toolSummary = strings.Join(tools, ", ")
		}
		summaryParts = append(summaryParts, "extension "+extension.ID+" at "+extension.Path+": "+toolSummary)
	}
	content = append(content, domainui.StartupContent{
		Severity: domainui.ContentSeverityInformation,
		Text:     strings.Join(summaryParts, "; "),
	})
	models := lo.Map(
		modelCatalog.Models(),
		func(descriptor model.Descriptor, _ int) domainui.ConfiguredModel {
			choices := lo.Map(
				descriptor.ReasoningCapabilities.Choices,
				func(choice model.ReasoningChoice, _ int) domainui.ReasoningChoice {
					return reasoningChoiceToUI(choice)
				},
			)
			return domainui.ConfiguredModel{
				ProviderID: string(descriptor.Provider), ModelID: string(descriptor.Model),
				Reasoning: domainui.ReasoningCapabilities{
					Supported: descriptor.ReasoningCapabilities.Supported,
					Choices:   choices,
					Default:   reasoningChoiceToUI(descriptor.ReasoningCapabilities.Default),
				},
			}
		},
	)
	return domainui.Initialization{
		SelectedUIID:   selectedUIID,
		StartupContent: content,
		Extensions:     extensions,
		Availability:   domainui.AvailabilityCheckingAuthentication,
		Models:         models,
		ModelSelection: mo.Some(selectionToUI(modelCatalog.Selection())),
		SessionInfo:    session.Info{},
	}
}
