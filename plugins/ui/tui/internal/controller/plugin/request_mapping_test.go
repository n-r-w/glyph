package plugin

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestMapRequestRequiresTextFrameScalarPresence verifies selected Host frame scalars cannot be omitted.
func TestMapRequestRequiresTextFrameScalarPresence(t *testing.T) {
	t.Parallel()

	tests := map[string]*uiv1.OpenRequest{
		"authorization URL": uiv1.OpenRequest_builder{
			Initialization:        nil,
			Lifecycle:             nil,
			Authorization:         uiv1.AuthorizationRequest_builder{Url: nil}.Build(),
			Information:           nil,
			Error:                 nil,
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
		"information text": uiv1.OpenRequest_builder{
			Initialization:        nil,
			Lifecycle:             nil,
			Authorization:         nil,
			Information:           uiv1.Information_builder{Text: nil}.Build(),
			Error:                 nil,
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
		"error text": uiv1.OpenRequest_builder{
			Initialization: nil,
			Lifecycle:      nil,
			Authorization:  nil,
			Information:    nil,
			Error: uiv1.Error_builder{
				Text:                nil,
				RetryAuthentication: new(false),
			}.Build(),
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
		"error retry authentication": uiv1.OpenRequest_builder{
			Initialization: nil,
			Lifecycle:      nil,
			Authorization:  nil,
			Information:    nil,
			Error: uiv1.Error_builder{
				Text:                new("error"),
				RetryAuthentication: nil,
			}.Build(),
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
	}

	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := mapRequest(request)
			require.Error(t, err)
		})
	}
}

// TestMapRequestPreservesPresentFalseRetryAuthentication verifies false is not treated as absence.
func TestMapRequestPreservesPresentFalseRetryAuthentication(t *testing.T) {
	t.Parallel()

	event, err := mapRequest(uiv1.OpenRequest_builder{
		Initialization: nil,
		Lifecycle:      nil,
		Authorization:  nil,
		Information:    nil,
		Error: uiv1.Error_builder{
			Text:                new(""),
			RetryAuthentication: new(false),
		}.Build(),
		ModelSelectionChanged: nil,
		SessionList:           nil,
		SessionChanged:        nil,
		SessionInformation:    nil,
	}.Build())
	require.NoError(t, err)
	assert.Equal(t, mo.Some(""), event.Text)
	assert.True(t, event.Availability.IsNone())
}

// TestMapRequestPreservesPresentEmptyText verifies empty text stays active for text frames.
func TestMapRequestPreservesPresentEmptyText(t *testing.T) {
	t.Parallel()

	for name, request := range map[string]*uiv1.OpenRequest{
		"authorization": uiv1.OpenRequest_builder{
			Initialization:        nil,
			Lifecycle:             nil,
			Authorization:         uiv1.AuthorizationRequest_builder{Url: new("")}.Build(),
			Information:           nil,
			Error:                 nil,
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
		"information": uiv1.OpenRequest_builder{
			Initialization:        nil,
			Lifecycle:             nil,
			Authorization:         nil,
			Information:           uiv1.Information_builder{Text: new("")}.Build(),
			Error:                 nil,
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			event, err := mapRequest(request)
			require.NoError(t, err)
			assert.Equal(t, mo.Some(""), event.Text)
		})
	}
}

// TestMapCommandRejectsMissingSelectedPayload verifies malformed presentation commands do not emit zero payloads.
func TestMapCommandRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	response, err := mapCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSubmit,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	})

	require.Error(t, err)
	assert.Nil(t, response)

	response, err = mapCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSubmit,
		Text:            mo.Some(""),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	})
	require.NoError(t, err)
	assert.True(t, response.GetSubmit().HasText())
	assert.Empty(t, response.GetSubmit().GetText())
}
