//go:build !integration

package plugin

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestOperationMappersRequireSelectedPayloadPresence verifies required retained scalar presence.
func TestOperationMappersRequireSelectedPayloadPresence(t *testing.T) {
	t.Parallel()
	// Arrange authorization for the operation payload mappers to verify required retained scalar presence.

	authorization := new(uiv1.HostProgress)
	authorization.SetAuthorization(new(uiv1.AuthorizationRequest))
	// Act by invoking the operation payload mappers to exercise required retained scalar presence.
	_, authorizationErr := mapHostProgress(authorization)
	// Assert required retained scalar presence.
	require.Error(t, authorizationErr)

	connection := new(uiv1.HostConnectionEvent)
	connection.SetInformation(new(uiv1.Information))
	_, informationErr := mapConnectionEvent(connection)
	require.Error(t, informationErr)

	connectionError := new(uiv1.HostConnectionEvent)
	connectionError.SetError(new(uiv1.Error))
	_, errorErr := mapConnectionEvent(connectionError)
	require.Error(t, errorErr)
}

// TestOperationMappersPreservePresentEmptyText verifies empty present text remains distinct from absence.
func TestOperationMappersPreservePresentEmptyText(t *testing.T) {
	t.Parallel()
	// Arrange information for mapConnectionEvent to verify empty present text remains distinct from absence.

	information := new(uiv1.HostConnectionEvent)
	information.SetInformation(uiv1.Information_builder{Text: new("")}.Build())
	// Act by invoking mapConnectionEvent to exercise empty present text remains distinct from absence.
	informationEvent, err := mapConnectionEvent(information)
	// Assert empty present text remains distinct from absence.
	require.NoError(t, err)
	assert.Equal(t, mo.Some(""), informationEvent.Text)

	connectionError := new(uiv1.HostConnectionEvent)
	connectionError.SetError(uiv1.Error_builder{Code: new("INTERNAL"), Text: new("")}.Build())
	_, err = mapConnectionEvent(connectionError)
	require.EqualError(t, err, "connection error category and text are required")
}

// TestMapCommandRejectsMissingSelectedPayload verifies command option ownership remains strict.
func TestMapCommandRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()
	// Arrange tests for mapCommand to verify command option ownership remains strict.

	tests := []presentationdomain.Command{
		commandFixture(presentationdomain.CommandSubmit, mo.None[string]()),
		commandFixture(presentationdomain.CommandResumeSession, mo.None[string]()),
		commandFixture(presentationdomain.CommandSetSessionName, mo.None[string]()),
	}
	for _, command := range tests {
		// Act by invoking mapCommand to exercise command option ownership remains strict.
		_, err := mapCommand(command)
		// Assert command option ownership remains strict.
		require.Error(t, err)
	}
}
