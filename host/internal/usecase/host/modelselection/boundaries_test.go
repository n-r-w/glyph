//go:build !integration

package modelselection

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// TestBoundaryResultsExposeExplicitDiagnostics verifies both consumers receive typed ordered result lists.
func TestBoundaryResultsExposeExplicitDiagnostics(t *testing.T) {
	t.Parallel()
	// Arrange one committed private result with handler, delivery, and observer causes.
	handlerErr := errors.New("ordinary handler failed")
	actionErr := errors.New("invalid handler action")
	deliveryErr := errors.New("selection event delivery failed")
	observerErr := errors.New("selection observer failed")
	result := selectionResult{
		selection: model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh},
		committed: true,
		issues: []Issue{
			{ExtensionID: "first", HandlerID: "ordinary", Code: IssueCodeHandlerError, Err: handlerErr},
			{ExtensionID: "second", HandlerID: "invalid", Code: IssueCodeInvalidHandlerAction, Err: actionErr},
			{ExtensionID: "third", HandlerID: "observer", Code: IssueCodeObserverError, Err: observerErr},
		},
		deliveryErr: deliveryErr,
		err:         nil,
	}

	// Act by projecting the same private result to both consumer-owned contracts.
	uiIssues := mapUIIssues(result)
	programmaticIssues := mapProgrammaticIssues(result)
	source := result.source()

	// Assert both lists retain order, typed codes, identity, text, and complete sources.
	require.Len(t, uiIssues, 4)
	assert.Equal(t, hostui.ModelSelectionIssueHandlerError, uiIssues[0].Kind)
	assert.Equal(t, "first", uiIssues[0].ExtensionID)
	assert.Equal(t, hostui.ModelSelectionIssueInvalidHandlerAction, uiIssues[1].Kind)
	assert.Equal(t, hostui.ModelSelectionIssueDeliveryFailed, uiIssues[2].Kind)
	assert.Equal(t, hostui.ModelSelectionIssueObserverError, uiIssues[3].Kind)
	require.Len(t, programmaticIssues, 4)
	assert.Equal(t, hostprogrammatic.ModelSelectionIssueHandlerError, programmaticIssues[0].Kind)
	assert.Equal(t, "ordinary", programmaticIssues[0].HandlerID)
	assert.Equal(t, hostprogrammatic.ModelSelectionIssueInvalidHandlerAction, programmaticIssues[1].Kind)
	assert.Equal(t, hostprogrammatic.ModelSelectionIssueDeliveryFailed, programmaticIssues[2].Kind)
	assert.Equal(t, hostprogrammatic.ModelSelectionIssueObserverError, programmaticIssues[3].Kind)
	assert.ErrorIs(t, source, handlerErr)
	assert.ErrorIs(t, source, actionErr)
	assert.ErrorIs(t, source, deliveryErr)
	assert.ErrorIs(t, source, observerErr)
}
