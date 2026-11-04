//go:build !integration

package tools

import (
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestToolConcurrencyAcceptance verifies validation and commit retain concurrency in the execution catalog.
func TestToolConcurrencyAcceptance(t *testing.T) {
	t.Parallel()
	// Arrange concurrent and serial registrations.
	runtime := NewMockRuntime(gomock.NewController(t))
	runtime.EXPECT().ToolRuntimeAvailable(gomock.Any()).Return(true).AnyTimes()
	service := New(runtime)
	read := rawDescriptor("read", "Read.")
	read.Concurrent = true
	pending := startup.PendingRegistration{ID: "extension", Path: "/extension", Tools: []startup.RawToolDescriptor{
		read, rawDescriptor("write", "Write."),
	}, Handlers: nil}
	// Act through startup validation and commit.
	descriptors, err := service.ValidateLocal(pending)
	require.NoError(t, err)
	service.Commit(
		[]startup.AcceptedRegistration{{ID: pending.ID, Path: pending.Path, Tools: descriptors, Handlers: nil}},
	)
	// Assert the accepted catalog retains both declarations.
	registered := service.Tools()
	require.Len(t, registered, 2)
	assert.True(t, registered[0].Concurrent)
	assert.False(t, registered[1].Concurrent)
}
