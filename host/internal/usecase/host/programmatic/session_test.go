//go:build !integration

package programmatic

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestRunPreparationRejectionPreservesClassificationAndCause verifies preparation errors retain domain behavior and
// details.
func TestRunPreparationRejectionPreservesClassificationAndCause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		prepareErr      error
		expectedCode    controller.RejectionCode
		expectedMessage string
	}{
		{
			name:            "busy",
			prepareErr:      session.ErrBusy,
			expectedCode:    controller.RejectionBusy,
			expectedMessage: "another operation is active",
		},
		{
			name:            "internal",
			prepareErr:      errors.New("allocate unique run ID"),
			expectedCode:    controller.RejectionInternal,
			expectedMessage: "Host run ID allocation failed: allocate unique run ID",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a coordinator that rejects run preparation.
			controllerMock := gomock.NewController(t)
			coordinator := NewMockCoordinator(controllerMock)
			coordinator.EXPECT().PrepareRun().Return("", test.prepareErr)
			service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())

			// Act by handling a user request while run preparation fails.
			response, operation, err := service.Handle(t.Context(), controller.Command{
				CorrelationID:   test.name,
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

			// Assert domain classification remains stable and internal details reach the rejection boundary.
			require.NoError(t, err)
			assert.Nil(t, operation)
			assert.Equal(t, test.expectedCode, response.Rejection.MustGet().Code)
			assert.Equal(t, test.expectedMessage, response.Rejection.MustGet().Message)
		})
	}
}

// TestSessionReplacementPreservesNondefaultModelSelection verifies create and resume keep runtime selection unchanged.
func TestSessionReplacementPreservesNondefaultModelSelection(t *testing.T) {
	t.Parallel()

	// Arrange create and resume commands around one nondefault provider, model, and reasoning selection.
	commandWithoutArguments := func(correlationID string, kind controller.CommandKind) controller.Command {
		return testProgrammaticCommand(correlationID, kind)
	}
	for _, test := range []struct {
		name string
		kind controller.CommandKind
	}{
		{name: "create", kind: controller.CommandCreateSession},
		{name: "resume", kind: controller.CommandResumeSession},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mockController := gomock.NewController(t)
			coordinator := NewMockCoordinator(mockController)
			catalog := NewMockModelCatalog(mockController)
			sessions := NewMockSessionControl(mockController)
			selection := model.Selection{
				Provider: "secondary-provider", Model: "secondary-model", ReasoningChoice: model.ReasoningChoiceHigh,
			}
			catalog.EXPECT().Models().Return(nil).Times(2)
			catalog.EXPECT().ActiveSelection().Return(selection).Times(2)
			info := session.Info{
				ID: "session-id", Name: mo.Some("session"), WorkingDirectory: "/project",
				StoragePath: mo.Some("/sessions/session.jsonl"), CreatedAt: time.Time{}, UpdatedAt: time.Time{},
			}
			if test.kind == controller.CommandCreateSession {
				sessions.EXPECT().Create(gomock.Any()).Return(session.Replacement{Info: info, Entries: nil}, nil)
			} else {
				sessions.EXPECT().Resume(gomock.Any(), session.ID("session-id")).Return(
					session.Replacement{Info: info, Entries: nil}, nil,
				)
			}
			service := New(coordinator, catalog, idleStateSnapshot, emptyHistorySnapshot, sessions, NewDelivery())

			// Act by reading selection before and after replacing the active session.
			before, _, err := service.Handle(
				t.Context(),
				commandWithoutArguments("before", controller.CommandGetModels),
			)
			require.NoError(t, err)
			command := commandWithoutArguments("replace", test.kind)
			if test.kind == controller.CommandResumeSession {
				command.SessionID = mo.Some(session.ID("session-id"))
			}
			_, _, err = service.Handle(t.Context(), command)
			require.NoError(t, err)
			after, _, err := service.Handle(t.Context(), commandWithoutArguments("after", controller.CommandGetModels))

			// Assert provider, model, and reasoning selection remain identical across replacement.
			require.NoError(t, err)
			require.Equal(t, mo.Some(selection), before.Models.MustGet().ActiveSelection)
			require.Equal(t, before.Models.MustGet().ActiveSelection, after.Models.MustGet().ActiveSelection)
		})
	}
}

// TestSessionErrorsUsePublicRejectionCodes verifies each session failure has one safe public code.
func TestSessionErrorsUsePublicRejectionCodes(t *testing.T) {
	t.Parallel()

	// Arrange domain, lookup, and persistence failures with expected public codes.
	tests := []struct {
		name            string
		kind            controller.CommandKind
		sessionID       mo.Option[session.ID]
		sessionName     mo.Option[string]
		operationErr    error
		expected        controller.RejectionCode
		expectedMessage string
	}{
		{
			name:            "busy",
			kind:            controller.CommandCreateSession,
			sessionID:       mo.None[session.ID](),
			sessionName:     mo.None[string](),
			operationErr:    session.ErrBusy,
			expected:        controller.RejectionBusy,
			expectedMessage: "another operation is active",
		},
		{
			name:            "invalid name",
			kind:            controller.CommandSetSessionName,
			sessionID:       mo.None[session.ID](),
			sessionName:     mo.Some("invalid"),
			operationErr:    session.ErrInvalidName,
			expected:        controller.RejectionInvalidArgument,
			expectedMessage: "session name is required",
		},
		{
			name:            "unknown ID",
			kind:            controller.CommandResumeSession,
			sessionID:       mo.Some(session.ID("missing")),
			sessionName:     mo.None[string](),
			operationErr:    os.ErrNotExist,
			expected:        controller.RejectionNotFound,
			expectedMessage: "session was not found",
		},
		{
			name:        "unavailable session",
			kind:        controller.CommandResumeSession,
			sessionID:   mo.Some(session.ID("stored")),
			sessionName: mo.None[string](),
			operationErr: fmt.Errorf(
				"load session: %w: decode record 7: unique parser failure",
				session.ErrUnavailable,
			),
			expected:        controller.RejectionSessionUnavailable,
			expectedMessage: "load session: session is unavailable: decode record 7: unique parser failure",
		},
		{
			name:            "persistence unavailable",
			kind:            controller.CommandSetSessionName,
			sessionID:       mo.None[session.ID](),
			sessionName:     mo.Some("session name"),
			operationErr:    fmt.Errorf("rename session: %w: disk sync failed", session.ErrPersistenceUnavailable),
			expected:        controller.RejectionPersistenceUnavailable,
			expectedMessage: "rename session: session persistence failed: disk sync failed",
		},
		{
			name:            "internal",
			kind:            controller.CommandCreateSession,
			sessionID:       mo.None[session.ID](),
			sessionName:     mo.None[string](),
			operationErr:    errors.New("create session invariant failed"),
			expected:        controller.RejectionInternal,
			expectedMessage: "session operation failed: create session invariant failed",
		},
	}

	// Act by handling each failing session command in an independent subtest.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			control := NewMockSessionControl(gomock.NewController(t))
			switch test.kind {
			case controller.CommandCreateSession:
				control.EXPECT().Create(gomock.Any()).Return(session.Replacement{}, test.operationErr)
			case controller.CommandResumeSession:
				control.EXPECT().
					Resume(gomock.Any(), test.sessionID.MustGet()).
					Return(session.Replacement{}, test.operationErr)
			case controller.CommandSetSessionName:
				control.EXPECT().
					SetName(gomock.Any(), test.sessionName.MustGet()).
					Return(session.Info{}, test.operationErr)
			case controller.CommandUnspecified, controller.CommandUserRequest, controller.CommandAbort,
				controller.CommandGetRunState, controller.CommandGetMessages, controller.CommandGetModels,
				controller.CommandSelectModel, controller.CommandSelectReasoningChoice,
				controller.CommandListSessions, controller.CommandGetSessionInfo,
				controller.CommandGetSessionEntries, controller.CommandGetSessionStats,
				controller.CommandGetSessionTree, controller.CommandNavigateSessionTree,
				controller.CommandForkSession, controller.CommandCloneSession, controller.CommandSetEntryLabel:
				t.Fatalf("unsupported command kind %d", test.kind)
			}
			service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
			response, operation, err := service.Handle(t.Context(), controller.Command{
				CorrelationID:   test.name,
				Kind:            test.kind,
				UserText:        mo.None[string](),
				ProviderID:      mo.None[model.ProviderID](),
				ModelID:         mo.None[model.ID](),
				ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:       test.sessionID,
				SessionName:     test.sessionName,
				TargetEntryID:   mo.None[string](),
				SummaryMode:     controller.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
			})
			require.NoError(t, err)
			assert.Nil(t, operation)

			// Assert the failure maps to the established rejection code and exact safe message without an operation.
			assert.Equal(t, test.expected, response.Rejection.MustGet().Code)
			assert.Equal(t, test.expectedMessage, response.Rejection.MustGet().Message)
		})
	}
}

// TestInvalidStoredSessionEntryProjectionIsRejected verifies invalid stored content returns a safe rejection.
func TestInvalidStoredSessionEntryProjectionIsRejected(t *testing.T) {
	t.Parallel()

	// Arrange session control to return an invalid stored model response.
	control := NewMockSessionControl(gomock.NewController(t))
	control.EXPECT().
		Entries().
		Return([]session.Entry{
			{
				ParentID:      mo.None[string](),
				ID:            "model",
				CreatedAt:     time.Unix(1, 0),
				Information:   mo.None[session.Information](),
				User:          mo.None[session.UserMessage](),
				Model:         mo.Some(invalidStoredModelResponse()),
				ToolResult:    mo.None[session.ToolResult](),
				Extension:     mo.None[session.ExtensionEnvelope](),
				EstimatedCost: mo.None[session.EstimatedCost](),
				BranchSummary: mo.None[session.BranchSummaryEntry](),
			},
		})
	service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())

	// Act by requesting the active session entries.
	response, operation, err := service.Handle(
		t.Context(),
		testProgrammaticCommand("entries", controller.CommandGetSessionEntries),
	)

	// Assert the service returns the mapping cause with an internal rejection and no partial entries.
	require.NoError(t, err)
	require.Nil(t, operation)
	require.Equal(t, controller.ResponseRejected, response.Kind)
	require.True(t, response.Rejection.IsPresent())
	require.Equal(t, controller.RejectionInternal, response.Rejection.MustGet().Code)
	require.Contains(t, response.Rejection.MustGet().Message, "invalid payload fields for kind 2")
	require.Nil(t, response.SessionEntries)
}

// TestSessionLifecycleCommands verifies each lifecycle command dispatches and returns its public response kind.
func TestSessionLifecycleCommands(t *testing.T) {
	t.Parallel()

	// Arrange create, list, resume, name, and information commands with session-control expectations.
	info := session.Info{
		ID:               "session-id",
		Name:             mo.Some("named"),
		WorkingDirectory: "/project",
		StoragePath:      mo.Some("/sessions/session-id.jsonl"),
		CreatedAt:        time.Unix(1, 0),
		UpdatedAt:        time.Unix(2, 0),
	}
	tests := []struct {
		name         string
		kind         controller.CommandKind
		sessionID    mo.Option[session.ID]
		sessionName  mo.Option[string]
		expectedKind controller.ResponseKind
		expect       func(*MockSessionControl)
	}{
		{
			name: "create", kind: controller.CommandCreateSession,
			sessionID: mo.None[session.ID](), sessionName: mo.None[string](),
			expectedKind: controller.ResponseSessionInfo,
			expect: func(control *MockSessionControl) {
				control.EXPECT().Create(gomock.Any()).Return(session.Replacement{Info: info, Entries: nil}, nil)
			},
		},
		{
			name: "list", kind: controller.CommandListSessions,
			sessionID: mo.None[session.ID](), sessionName: mo.None[string](),
			expectedKind: controller.ResponseSessions,
			expect: func(control *MockSessionControl) {
				control.EXPECT().List(gomock.Any()).Return([]session.Summary{{
					Info: info, FirstUserText: mo.None[string](), TotalMessages: 0,
				}}, nil)
			},
		},
		{
			name: "resume", kind: controller.CommandResumeSession,
			sessionID: mo.Some(session.ID("session-id")), sessionName: mo.None[string](),
			expectedKind: controller.ResponseSessionInfo,
			expect: func(control *MockSessionControl) {
				control.EXPECT().Resume(gomock.Any(), session.ID("session-id")).Return(
					session.Replacement{Info: info, Entries: nil}, nil,
				)
			},
		},
		{
			name: "name", kind: controller.CommandSetSessionName,
			sessionID: mo.None[session.ID](), sessionName: mo.Some("named"),
			expectedKind: controller.ResponseSessionInfo,
			expect: func(control *MockSessionControl) {
				control.EXPECT().SetName(gomock.Any(), "named").Return(info, nil)
			},
		},
		{
			name: "information", kind: controller.CommandGetSessionInfo,
			sessionID: mo.None[session.ID](), sessionName: mo.None[string](),
			expectedKind: controller.ResponseSessionInfo,
			expect:       func(control *MockSessionControl) { control.EXPECT().Info().Return(info) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			control := NewMockSessionControl(gomock.NewController(t))
			test.expect(control)
			service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())

			// Act by handling the lifecycle command through Programmatic Control.
			response, operation, err := service.Handle(t.Context(), controller.Command{
				CorrelationID:   test.name,
				Kind:            test.kind,
				UserText:        mo.None[string](),
				ProviderID:      mo.None[model.ProviderID](),
				ModelID:         mo.None[model.ID](),
				ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:       test.sessionID,
				SessionName:     test.sessionName,
				TargetEntryID:   mo.None[string](),
				SummaryMode:     controller.SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
			})

			// Assert handling succeeds without a run operation and returns the expected response kind.
			require.NoError(t, err)
			assert.Nil(t, operation)
			assert.Equal(t, test.expectedKind, response.Kind)
		})
	}
}
