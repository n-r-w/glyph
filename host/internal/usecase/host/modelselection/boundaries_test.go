//go:build !integration

package modelselection

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
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

// TestSelectionAdmissionIsSharedAcrossInitiators verifies UI, Programmatic, and Extension callers reserve one gate.
func TestSelectionAdmissionIsSharedAcrossInitiators(t *testing.T) {
	t.Parallel()

	// Arrange each public initiator as the owner of one accepted selection.
	tests := []struct {
		// name identifies the initiator that owns the reservation.
		name string
		// prepare acquires and returns the reservation release function.
		prepare func(*Service) (func(), error)
	}{
		{
			name: "UI",
			prepare: func(service *Service) (func(), error) {
				prepared, err := service.PrepareUISelection(hostui.ModelSelectionCommand{
					Kind:            hostui.ModelSelectionCommandModel,
					Provider:        "provider",
					Model:           "model",
					ReasoningChoice: "",
				})
				if err != nil {
					return nil, err
				}
				return prepared.Release, nil
			},
		},
		{
			name: "Programmatic",
			prepare: func(service *Service) (func(), error) {
				prepared, err := service.PrepareProgrammaticSelection(hostprogrammatic.ModelSelectionCommand{
					Kind:            hostprogrammatic.ModelSelectionCommandModel,
					Provider:        "provider",
					Model:           "model",
					ReasoningChoice: "",
				})
				if err != nil {
					return nil, err
				}
				return prepared.Release, nil
			},
		},
		{
			name: "Extension",
			prepare: func(service *Service) (func(), error) {
				prepared, err := service.PrepareExtensionSelection(extensioncontroller.SelectionCommand{
					Kind:        extensioncontroller.SelectionCommandModel,
					ExtensionID: "extension",
					RuntimeID:   "runtime",
					Context: extensiondomain.ContextRef{
						ID: "context", RuntimeInstanceID: "runtime", SessionID: "session",
					},
					Provider: "provider", Model: "model", ReasoningChoice: "",
				})
				if err != nil {
					return nil, err
				}
				return prepared.Release, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			catalog := NewMockCatalog(controller)
			publisher := NewMockPublisher(controller)
			target := model.Selection{
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow,
			}
			catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
			service := New(catalog, publisher)
			release, err := test.prepare(service)
			require.NoError(t, err)
			defer release()

			// Act: ask every public initiator to prepare while the first reservation is active.
			_, uiErr := service.PrepareUISelection(hostui.ModelSelectionCommand{
				Kind:            hostui.ModelSelectionCommandReasoning,
				Provider:        "",
				Model:           "",
				ReasoningChoice: model.ReasoningChoiceHigh,
			})
			_, programmaticErr := service.PrepareProgrammaticSelection(hostprogrammatic.ModelSelectionCommand{
				Kind:            hostprogrammatic.ModelSelectionCommandReasoning,
				Provider:        "",
				Model:           "",
				ReasoningChoice: model.ReasoningChoiceHigh,
			})
			_, extensionErr := service.PrepareExtensionSelection(extensioncontroller.SelectionCommand{
				Kind:        extensioncontroller.SelectionCommandReasoning,
				ExtensionID: "extension",
				RuntimeID:   "runtime",
				Context: extensiondomain.ContextRef{
					ID: "context", RuntimeInstanceID: "runtime", SessionID: "session",
				},
				Provider: "", Model: "", ReasoningChoice: model.ReasoningChoiceHigh,
			})

			// Assert: every initiator observes the same BUSY admission result.
			for _, admissionErr := range []error{uiErr, programmaticErr, extensionErr} {
				var selectionErr *SelectionError
				require.ErrorAs(t, admissionErr, &selectionErr)
				assert.Equal(t, ErrorCodeBusy, selectionErr.Code)
			}
		})
	}
}
