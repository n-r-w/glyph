package ui

import (
	"fmt"
	"strings"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// BuildInitialization creates the single startup frame from resolved Host availability.
func BuildInitialization(
	selectedUIID string,
	report toolservice.LoadReport,
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
		tools := make([]string, len(extension.Tools))
		for index, descriptor := range extension.Tools {
			tools[index] = descriptor.Name
		}
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
	initialization := domainui.Initialization{
		SelectedUIID:   selectedUIID,
		StartupContent: content,
		Extensions:     extensions,
		Availability:   domainui.AvailabilityCheckingAuthentication,
		Models:         nil,
		ModelSelection: emptyModelSelection(),
	}
	for _, descriptor := range modelCatalog.Models() {
		choices := make([]domainui.ReasoningChoice, 0, len(descriptor.ReasoningCapabilities.Choices))
		for _, choice := range descriptor.ReasoningCapabilities.Choices {
			choices = append(choices, reasoningChoiceToUI(choice))
		}
		initialization.Models = append(initialization.Models, domainui.ConfiguredModel{
			ProviderID: string(descriptor.Provider), ModelID: string(descriptor.Model),
			Reasoning: domainui.ReasoningCapabilities{
				Supported: descriptor.ReasoningCapabilities.Supported,
				Choices:   choices,
				Default:   reasoningChoiceToUI(descriptor.ReasoningCapabilities.Default),
			},
		})
	}
	initialization.ModelSelection = selectionToUI(modelCatalog.Selection())
	return initialization
}
