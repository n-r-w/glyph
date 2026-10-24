//go:build !integration

package sessiontree

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestBindModelsValidatesBeforeChangingBindings verifies incomplete and repeated bindings return errors.
func TestBindModelsValidatesBeforeChangingBindings(t *testing.T) {
	t.Parallel()
	// Arrange unbound navigation dependencies.
	controller := gomock.NewController(t)
	selection := NewMockModelSelection(controller)
	requester := NewMockModelRequester(controller)
	service := New(nil, nil, nil, nil)
	// Act with each incomplete binding before a valid binding.
	require.Error(t, service.BindModels(nil, requester))
	require.Error(t, service.BindModels(selection, nil))
	require.Error(t, service.BindModels(nil, nil))
	require.NoError(t, service.BindModels(selection, requester))
	// Assert repeated binding returns an initialization error.
	require.Error(t, service.BindModels(NewMockModelSelection(controller), NewMockModelRequester(controller)))
}
