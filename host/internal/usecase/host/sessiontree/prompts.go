package sessiontree

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/samber/mo"
)

var (
	// branchSummaryPromptText contains the embedded generation instructions.
	//go:embed prompts/branch_summary.md
	branchSummaryPromptText string
	// branchSummaryContextText contains the embedded active-history layout.
	//go:embed prompts/branch_summary_context.md
	branchSummaryContextText string
)

// branchSummaryPromptData contains optional focus for the embedded generation prompt.
type branchSummaryPromptData struct {
	// CustomFocus contains caller focus only in custom-prompt mode.
	CustomFocus string
}

// branchSummaryContextData contains persisted text for the embedded active-history template.
type branchSummaryContextData struct {
	// Summary contains persisted summary text inserted unchanged.
	Summary string
}

// renderBranchSummaryPrompt executes the embedded generation instructions with optional focus.
func renderBranchSummaryPrompt(customFocus mo.Option[string]) (string, error) {
	var rendered strings.Builder
	prompt := template.Must(template.New("branch_summary").Parse(branchSummaryPromptText))
	if err := prompt.Execute(
		&rendered,
		branchSummaryPromptData{CustomFocus: customFocus.OrEmpty()},
	); err != nil {
		return "", fmt.Errorf("render branch summary prompt: %w", err)
	}
	return rendered.String(), nil
}

// RenderBranchSummaryContext renders one persisted summary as provider-neutral user context.
func RenderBranchSummaryContext(summary string) string {
	var rendered strings.Builder
	contextTemplate := template.Must(template.New("branch_summary_context").Parse(branchSummaryContextText))
	if err := contextTemplate.Execute(&rendered, branchSummaryContextData{Summary: summary}); err != nil {
		panic(fmt.Sprintf("render embedded branch summary context: %v", err))
	}
	return rendered.String()
}
