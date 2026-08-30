package sessiontree

import (
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/samber/mo"
)

const (
	// branchSummaryTaskTemplateName identifies the embedded summary task template.
	branchSummaryTaskTemplateName = "branch_summary_task"
	// branchSummaryContextTemplateName identifies the embedded active-history template.
	branchSummaryContextTemplateName = "branch_summary_context"
)

var (
	// branchSummarySystemText contains the embedded static generation rules.
	//go:embed prompts/branch_summary_system.md
	branchSummarySystemText string
	// branchSummaryTaskText contains the embedded user-task layout.
	//go:embed prompts/branch_summary_task.md
	branchSummaryTaskText string
	// branchSummaryContextText contains the embedded active-history layout.
	//go:embed prompts/branch_summary_context.md
	branchSummaryContextText string
)

// branchSummaryTaskData contains dynamic values for the embedded user task.
type branchSummaryTaskData struct {
	// Conversation contains the serialized source conversation.
	Conversation string
	// AdditionalFocus contains escaped caller focus only in custom-prompt mode.
	AdditionalFocus string
	// HasAdditionalFocus controls whether the complete optional focus block is present.
	HasAdditionalFocus bool
}

// branchSummaryContextData contains persisted text for the embedded active-history template.
type branchSummaryContextData struct {
	// Summary contains persisted summary text inserted unchanged.
	Summary string
}

// renderBranchSummaryTask executes the embedded user task with serialized conversation and optional focus.
func renderBranchSummaryTask(conversation string, additionalFocus mo.Option[string]) (string, error) {
	focus, hasAdditionalFocus := additionalFocus.Get()
	var rendered strings.Builder
	taskTemplate := template.Must(template.New(branchSummaryTaskTemplateName).Parse(branchSummaryTaskText))
	if err := taskTemplate.Execute(
		&rendered,
		branchSummaryTaskData{
			Conversation: conversation, AdditionalFocus: escapeXMLText(focus), HasAdditionalFocus: hasAdditionalFocus,
		},
	); err != nil {
		return "", fmt.Errorf("render branch summary task: %w", err)
	}
	return rendered.String(), nil
}

// RenderBranchSummaryContext renders one persisted summary as provider-neutral user context.
func RenderBranchSummaryContext(summary string) string {
	var rendered strings.Builder
	contextTemplate := template.Must(template.New(branchSummaryContextTemplateName).Parse(branchSummaryContextText))
	if err := contextTemplate.Execute(&rendered, branchSummaryContextData{Summary: summary}); err != nil {
		panic(fmt.Sprintf("render embedded branch summary context: %v", err))
	}
	return rendered.String()
}
