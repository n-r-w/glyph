package presentation

import (
	"strings"

	"github.com/samber/lo"
)

const (
	// treeOperationFailedText identifies a tree failure without a supplied diagnostic.
	treeOperationFailedText = "Session tree operation failed"
	// treeIssueSeparator joins ordered operation diagnostics.
	treeIssueSeparator = "; "
)

// formatOperationIssues constructs the operation-status text retained by tree interaction.
func formatOperationIssues(issues []OperationIssue) string {
	return strings.Join(
		lo.Map(issues, func(issue OperationIssue, _ int) string { return issue.Message }),
		treeIssueSeparator,
	)
}
