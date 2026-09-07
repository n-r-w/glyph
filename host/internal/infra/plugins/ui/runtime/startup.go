package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// selectionWarningFormat preserves the established excluded-candidate diagnostic.
const selectionWarningFormat = "excluded UI %s at %s: %v"

// warningPrefix marks the stderr fallback for a selection warning.
const warningPrefix = "[warning] "

const (
	// extensionIdentityText names a startup failure without a known plugin identity.
	extensionIdentityText = "extension"
	// startupFailureFormat retains identity and the complete startup cause.
	startupFailureFormat = "%s startup failure: %v"
	// selectedUISummaryFormat identifies the selected UI in the startup summary.
	selectedUISummaryFormat = "UI %s"
	// noExtensionsSummaryText describes a normal empty extension catalog.
	noExtensionsSummaryText = "extensions: none"
	// noToolsSummaryText describes an accepted extension without tools.
	noToolsSummaryText = "no tools"
	// extensionSummaryFormat presents one accepted executable and its tool names.
	extensionSummaryFormat = "extension %s at %s: %s"
	// identityPathFormat adds the inspected path to a startup issue identity.
	identityPathFormat = "%s at %s"
	// startupListSeparator separates identities and tool names.
	startupListSeparator = ", "
	// startupSummarySeparator separates startup summary items.
	startupSummarySeparator = "; "
)

// BindSelection retains startup-only selection output until initialization or close delivers it.
func (s *Service) BindSelection(selection hostui.Selection, stderr io.Writer) {
	s.selectedUIID = selection.ID
	s.selectionIssues = slices.Clone(selection.Issues)
	s.warningWriter = stderr
}

// ReportIssue accepts incremental reports; the final report alone supplies initialization content.
func (s *Service) ReportIssue(context.Context, startup.Issue) error { return nil }

// ReportSummary retains the authoritative report, including each isolated issue exactly once.
func (s *Service) ReportSummary(_ context.Context, report startup.LoadReport) error {
	s.startupReport = report
	return nil
}

// selectionWarning constructs one output-owned diagnostic without discarding its cause.
func selectionWarning(issue hostui.SelectionIssue) StartupContent {
	return StartupContent{
		Severity: ContentSeverityWarning,
		Text:     fmt.Sprintf(selectionWarningFormat, issue.Candidate.ID, issue.Candidate.Path, issue.Err),
	}
}

// flushSelectionWarnings attempts only warnings not delivered through initialization or an earlier close.
func (s *Service) flushSelectionWarnings() error {
	// Consume pending warnings before I/O so repeated close cannot duplicate a partial write.
	issues := s.selectionIssues
	s.selectionIssues = nil
	// Each warning gets one attempt, and shutdown receives every failed attempt.
	var result error
	for _, issue := range issues {
		line := warningPrefix + selectionWarning(issue).Text
		if !strings.HasSuffix(line, "\n") {
			line += "\n"
		}
		written, err := io.WriteString(s.warningWriter, line)
		if err != nil {
			result = errors.Join(result, issue.Err, fmt.Errorf("write CLI warning: %w", err))
		} else if written != len(line) {
			result = errors.Join(result, issue.Err, fmt.Errorf("write CLI warning: %w", io.ErrShortWrite))
		}
	}
	return result
}

// startupSources identifies declared startup failures without interpreting rendered content.
func (s *Service) startupSources() error {
	sources := make([]error, 0, len(s.startupReport.Issues)+len(s.selectionIssues))
	for _, issue := range s.startupReport.Issues {
		sources = append(sources, issue.Err)
	}
	for _, issue := range s.selectionIssues {
		sources = append(sources, issue.Err)
	}
	return errors.Join(sources...)
}

// buildInitialization combines authoritative Host state with output-owned startup diagnostics.
func (s *Service) buildInitialization(state hostui.Initialization) Initialization {
	// Only the authoritative final report supplies extension issues.
	content := make([]StartupContent, 0, len(s.startupReport.Issues)+len(s.selectionIssues)+1)
	for _, issue := range s.startupReport.Issues {
		identity := extensionIdentityText
		if len(issue.PluginIDs) > 0 {
			identity += " " + strings.Join(issue.PluginIDs, startupListSeparator)
		}
		if issue.Path != "" {
			identity = fmt.Sprintf(identityPathFormat, identity, issue.Path)
		}
		content = append(content, StartupContent{
			Severity: ContentSeverityError,
			Text:     fmt.Sprintf(startupFailureFormat, identity, issue.Err),
		})
	}
	for _, issue := range s.selectionIssues {
		content = append(content, selectionWarning(issue))
	}
	// Availability and the single summary share the accepted report order.
	extensions := make([]ExtensionAvailability, 0, len(s.startupReport.Extensions))
	summaryParts := []string{fmt.Sprintf(selectedUISummaryFormat, s.selectedUIID)}
	if len(s.startupReport.Extensions) == 0 {
		summaryParts = append(summaryParts, noExtensionsSummaryText)
	}
	for _, extension := range s.startupReport.Extensions {
		tools := make([]string, len(extension.Tools))
		for index := range extension.Tools {
			tools[index] = extension.Tools[index].Name
		}
		extensions = append(extensions, ExtensionAvailability{
			PluginID: extension.ID, Path: extension.Path, Tools: tools,
		})
		toolSummary := noToolsSummaryText
		if len(tools) > 0 {
			toolSummary = strings.Join(tools, startupListSeparator)
		}
		summaryParts = append(
			summaryParts,
			fmt.Sprintf(extensionSummaryFormat, extension.ID, extension.Path, toolSummary),
		)
	}
	content = append(content, StartupContent{
		Severity: ContentSeverityInformation,
		Text:     strings.Join(summaryParts, startupSummarySeparator),
	})
	return Initialization{
		SelectedUIID:   s.selectedUIID,
		StartupContent: content,
		Extensions:     extensions,
		Availability:   state.Availability,
		Models:         state.Models,
		ModelSelection: state.ModelSelection,
		SessionInfo:    state.SessionInfo,
	}
}
