package modelselection

import (
	"context"
	"fmt"

	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
)

// extensionPrepared projects one private selection operation to the extension controller boundary.
type extensionPrepared struct {
	// prepared owns shared selection admission and execution.
	prepared *preparedSelection
}

var _ extensioncontroller.PreparedSelection = (*extensionPrepared)(nil)

// PrepareExtensionSelection prepares one bound extension selection through shared admission.
func (s *Service) PrepareExtensionSelection(
	command extensioncontroller.SelectionCommand,
) (extensioncontroller.PreparedSelection, error) {
	var prepared *preparedSelection
	var err error
	switch command.Kind {
	case extensioncontroller.SelectionCommandModel:
		prepared, err = s.prepareModel(command.Provider, command.Model)
	case extensioncontroller.SelectionCommandReasoning:
		prepared, err = s.prepareReasoning(command.ReasoningChoice)
	default:
		return nil, fmt.Errorf("unsupported extension model selection command %d", command.Kind)
	}
	if err != nil {
		return nil, err
	}
	prepared.binding = mo.Some(Binding{
		ExtensionID: command.ExtensionID, RuntimeID: command.RuntimeID, Context: command.Context,
	})
	return &extensionPrepared{prepared: prepared}, nil
}

// Run executes one bound extension selection and projects its committed result and diagnostics.
func (p *extensionPrepared) Run(ctx context.Context) extensioncontroller.SelectionResult {
	result := p.prepared.Run(ctx)
	projected := projectIssues(result)
	issues := make([]extensioncontroller.SelectionIssue, 0, len(projected))
	for issueIndex := range projected {
		issue := &projected[issueIndex]
		issues = append(issues, extensioncontroller.SelectionIssue{
			ExtensionID: issue.extensionID, HandlerID: issue.handlerID, Code: issue.code, Message: issue.message,
		})
	}
	return extensioncontroller.SelectionResult{
		Selection: result.selection, Committed: result.committed, Issues: issues, Source: result.source(),
	}
}

// Release releases shared selection admission exactly once.
func (p *extensionPrepared) Release() { p.prepared.Release() }
