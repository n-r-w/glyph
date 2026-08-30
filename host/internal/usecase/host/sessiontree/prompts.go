package sessiontree

import (
	_ "embed"
	"strings"

	"github.com/samber/mo"
)

const (
	// branchSummaryConversationOpening starts source data in the provider user-role message.
	branchSummaryConversationOpening = "<conversation>\n"
	// branchSummaryConversationClosing ends source data in the provider user-role message.
	branchSummaryConversationClosing = "\n</conversation>\n"
	// branchSummaryFocusOpening starts optional caller focus in the provider user-role message.
	branchSummaryFocusOpening = "\n<additional_focus>\n"
	// branchSummaryFocusClosing ends optional caller focus in the provider user-role message.
	branchSummaryFocusClosing = "\n</additional_focus>\n"
	// branchSummaryTaskOpening starts task text in the provider user-role message.
	branchSummaryTaskOpening = "\n<task>\n"
	// branchSummaryTaskClosing ends task text in the provider user-role message.
	branchSummaryTaskClosing = "</task>"
)

var (
	// branchSummarySystemText contains the provider system-role rules.
	//go:embed prompts/branch_summary_system.md
	branchSummarySystemText string
	// branchSummaryTaskText contains task text inserted into the provider user-role message.
	//go:embed prompts/branch_summary_task.md
	branchSummaryTaskText string
)

// renderBranchSummaryUserInput builds one provider user-role message from source data, optional focus, and task text.
func renderBranchSummaryUserInput(conversation string, additionalFocus mo.Option[string]) string {
	var input strings.Builder
	input.WriteString(branchSummaryConversationOpening)
	input.WriteString(conversation)
	input.WriteString(branchSummaryConversationClosing)
	if focus, present := additionalFocus.Get(); present {
		input.WriteString(branchSummaryFocusOpening)
		input.WriteString(escapeXMLText(focus))
		input.WriteString(branchSummaryFocusClosing)
	}
	input.WriteString(branchSummaryTaskOpening)
	input.WriteString(branchSummaryTaskText)
	if !strings.HasSuffix(branchSummaryTaskText, "\n") {
		input.WriteByte('\n')
	}
	input.WriteString(branchSummaryTaskClosing)
	return input.String()
}
