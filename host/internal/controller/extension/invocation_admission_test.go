//go:build !integration

package extension

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestInvocationAdmissionRejectionReleasesRuntime checks that a stale parent cannot detach an accepted runtime reservation.
func TestInvocationAdmissionRejectionReleasesRuntime(t *testing.T) {
	t.Parallel()
	// Arrange a valid session reference and a rejected parent after runtime reservation.
	mocks := gomock.NewController(t)
	contexts := NewMockContextOperations(mocks)
	runtime := NewMockRuntimeOperations(mocks)
	reference := extensiondomain.ContextRef{ID: "binding", RuntimeInstanceID: "runtime", SessionID: "session"}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
	released := 0
	runtime.EXPECT().BeginContextOperation(gomock.Any(), "extension", "runtime").Return(func() { released++ }, nil)
	cause := errors.New("parent invocation has ended: complete retained cause")
	runtime.EXPECT().BeginInvocationChild("extension", "runtime", "stale").Return(nil, cause)
	service := New(nil, contexts, runtime, "extension", "runtime")
	request := new(extensionpb.ExtensionRequest)
	request.SetParentInvocationId("stale")
	request.SetGetModels(extensionpb.GetModelsRequest_builder{Context: extensionpb.ExtensionContextRef_builder{
		ContextId: new(
			reference.ID,
		),
		RuntimeInstanceId: new(reference.RuntimeInstanceID),
		SessionId:         new(reference.SessionID),
	}.Build()}.Build())
	// Act through parent validation before action preparation or execution.
	prepared, err := service.Prepare(t.Context(), "child", request)
	// Assert no child operation escapes admission and the runtime reservation is released exactly once.
	require.Nil(t, prepared)
	require.ErrorIs(t, err, cause)
	require.Equal(t, 1, released)
}
