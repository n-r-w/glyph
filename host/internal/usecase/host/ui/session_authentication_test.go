//go:build !integration

package ui

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestAuthenticationCheckRequiresExplicitRetry verifies startup does not create an uncorrelated sign-in operation.
func TestAuthenticationCheckRequiresExplicitRetry(t *testing.T) {
	t.Parallel()

	// Arrange one failed check that requires sign-in.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	authenticator := NewMockAuthenticator(controller)
	source := errors.New("sign-in required")
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(source)
	authenticator.EXPECT().IsSignInRequired(source).Return(true)
	frames := make([]domainui.Frame, 0, 2)
	channel.EXPECT().Send(gomock.Any()).Times(2).DoAndReturn(func(frame domainui.Frame) error {
		frames = append(frames, frame)
		return nil
	})
	service := NewSession(
		channel, NewMockAgentRunner(controller), authenticator, NewMockModelCatalog(controller), nil,
		func(context.Context) {},
	)

	// Act through startup authentication classification.
	service.checkOperationAuthentication(t.Context())

	// Assert the authentication category and failed availability.
	assert.Equal(t, domainui.AvailabilityAuthenticationFailed, service.operationAvailabilitySnapshot())
	require.Len(t, frames, 2)
	assert.Equal(t, mo.Some(failureCodeAuthentication), frames[0].ErrorCode)
}

// TestAuthenticationCheckUsesInternalCategoryForOtherFailures verifies explicit source classification.
func TestAuthenticationCheckUsesInternalCategoryForOtherFailures(t *testing.T) {
	t.Parallel()

	// Arrange a failed check that does not require sign-in.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	authenticator := NewMockAuthenticator(controller)
	source := errors.New("credential store failed")
	authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(source)
	authenticator.EXPECT().IsSignInRequired(source).Return(false)
	frames := make([]domainui.Frame, 0, 2)
	channel.EXPECT().Send(gomock.Any()).Times(2).DoAndReturn(func(frame domainui.Frame) error {
		frames = append(frames, frame)
		return nil
	})
	service := NewSession(
		channel, NewMockAgentRunner(controller), authenticator, NewMockModelCatalog(controller), nil,
		func(context.Context) {},
	)

	// Act through startup authentication classification.
	service.checkOperationAuthentication(t.Context())

	// Assert the source selects INTERNAL without a retry flag.
	require.Len(t, frames, 2)
	assert.Equal(t, mo.Some(failureCodeInternal), frames[0].ErrorCode)
}

// TestAuthenticationRetryRequiresFailedAvailability verifies bounded retry admission.
func TestAuthenticationRetryRequiresFailedAvailability(t *testing.T) {
	t.Parallel()
	// Arrange controller and service for service.Prepare to verify bounded retry admission.

	controller := gomock.NewController(t)
	service := authenticationService(controller)
	service.setOperationAvailability(domainui.AvailabilityIdle)

	// Act by invoking service.Prepare to exercise bounded retry admission.
	_, err := service.Prepare(t.Context(), newCommandForPreparedTest(domainui.CommandRetryAuthentication))

	var rejection *PreparationError
	// Assert bounded retry admission.
	require.ErrorAs(t, err, &rejection)
	assert.Equal(t, rejectionCodeNotReady, rejection.Code())
}

// TestAuthenticationRetryTransitionsToIdle verifies successful retry lifecycle and availability.
func TestAuthenticationRetryTransitionsToIdle(t *testing.T) {
	t.Parallel()
	// Arrange controller, channel, and authenticator for Prepared.Run to verify successful retry lifecycle and availability.

	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	authenticator := NewMockAuthenticator(controller)
	channel.EXPECT().BindProgress(gomock.Any()).Return(func() {})
	channel.EXPECT().Send(gomock.Any()).Times(2).Return(nil)
	authenticator.EXPECT().SignIn(gomock.Any()).Return(nil)
	service := NewSession(
		channel, NewMockAgentRunner(controller), authenticator, NewMockModelCatalog(controller), nil,
		func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityAuthenticationFailed)
	command := newCommandForPreparedTest(domainui.CommandRetryAuthentication)
	command.OperationID = "operation"
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)

	// Act by invoking Prepared.Run to exercise successful retry lifecycle and availability.
	outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
	prepared.Release()

	// Assert successful retry lifecycle and availability.
	assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
	assert.Equal(t, domainui.AvailabilityIdle, service.operationAvailabilitySnapshot())
}

// TestAuthenticationRetryFailurePreservesCause verifies retry failures remain classified and visible.
func TestAuthenticationRetryFailurePreservesCause(t *testing.T) {
	t.Parallel()
	// Arrange controller, channel, and authenticator for Prepared.Run to verify retry failures remain classified and visible.

	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	authenticator := NewMockAuthenticator(controller)
	source := errors.New("browser authentication failed")
	channel.EXPECT().BindProgress(gomock.Any()).Return(func() {})
	channel.EXPECT().Send(gomock.Any()).Times(2).Return(nil)
	authenticator.EXPECT().SignIn(gomock.Any()).Return(source)
	service := NewSession(
		channel, NewMockAgentRunner(controller), authenticator, NewMockModelCatalog(controller), nil,
		func(context.Context) {},
	)
	service.setOperationAvailability(domainui.AvailabilityAuthenticationFailed)
	command := newCommandForPreparedTest(domainui.CommandRetryAuthentication)
	command.OperationID = "operation"
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)

	// Act by invoking Prepared.Run to exercise retry failures remain classified and visible.
	outcome := prepared.Run(t.Context(), operation.Reporter[domainui.Frame]{})
	prepared.Release()

	// Assert retry failures remain classified and visible.
	assert.Equal(t, operation.TerminalStateFailed, outcome.State())
	assert.Equal(t, failureCodeAuthentication, outcome.Code())
	assert.ErrorIs(t, outcome.Err(), source)
	assert.Equal(t, domainui.AvailabilityAuthenticationFailed, service.operationAvailabilitySnapshot())
}

// authenticationService creates one session service for admission-only authentication tests.
func authenticationService(controller *gomock.Controller) *Session {
	return NewSession(
		NewMockChannel(controller), NewMockAgentRunner(controller), NewMockAuthenticator(controller),
		NewMockModelCatalog(controller), nil, func(context.Context) {},
	)
}
