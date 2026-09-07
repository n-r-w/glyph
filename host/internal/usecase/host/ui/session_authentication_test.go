//go:build !integration

package ui

import (
	"errors"
	"testing"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/internal/operation"
)

// TestAuthenticationCheckClassifiesFailure verifies startup reports the cause and requires explicit retry.
func TestAuthenticationCheckClassifiesFailure(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		// name identifies the authentication source condition.
		name string
		// message is the complete source diagnostic.
		message string
		// signInRequired selects the authentication category.
		signInRequired bool
		// code is the expected client failure category.
		code string
	}{
		{
			name: "sign-in required", message: "sign-in required",
			signInRequired: true, code: controllerui.FailureCodeAuthentication,
		},
		{
			name: "other failure", message: "credential store failed",
			signInRequired: false, code: controllerui.FailureCodeInternal,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a failed authentication check with its explicit source classification.
			controller := gomock.NewController(t)
			channel := NewMockOutput(controller)
			authenticator := NewMockAuthenticator(controller)
			source := errors.New(test.message)
			authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(source)
			authenticator.EXPECT().IsSignInRequired(source).Return(test.signInRequired)
			gomock.InOrder(
				channel.EXPECT().ReportError(test.code, source.Error()).Return(nil),
				channel.EXPECT().SetAvailability(AvailabilityAuthenticationFailed).Return(nil),
			)
			service := NewSession(
				channel, NewMockAgentRunner(controller), authenticator, NewMockModelCatalog(controller), nil, nil,
				nil, nil,
			)

			// Act through startup authentication classification without starting sign-in.
			service.checkOperationAuthentication(t.Context())

			// Assert the failed availability requires an explicit retry for either cause.
			assert.Equal(t, AvailabilityAuthenticationFailed, service.operationAvailabilitySnapshot())
		})
	}
}

// TestAuthenticationRetryRequiresFailedAvailability verifies bounded retry admission.
func TestAuthenticationRetryRequiresFailedAvailability(t *testing.T) {
	t.Parallel()
	// Arrange controller and service for service.Prepare to verify bounded retry admission.

	controller := gomock.NewController(t)
	service := authenticationService(controller)
	service.setOperationAvailability(AvailabilityIdle)

	// Act by invoking service.Prepare to exercise bounded retry admission.
	_, err := service.Prepare(t.Context(), newCommandForPreparedTest(controllerui.CommandRetryAuthentication))

	var rejection *PreparationError
	// Assert bounded retry admission.
	require.ErrorAs(t, err, &rejection)
	assert.Equal(t, controllerui.RejectionCodeNotReady, rejection.PreparationCode())
}

// TestAuthenticationRetryTransitionsToIdle verifies successful retry lifecycle and availability.
func TestAuthenticationRetryTransitionsToIdle(t *testing.T) {
	t.Parallel()
	// Arrange controller, channel, and authenticator for Prepared.Run to verify successful retry lifecycle and availability.

	controller := gomock.NewController(t)
	channel := NewMockOutput(controller)
	authenticator := NewMockAuthenticator(controller)
	channel.EXPECT().BindProgress(gomock.Any()).Return(func() {})
	channel.EXPECT().SetAvailability(gomock.Any()).Times(2).Return(nil)
	authenticator.EXPECT().SignIn(gomock.Any()).Return(nil)
	service := NewSession(
		channel, NewMockAgentRunner(controller), authenticator, NewMockModelCatalog(controller), nil, nil,
		nil, nil,
	)
	service.setOperationAvailability(AvailabilityAuthenticationFailed)
	command := newCommandForPreparedTest(controllerui.CommandRetryAuthentication)
	command.OperationID = "operation"
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)

	// Act by invoking Prepared.Run to exercise successful retry lifecycle and availability.
	outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
	prepared.Release()

	// Assert successful retry lifecycle and availability.
	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
	assert.Equal(t, AvailabilityIdle, service.operationAvailabilitySnapshot())
}

// TestAuthenticationRetryFailurePreservesCause verifies retry failures remain classified and visible.
func TestAuthenticationRetryFailurePreservesCause(t *testing.T) {
	t.Parallel()
	// Arrange controller, channel, and authenticator for Prepared.Run to verify retry failures remain classified and visible.

	controller := gomock.NewController(t)
	channel := NewMockOutput(controller)
	authenticator := NewMockAuthenticator(controller)
	source := errors.New("browser authentication failed")
	channel.EXPECT().BindProgress(gomock.Any()).Return(func() {})
	channel.EXPECT().SetAvailability(gomock.Any()).Times(2).Return(nil)
	authenticator.EXPECT().SignIn(gomock.Any()).Return(source)
	service := NewSession(
		channel, NewMockAgentRunner(controller), authenticator, NewMockModelCatalog(controller), nil, nil,
		nil, nil,
	)
	service.setOperationAvailability(AvailabilityAuthenticationFailed)
	command := newCommandForPreparedTest(controllerui.CommandRetryAuthentication)
	command.OperationID = "operation"
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)

	// Act by invoking Prepared.Run to exercise retry failures remain classified and visible.
	outcome := prepared.Run(t.Context(), operation.Reporter[controllerui.Frame]{})
	prepared.Release()

	// Assert retry failures remain classified and visible.
	assert.Equal(t, operation.TerminalStateFailed, outcome.State())
	assert.Equal(t, controllerui.FailureCodeAuthentication, outcome.Code())
	assert.ErrorIs(t, outcome.Err(), source)
	assert.Equal(t, AvailabilityAuthenticationFailed, service.operationAvailabilitySnapshot())
}

// authenticationService creates one session service for admission-only authentication tests.
func authenticationService(controller *gomock.Controller) *Session {
	return NewSession(
		NewMockOutput(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), nil, nil, nil, nil,
	)
}
