//go:build !integration

package extensionruntime

import (
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestInvocationAdmissionChecksExactRuntimeGeneration verifies stale contexts cannot address a replacement parent.
func TestInvocationAdmissionChecksExactRuntimeGeneration(t *testing.T) {
	t.Parallel()
	// Arrange a replacement runtime with a reused connection-local parent identifier.
	mocks := gomock.NewController(t)
	process := NewMockExtensionRuntime(mocks)
	state := &runtimeState{
		runtime:          process,
		instanceID:       "current",
		available:        true,
		activeExecutions: 0,
		exitPending:      false,
		work:             sync.WaitGroup{},
		closeOnce:        sync.Once{},
		shutdownDone:     make(chan struct{}),
		observed:         false,
		monitorDone: make(
			chan struct{},
		),
		commit:         sync.RWMutex{},
		invalidated:    make(chan struct{}),
		invalidateOnce: sync.Once{},
	}
	service := New(nil, nil, nil)
	service.runtimes["extension"] = state
	source := errors.New("current parent admission cause")
	process.EXPECT().BeginInvocationChild("parent").Return(nil, source)
	// Act through stale runtime, exact runtime and invalidated runtime admission.
	_, stale := service.BeginInvocationChild("extension", "previous", "parent")
	_, current := service.BeginInvocationChild("extension", "current", "parent")
	state.invalidate()
	_, invalidated := service.BeginInvocationChild("extension", "current", "parent")
	// Assert only the exact live generation reaches its connection-local owner.
	require.ErrorContains(t, stale, "stale or unavailable")
	require.ErrorIs(t, current, source)
	require.ErrorContains(t, invalidated, "stale or unavailable")
}
