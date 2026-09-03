//go:build !integration

package sessiontree

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestServiceValidatesHandlers verifies missing, empty, unknown, and duplicate registrations retain existing errors.
func TestServiceValidatesHandlers(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		handlers  []startup.RawHandlerDescriptor
		errorText string
	}{
		"missing": {
			handlers: []startup.RawHandlerDescriptor{
				{Present: false, ID: "observer", Kind: startup.RawHandlerKindSessionTree},
			},
			errorText: "handler ID is empty",
		},
		"empty": {
			handlers:  []startup.RawHandlerDescriptor{{Present: true, ID: "", Kind: startup.RawHandlerKindSessionTree}},
			errorText: "handler ID is empty",
		},
		"unknown": {
			handlers: []startup.RawHandlerDescriptor{
				{Present: true, ID: "observer", Kind: startup.RawHandlerKind(99)},
			},
			errorText: `handler "observer" has unknown kind 99`,
		},
		"duplicate": {
			handlers: []startup.RawHandlerDescriptor{
				{Present: true, ID: "same", Kind: startup.RawHandlerKindSessionTree},
				{Present: true, ID: "same", Kind: startup.RawHandlerKindSessionBeforeTreeRequest},
			},
			errorText: `handler ID "same" is duplicated`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one raw handler registration.
			service := New(nil, nil, nil)
			// Act by validating the handler registration.
			_, err := service.ValidateHandlers(startup.PendingRegistration{
				ID: "extension", Path: "/extension", Tools: nil, Handlers: test.handlers,
			})
			// Assert the existing policy error remains exact.
			require.EqualError(t, err, test.errorText)
		})
	}
}

// TestServiceKeepsAcceptedHandlerOrder verifies registration order remains deterministic by handler kind.
func TestServiceKeepsAcceptedHandlerOrder(t *testing.T) {
	t.Parallel()

	// Arrange accepted handlers from two extensions with an observer between request handlers.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	service := New(nil, nil, runtime)
	service.CommitHandlers([]startup.AcceptedRegistration{
		{
			ID: "first", Path: "/first", Tools: nil,
			Handlers: []startup.AcceptedHandler{
				{ID: "request-a", Kind: startup.RawHandlerKindSessionBeforeTreeRequest},
				{ID: "observer", Kind: startup.RawHandlerKindSessionTree},
			},
		},
		{
			ID: "second", Path: "/second", Tools: nil,
			Handlers: []startup.AcceptedHandler{
				{ID: "request-b", Kind: startup.RawHandlerKindSessionBeforeTreeRequest},
			},
		},
	})
	runtime.EXPECT().HandlerRuntimeAvailable("first").Return(true)
	runtime.EXPECT().HandlerRuntimeAvailable("second").Return(true)

	// Act by taking one available request-handler snapshot.
	handlers := service.handlersFor(HandlerKindRequest)

	// Assert the snapshot keeps extension and registration order.
	assert.Equal(t, []Handler{
		{ExtensionID: "first", HandlerID: "request-a"},
		{ExtensionID: "second", HandlerID: "request-b"},
	}, handlers)
}
