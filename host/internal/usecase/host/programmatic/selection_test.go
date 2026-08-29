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
			name: "missing payload precedes active correlation and busy state", active: true,
			command: controller.Command{CorrelationID: "active", Kind: controller.CommandUnspecified, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			expectedCode: controller.RejectionInvalidArgument, expectedType: controller.CommandUnspecified, prepareErr: nil,
		},
		{
			name: "blank user request precedes active correlation and busy state", active: true,
			command: controller.Command{CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some(" \t"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			expectedCode: controller.RejectionInvalidArgument, expectedType: controller.CommandUserRequest, prepareErr: nil,
		},
		{
			name: "unexpected query payload precedes active correlation", active: true,
			command: controller.Command{CorrelationID: "active", Kind: controller.CommandGetRunState, UserText: mo.Some("unexpected"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			expectedCode: controller.RejectionInvalidArgument, expectedType: controller.CommandGetRunState, prepareErr: nil,
		},
		{
			name: "active correlation precedes busy state", active: true,
			command: controller.Command{CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("next"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			expectedCode: controller.RejectionCorrelationInUse, expectedType: controller.CommandUserRequest, prepareErr: nil,
		},
		{
			name: "busy state precedes allocation failure", active: true,
			command: controller.Command{CorrelationID: "other", Kind: controller.CommandUserRequest, UserText: mo.Some("next"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			prepareErr: errors.New("must not allocate"), expectedCode: controller.RejectionBusy,
			expectedType: controller.CommandUserRequest,
		},
		{
			name: "abort without active run", command: controller.Command{CorrelationID: "abort", Kind: controller.CommandAbort, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			expectedCode: controller.RejectionNoActiveRun, expectedType: controller.CommandAbort, active: false, prepareErr: nil,
		},
		{
			name: "allocation failure after valid idle request",
			command: controller.Command{CorrelationID: "request", Kind: controller.CommandUserRequest, UserText: mo.Some("next"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			prepareErr: errors.New("entropy failed"), expectedCode: controller.RejectionInternal,
			expectedType: controller.CommandUserRequest, active: false,
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
				_, operation, err := service.Handle(s.T().Context(), controller.Command{
					CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("first"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
					SessionID:   mo.None[session.ID](),
					SessionName: mo.None[string](),
				})
				s.Require().NoError(err)
				s.Require().NotNil(operation)
				defer func() { s.Require().NoError(service.CancelAndWait(s.T().Context())) }()
			}
			if test.expectedCode == controller.RejectionInternal {
				coordinator.EXPECT().PrepareRun().Return("", test.prepareErr)
			}

			response, operation, err := service.Handle(s.T().Context(), test.command)

			s.Require().NoError(err)
			s.Nil(operation)
			s.Equal(test.command.CorrelationID, response.CorrelationID)
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
	_, activeOperation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("request"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:   mo.None[session.ID](),
		SessionName: mo.None[string](),
	})
	s.Require().NoError(err)
	s.Require().NotNil(activeOperation)
	defer func() { s.Require().NoError(service.CancelAndWait(s.T().Context())) }()

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
	catalog.EXPECT().Selection().Return(initial)
	catalog.EXPECT().SelectModel(gomock.Eq(commandContext), model.ProviderID("other"), model.ID("next")).Return(selectedModel, nil)
	catalog.EXPECT().SelectReasoningChoice(model.ReasoningChoiceHigh).Return(selectedReasoning, nil)

	tests := []struct {
		command controller.Command
		want    controller.Response
	}{
		{
			command: controller.Command{CorrelationID: "models", Kind: controller.CommandGetModels, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string]()},
			want: controller.Response{
				SessionEntries: nil,
				CorrelationID:  "models", Kind: controller.ResponseModels,
				Models: mo.Some(controller.ModelsResult{Models: models, ActiveSelection: mo.Some(initial)}), State: mo.None[controller.RunStateResult](), Messages: nil, Selection: mo.None[model.Selection](), Rejection: mo.None[controller.Rejection](),
				SessionInfo:       mo.None[session.Info](),
				Sessions:          nil,
				SessionStatistics: mo.None[session.Statistics](),
			},
		},
		{
			command: controller.Command{
				CorrelationID: "model", Kind: controller.CommandSelectModel, ProviderID: mo.Some(model.ProviderID("other")), ModelID: mo.Some(model.ID("next")), UserText: mo.None[string](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string](),
			},
			want: controller.Response{
				SessionEntries: nil,
				CorrelationID:  "model", Kind: controller.ResponseModelSelection, Selection: mo.Some(selectedModel), State: mo.None[controller.RunStateResult](), Messages: nil, Models: mo.None[controller.ModelsResult](), Rejection: mo.None[controller.Rejection](),
				SessionInfo:       mo.None[session.Info](),
				Sessions:          nil,
				SessionStatistics: mo.None[session.Statistics](),
			},
		},
		{
			command: controller.Command{
				CorrelationID: "reasoning", Kind: controller.CommandSelectReasoningChoice,
				ReasoningChoice: mo.Some(model.ReasoningChoiceHigh), UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string](),
			},
			want: controller.Response{
				SessionEntries: nil,
				CorrelationID:  "reasoning", Kind: controller.ResponseModelSelection, Selection: mo.Some(selectedReasoning), State: mo.None[controller.RunStateResult](), Messages: nil, Models: mo.None[controller.ModelsResult](), Rejection: mo.None[controller.Rejection](),
				SessionInfo:       mo.None[session.Info](),
				Sessions:          nil,
				SessionStatistics: mo.None[session.Statistics](),
			},
		},
	}

	// Act by handling each catalog command while the run remains active.
	for _, test := range tests {
		response, operation, handleErr := service.Handle(commandContext, test.command)

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
		{CorrelationID: "provider", Kind: controller.CommandSelectModel, ModelID: mo.Some(model.ID("model")), UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string]()},
		{CorrelationID: "model", Kind: controller.CommandSelectModel, ProviderID: mo.Some(model.ProviderID("provider")), UserText: mo.None[string](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string]()},
		{CorrelationID: "reasoning", Kind: controller.CommandSelectReasoningChoice, UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string]()},
	}
	for _, command := range commands {
		response, operation, err := service.Handle(s.T().Context(), command)
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
			catalog.EXPECT().SelectModel(gomock.Any(), model.ProviderID("provider"), model.ID("model")).Return(model.Selection{}, test.err)

			response, operation, err := service.Handle(s.T().Context(), controller.Command{
				CorrelationID: "selection", Kind: controller.CommandSelectModel,
				ProviderID: mo.Some(model.ProviderID("provider")), ModelID: mo.Some(model.ID("model")), UserText: mo.None[string](), ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:   mo.None[session.ID](),
				SessionName: mo.None[string](),
			})
			s.Require().NoError(err)
			s.Nil(operation)
			s.Equal(controller.ResponseRejected, response.Kind)
			s.Equal(test.code, response.Rejection.OrEmpty().Code)
			s.Contains(response.Rejection.OrEmpty().Message, test.err.Error())
		})
	}
}
