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
)

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
			service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
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
				defer func() { s.Require().NoError(cancelActiveTestRun(service)) }()
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
	service := New(coordinator, catalog, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
	coordinator.EXPECT().PrepareRun().Return("run-active", nil)
	_, activeOperation, err := service.handle(s.T().Context(), controller.Command{
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
	defer func() { s.Require().NoError(cancelActiveTestRun(service)) }()

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
	catalog.EXPECT().
		SelectModel(gomock.Eq(commandContext), model.ProviderID("other"), model.ID("next")).
		Return(selectedModel, nil)
	catalog.EXPECT().SelectReasoningChoice(model.ReasoningChoiceHigh).Return(selectedReasoning, nil)

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
		response, operation, handleErr := service.handle(commandContext, test.command)

		// Assert the command returns its exact catalog response without a new operation.
		s.Require().NoError(handleErr)
		s.Nil(operation)
		s.Equal(test.want, response)
	}
}

// TestInvalidModelCommandsDoNotCallCatalog verifies argument validation before selection.
func (s *ServiceSuite) TestInvalidModelCommandsDoNotCallCatalog() {
	ctrl := gomock.NewController(s.T())
	service := New(
		NewMockCoordinator(ctrl), NewMockModelCatalog(ctrl),
		idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery(),
	)

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
		response, operation, err := service.handle(s.T().Context(), command)
		s.Require().NoError(err)
		s.Nil(operation)
		s.Equal(controller.ResponseRejected, response.Kind)
		s.Equal(controller.RejectionInvalidArgument, response.Rejection.OrEmpty().Code)
		s.Equal(command.Kind, response.Rejection.OrEmpty().Command)
	}
}

// TestSelectionErrorsPreserveRejectionCodesAndCauses verifies the catalog error boundary.
func (s *ServiceSuite) TestSelectionErrorsPreserveRejectionCodesAndCauses() {
	tests := []struct {
		name string
		err  error
		code controller.RejectionCode
	}{
		{
			name: "not found",
			err:  selectionError{code: SelectionNotFound},
			code: controller.RejectionNotFound,
		},
		{
			name: "reasoning unsupported",
			err:  selectionError{code: SelectionReasoningUnsupported},
			code: controller.RejectionReasoningUnsupported,
		},
		{
			name: "credential unavailable",
			err:  selectionError{code: SelectionCredentialUnavailable},
			code: controller.RejectionCredentialUnavailable,
		},
		{name: "internal", err: errors.New("internal details"), code: controller.RejectionInternal},
	}
	for _, test := range tests {
		s.Run(test.name, func() {
			ctrl := gomock.NewController(s.T())
			catalog := NewMockModelCatalog(ctrl)
			service := New(
				NewMockCoordinator(ctrl), catalog,
				idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery(),
			)
			catalog.EXPECT().
				SelectModel(gomock.Any(), model.ProviderID("provider"), model.ID("model")).
				Return(model.Selection{}, test.err)

			response, operation, err := service.handle(s.T().Context(), controller.Command{
				OperationID: "selection",
				Kind:        controller.CommandSelectModel,
				ProviderID: mo.Some(
					model.ProviderID("provider"),
				),
				ModelID:         mo.Some(model.ID("model")),
				UserText:        mo.None[string](),
				ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:       mo.None[session.ID](),
				SessionName:     mo.None[string](),
				TargetEntryID:   mo.None[string](),
				SummaryMode:     controller.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
			})
			s.Require().NoError(err)
			s.Nil(operation)
			s.Equal(controller.ResponseRejected, response.Kind)
			s.Equal(test.code, response.Rejection.OrEmpty().Code)
			s.Contains(response.Rejection.OrEmpty().Message, test.err.Error())
		})
	}
}
