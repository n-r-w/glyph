//go:build !integration

package programmatic

import (
	"context"
	"errors"

	"github.com/samber/mo"

	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestSelectionHandlerDiagnosticsRemainOrdered verifies explicit result diagnostics reach Programmatic completion.
func (s *ServiceSuite) TestSelectionHandlerDiagnosticsRemainOrdered() {
	// Arrange one committed result with ordered handler and delivery diagnostics.
	ctrl := gomock.NewController(s.T())
	selectionOwner := NewMockModelSelection(ctrl)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	source := errors.New("ordinary failed\ninvalid action\nselection event delivery failed")
	resultIssues := []ModelSelectionIssue{
		{
			Kind:        ModelSelectionIssueHandlerError,
			ExtensionID: "first",
			HandlerID:   "ordinary",
			Message:     "ordinary failed",
		},
		{
			Kind:        ModelSelectionIssueInvalidHandlerAction,
			ExtensionID: "second",
			HandlerID:   "invalid",
			Message:     "invalid action",
		},
		{
			Kind:        ModelSelectionIssueDeliveryFailed,
			ExtensionID: "",
			HandlerID:   "",
			Message:     "selection event delivery failed",
		},
	}
	expectedIssues := projectModelSelectionIssues(resultIssues)
	expectProgrammaticSelection(ctrl, selectionOwner, ModelSelectionCommand{
		Kind: ModelSelectionCommandModel, Provider: selection.Provider, Model: selection.Model, ReasoningChoice: "",
	}, ModelSelectionResult{Selection: selection, Committed: true, Issues: resultIssues, Source: source})
	service := New(
		NewMockCoordinator(ctrl), NewMockModelCatalog(ctrl), testStateQuery(s.T(), false),
		nil, nil, nil, testRunOutput(s.T()), selectionOwner, nil,
	)
	command := testProgrammaticCommand("selection", controller.CommandSelectModel)
	command.ProviderID = mo.Some(selection.Provider)
	command.ModelID = mo.Some(selection.Model)
	prepared, err := service.Prepare(s.T().Context(), command)
	s.Require().NoError(err)
	defer prepared.Release()

	// Act by executing the admitted selection.
	outcome := prepared.Run(s.T().Context(), operation.Reporter[controller.OperationProgress]{})

	// Assert completion uses the explicit ordered list without decoding an error protocol.
	response, present := outcome.Result()
	s.True(present)
	s.Equal(expectedIssues, response.SelectionIssues)
	s.ErrorIs(outcome.SourceError(), source)
}

// TestCommandRejectionPrecedence verifies first-match evaluation for overlapping failures.
func (s *ServiceSuite) TestCommandRejectionPrecedence() {
	tests := []struct {
		name         string
		active       bool
		command      controller.Command
		prepareErr   error
		expectedCode controller.RejectionCode
		expectedType controller.CommandKind
	}{
		{
			name:         "missing payload precedes active operation and busy state",
			active:       true,
			command:      testProgrammaticCommand("active", controller.CommandUnspecified),
			expectedCode: controller.RejectionInvalidArgument,
			expectedType: controller.CommandUnspecified,
			prepareErr:   nil,
		},
		{
			name:         "blank user request precedes active operation and busy state",
			active:       true,
			command:      testProgrammaticUserCommand("active", " \t"),
			expectedCode: controller.RejectionInvalidArgument,
			expectedType: controller.CommandUserRequest,
			prepareErr:   nil,
		},
		{
			name:   "unexpected query payload precedes active operation",
			active: true,
			command: controller.Command{
				OperationID:     "active",
				Kind:            controller.CommandGetRunState,
				UserText:        mo.Some("unexpected"),
				ProviderID:      mo.None[model.ProviderID](),
				ModelID:         mo.None[model.ID](),
				ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:       mo.None[session.ID](),
				SessionName:     mo.None[string](),
				TargetEntryID:   mo.None[string](),
				SummaryMode:     controller.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
			},
			expectedCode: controller.RejectionInvalidArgument,
			expectedType: controller.CommandGetRunState,
			prepareErr:   nil,
		},
		{
			name:         "active operation precedes busy state",
			active:       true,
			command:      testProgrammaticUserCommand("active", "next"),
			expectedCode: controller.RejectionOperationIDInUse,
			expectedType: controller.CommandUserRequest,
			prepareErr:   nil,
		},
		{
			name: "busy state precedes allocation failure", active: true,
			command:    testProgrammaticUserCommand("other", "next"),
			prepareErr: errors.New("must not allocate"), expectedCode: controller.RejectionBusy,
			expectedType: controller.CommandUserRequest,
		},
		{
			name:         "controller-owned cancellation is invalid Host work",
			command:      testProgrammaticCommand("cancel", controller.CommandCancel),
			expectedCode: controller.RejectionInvalidArgument,
			expectedType: controller.CommandCancel,
			active:       false,
			prepareErr:   nil,
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			ctrl := gomock.NewController(s.T())
			coordinator := NewMockCoordinator(ctrl)
			coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
			service := New(
				coordinator,
				nil,
				testStateQuery(s.T(), false),
				nil, nil,
				nil, testRunOutput(s.T()), nil, nil)

			if test.active {
				coordinator.EXPECT().PrepareRun().Return("run-active", nil)
				_, operation, err := service.handle(s.T().Context(), controller.Command{
					OperationID:     "active",
					Kind:            controller.CommandUserRequest,
					UserText:        mo.Some("first"),
					ProviderID:      mo.None[model.ProviderID](),
					ModelID:         mo.None[model.ID](),
					ReasoningChoice: mo.None[model.ReasoningChoice](),
					SessionID:       mo.None[session.ID](),
					SessionName:     mo.None[string](),
					TargetEntryID:   mo.None[string](),
					SummaryMode:     controller.SummaryModeNoSummary,
					CustomFocus:     mo.None[string](),
					EntryLabel:      mo.None[string](),
				})
				s.Require().NoError(err)
				s.Require().NotNil(operation)
				defer operation.Release()
			}
			if test.expectedCode == controller.RejectionInternal {
				coordinator.EXPECT().PrepareRun().Return("", test.prepareErr)
			}

			response, operation, err := service.handle(s.T().Context(), test.command)

			s.Require().NoError(err)
			s.Nil(operation)
			s.Equal(test.command.OperationID, response.OperationID)
			s.Equal(controller.ResponseRejected, response.Kind)
			s.Equal(test.expectedType, response.Rejection.OrEmpty().Command)
			s.Equal(test.expectedCode, response.Rejection.OrEmpty().Code)
		})
	}
}

// TestModelCommandsUseCatalogDuringActiveRun verifies independent catalog commands.
func (s *ServiceSuite) TestModelCommandsUseCatalogDuringActiveRun() {
	// Arrange an active run and catalog responses for each independent model command.
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	catalog := NewMockModelCatalog(ctrl)
	selectionOwner := NewMockModelSelection(ctrl)
	service := New(
		coordinator,
		catalog,
		testStateQuery(s.T(), false),
		nil, nil,
		nil,
		testRunOutput(s.T()),
		selectionOwner, nil,
	)
	coordinator.EXPECT().PrepareRun().Return("run-active", nil)
	activeOperation, err := service.Prepare(s.T().Context(), controller.Command{
		OperationID:     "active",
		Kind:            controller.CommandUserRequest,
		UserText:        mo.Some("request"),
		ProviderID:      mo.None[model.ProviderID](),
		ModelID:         mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:       mo.None[session.ID](),
		SessionName:     mo.None[string](),
		TargetEntryID:   mo.None[string](),
		SummaryMode:     controller.SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	})
	s.Require().NoError(err)
	s.Require().NotNil(activeOperation)
	defer activeOperation.Release()

	type contextKey struct{}
	commandContext := context.WithValue(s.T().Context(), contextKey{}, "selection")
	models := []model.Descriptor{{
		Provider: "provider", Model: "model",
		Input: nil, ContextWindow: 0, MaxTokens: 0,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true, Choices: []model.ReasoningChoice{model.ReasoningChoiceLow, model.ReasoningChoiceHigh},
			Default: model.ReasoningChoiceLow,
		}, ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
	}}
	initial := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	selectedModel := model.Selection{Provider: "other", Model: "next", ReasoningChoice: model.ReasoningChoiceLow}
	selectedReasoning := model.Selection{Provider: "other", Model: "next", ReasoningChoice: model.ReasoningChoiceHigh}
	catalog.EXPECT().Models().Return(models)
	catalog.EXPECT().ActiveSelection().Return(initial)
	expectProgrammaticSelection(ctrl, selectionOwner, ModelSelectionCommand{
		Kind: ModelSelectionCommandModel, Provider: "other", Model: "next", ReasoningChoice: "",
	}, ModelSelectionResult{Selection: selectedModel, Committed: true, Issues: nil, Source: nil})
	expectProgrammaticSelection(ctrl, selectionOwner, ModelSelectionCommand{
		Kind: ModelSelectionCommandReasoning, Provider: "", Model: "", ReasoningChoice: model.ReasoningChoiceHigh,
	}, ModelSelectionResult{Selection: selectedReasoning, Committed: true, Issues: nil, Source: nil})

	tests := []struct {
		command controller.Command
		want    controller.Response
	}{
		{
			command: testProgrammaticCommand("models", controller.CommandGetModels),
			want: controller.Response{
				SessionEntries: nil,
				OperationID:    "models",
				Kind:           controller.ResponseModels,
				Models: mo.Some(
					controller.ModelsResult{Models: models, ActiveSelection: mo.Some(initial)},
				),
				State:             mo.None[controller.RunStateResult](),
				Messages:          nil,
				Selection:         mo.None[model.Selection](),
				Rejection:         mo.None[controller.Rejection](),
				SessionInfo:       mo.None[session.Info](),
				Sessions:          nil,
				SessionStatistics: mo.None[session.Statistics](),
				SessionTree:       mo.None[controller.SessionTree](),
				TreeNavigation:    mo.None[controller.TreeNavigationResult](),
				Replacement:       mo.None[controller.SessionReplacement](),
			},
		},
		{
			command: controller.Command{
				OperationID:     "model",
				Kind:            controller.CommandSelectModel,
				ProviderID:      mo.Some(model.ProviderID("other")),
				ModelID:         mo.Some(model.ID("next")),
				UserText:        mo.None[string](),
				ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:       mo.None[session.ID](),
				SessionName:     mo.None[string](),
				TargetEntryID:   mo.None[string](),
				SummaryMode:     controller.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
			},
			want: controller.Response{
				SessionEntries:    nil,
				OperationID:       "model",
				Kind:              controller.ResponseModelSelection,
				Selection:         mo.Some(selectedModel),
				State:             mo.None[controller.RunStateResult](),
				Messages:          nil,
				Models:            mo.None[controller.ModelsResult](),
				Rejection:         mo.None[controller.Rejection](),
				SessionInfo:       mo.None[session.Info](),
				Sessions:          nil,
				SessionStatistics: mo.None[session.Statistics](),
				SessionTree:       mo.None[controller.SessionTree](),
				TreeNavigation:    mo.None[controller.TreeNavigationResult](),
				Replacement:       mo.None[controller.SessionReplacement](),
			},
		},
		{
			command: controller.Command{
				OperationID: "reasoning",
				Kind:        controller.CommandSelectReasoningChoice,
				ReasoningChoice: mo.Some(
					model.ReasoningChoiceHigh,
				),
				UserText:      mo.None[string](),
				ProviderID:    mo.None[model.ProviderID](),
				ModelID:       mo.None[model.ID](),
				SessionID:     mo.None[session.ID](),
				SessionName:   mo.None[string](),
				TargetEntryID: mo.None[string](),
				SummaryMode:   controller.SummaryModeNoSummary,
				CustomFocus:   mo.None[string](),
				EntryLabel:    mo.None[string](),
			},
			want: controller.Response{
				SessionEntries:    nil,
				OperationID:       "reasoning",
				Kind:              controller.ResponseModelSelection,
				Selection:         mo.Some(selectedReasoning),
				State:             mo.None[controller.RunStateResult](),
				Messages:          nil,
				Models:            mo.None[controller.ModelsResult](),
				Rejection:         mo.None[controller.Rejection](),
				SessionInfo:       mo.None[session.Info](),
				Sessions:          nil,
				SessionStatistics: mo.None[session.Statistics](),
				SessionTree:       mo.None[controller.SessionTree](),
				TreeNavigation:    mo.None[controller.TreeNavigationResult](),
				Replacement:       mo.None[

				// Act by handling each catalog command while the run remains active.
				controller.SessionReplacement](),
			},
		},
	}

	for _, test := range tests {
		prepared, prepareErr := service.Prepare(commandContext, test.command)
		s.Require().NoError(prepareErr)
		outcome := prepared.Run(commandContext, operation.Reporter[controller.OperationProgress]{})
		prepared.Release()

		// Assert the public lifecycle completes with the exact catalogue response during the active run.
		s.Equal(operation.TerminalStateCompleted, outcome.State())
		response, present := outcome.Result()
		s.True(present)
		s.Equal(test.want, response)
	}
}

// TestInvalidModelCommandsDoNotCallCatalog verifies argument validation before selection.
func (s *ServiceSuite) TestInvalidModelCommandsDoNotCallCatalog() {
	ctrl := gomock.NewController(s.T())
	service := New(
		NewMockCoordinator(ctrl), NewMockModelCatalog(ctrl),
		testStateQuery(s.T(), false), nil, nil, nil, testRunOutput(s.T()), nil, nil)

	commands := []controller.Command{
		{
			OperationID:     "provider",
			Kind:            controller.CommandSelectModel,
			ModelID:         mo.Some(model.ID("model")),
			UserText:        mo.None[string](),
			ProviderID:      mo.None[model.ProviderID](),
			ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:       mo.None[session.ID](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     controller.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
		},
		{
			OperationID:     "model",
			Kind:            controller.CommandSelectModel,
			ProviderID:      mo.Some(model.ProviderID("provider")),
			UserText:        mo.None[string](),
			ModelID:         mo.None[model.ID](),
			ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:       mo.None[session.ID](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     controller.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
		},
		{
			OperationID:     "reasoning",
			Kind:            controller.CommandSelectReasoningChoice,
			UserText:        mo.None[string](),
			ProviderID:      mo.None[model.ProviderID](),
			ModelID:         mo.None[model.ID](),
			ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:       mo.None[session.ID](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     controller.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](),
			EntryLabel:      mo.None[string](),
		},
	}
	for _, command := range commands {
		prepared, err := service.Prepare(s.T().Context(), command)
		s.Nil(prepared)
		var rejection *controller.RejectionError
		s.Require().ErrorAs(err, &rejection)
		s.Equal(controller.RejectionCodeInvalidArgument, rejection.Code())
	}
}

// TestSelectionPublicationFailureCompletesWithCommittedState verifies post-commit diagnostics do not report failure.
func (s *ServiceSuite) TestSelectionPublicationFailureCompletesWithCommittedState() {
	// Arrange a shared operation that committed before full delivery failed.
	ctrl := gomock.NewController(s.T())
	selectionOwner := NewMockModelSelection(ctrl)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	deliveryErr := errors.New("selection delivery failed")
	expectProgrammaticSelection(ctrl, selectionOwner, ModelSelectionCommand{
		Kind: ModelSelectionCommandModel, Provider: selection.Provider, Model: selection.Model, ReasoningChoice: "",
	}, ModelSelectionResult{
		Selection: selection, Committed: true,
		Issues: []ModelSelectionIssue{{
			Kind: ModelSelectionIssueDeliveryFailed, ExtensionID: "", HandlerID: "", Message: deliveryErr.Error(),
		}},
		Source: deliveryErr,
	})
	service := New(
		NewMockCoordinator(ctrl), NewMockModelCatalog(ctrl), testStateQuery(s.T(), false),
		nil, nil, nil, testRunOutput(s.T()), selectionOwner, nil,
	)
	command := testProgrammaticCommand("selection", controller.CommandSelectModel)
	command.ProviderID = mo.Some(selection.Provider)
	command.ModelID = mo.Some(selection.Model)
	prepared, err := service.Prepare(s.T().Context(), command)
	s.Require().NoError(err)
	defer prepared.Release()

	// Act by executing the admitted selection.
	outcome := prepared.Run(s.T().Context(), operation.Reporter[controller.OperationProgress]{})

	// Assert terminal completion retains both committed state and complete diagnostics.
	s.Equal(operation.TerminalStateCompleted, outcome.State())
	response, present := outcome.Result()
	s.True(present)
	s.Equal(selection, response.Selection.MustGet())
	s.Require().Len(response.SelectionIssues, 1)
	s.Equal(controller.OperationIssueDeliveryFailed, response.SelectionIssues[0].Code)
	s.Equal(deliveryErr.Error(), response.SelectionIssues[0].Message)
	s.ErrorIs(outcome.SourceError(), deliveryErr)
}

// TestSelectionErrorsPreservePublicCodesAndCauses verifies preparation and execution error projection.
func (s *ServiceSuite) TestSelectionErrorsPreservePublicCodesAndCauses() {
	tests := []struct {
		name         string
		err          error
		preparation  bool
		expectedCode string
	}{
		{
			name: "not found", err: selectionError{code: SelectionNotFound}, preparation: true,
			expectedCode: controller.RejectionCodeNotFound,
		},
		{
			name: "reasoning unsupported", err: selectionError{code: SelectionReasoningUnsupported}, preparation: true,
			expectedCode: controller.RejectionCodeReasoningUnsupported,
		},
		{
			name:         "credential unavailable",
			err:          selectionError{code: SelectionCredentialUnavailable},
			preparation:  false,
			expectedCode: controller.FailureCodeCredentialUnavailable,
		},
		{
			name: "final model unavailable", err: selectionError{code: SelectionModelUnavailable}, preparation: false,
			expectedCode: controller.FailureCodeModelUnavailable,
		},
		{
			name: "extension rejected", err: selectionError{code: SelectionExtensionRejected}, preparation: false,
			expectedCode: controller.FailureCodeExtensionRejected,
		},
		{
			name: "extension unavailable", err: selectionError{code: SelectionExtensionUnavailable}, preparation: false,
			expectedCode: controller.FailureCodeExtensionUnavailable,
		},
		{
			name: "internal", err: errors.New("internal details"), preparation: false,
			expectedCode: controller.FailureCodeInternal,
		},
	}
	for _, test := range tests {
		s.Run(test.name, func() {
			ctrl := gomock.NewController(s.T())
			selectionOwner := NewMockModelSelection(ctrl)
			service := New(
				NewMockCoordinator(ctrl), NewMockModelCatalog(ctrl),
				testStateQuery(s.T(), false), nil, nil, nil, testRunOutput(s.T()), selectionOwner, nil,
			)
			selectionCommand := ModelSelectionCommand{
				Kind: ModelSelectionCommandModel, Provider: "provider", Model: "model", ReasoningChoice: "",
			}
			if test.preparation {
				selectionOwner.EXPECT().PrepareProgrammaticSelection(selectionCommand).Return(nil, test.err)
			} else {
				expectProgrammaticSelection(ctrl, selectionOwner, selectionCommand, ModelSelectionResult{
					Selection: model.Selection{}, Committed: false, Issues: nil, Source: test.err,
				})
			}
			command := testProgrammaticCommand("selection", controller.CommandSelectModel)
			command.ProviderID = mo.Some(model.ProviderID("provider"))
			command.ModelID = mo.Some(model.ID("model"))

			prepared, err := service.Prepare(s.T().Context(), command)
			if test.preparation {
				s.Nil(prepared)
				var rejection *controller.RejectionError
				s.Require().ErrorAs(err, &rejection)
				s.Equal(test.expectedCode, rejection.Code())
				s.ErrorContains(err, test.err.Error())
				return
			}
			s.Require().NoError(err)
			outcome := prepared.Run(s.T().Context(), operation.Reporter[controller.OperationProgress]{})
			prepared.Release()
			s.Equal(operation.TerminalStateFailed, outcome.State())
			s.Equal(test.expectedCode, outcome.Code())
			s.ErrorContains(outcome.SourceError(), test.err.Error())
		})
	}
}

// expectProgrammaticSelection configures generated mocks for one explicit consumer-owned result.
func expectProgrammaticSelection(
	controller *gomock.Controller,
	owner *MockModelSelection,
	command ModelSelectionCommand,
	result ModelSelectionResult,
) {
	prepared := NewMockPreparedModelSelection(controller)
	owner.EXPECT().PrepareProgrammaticSelection(command).Return(prepared, nil)
	prepared.EXPECT().Run(gomock.Any()).Return(result)
	prepared.EXPECT().Release()
}
