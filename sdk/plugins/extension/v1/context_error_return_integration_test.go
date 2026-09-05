//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestReturnedContextFailureDoesNotInvalidateExtension verifies nested failure categories do not become invalid outer
// codes.
func TestReturnedContextFailureDoesNotInvalidateExtension(t *testing.T) {
	t.Parallel()

	// Arrange: let a tool return its accepted catalog failure through the normal SDK error path.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	first := NewMockExecuteOperation(controller)
	second := NewMockExecuteOperation(controller)
	host := NewMockHostService(controller)
	read := NewMockHostOperation(controller)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(contractRegistration(), nil)
	registration.EXPECT().Release()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(first, nil)
	first.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
			binding, err := ContextFrom(ctx)
			if err != nil {
				return nil, err
			}
			started, err := binding.StartGetModels(ctx)
			if err != nil {
				return nil, err
			}
			_, err = started.Wait(ctx)
			var failure *FailureError
			if assert.ErrorAs(t, err, &failure) {
				assert.Equal(t, "STALE_CONTEXT", failure.Code())
			}
			return nil, err
		})
	first.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(read, nil)
	read.EXPECT().
		Run(gomock.Any()).
		Return(nil, Fail("STALE_CONTEXT", errors.New("session incarnation changed during catalog read")))
	read.EXPECT().Release()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(second, nil)
	second.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		Return(extensionpb.ToolResult_builder{Contents: nil, IsError: new(false)}.Build(), nil)
	second.EXPECT().Release()
	connection := openContextTestConnection(t, service, host)
	register := new(extensionpb.HostRequest)
	register.SetRegister(new(extensionpb.RegisterRequest))
	registered, err := connection.Start(t.Context(), "register", register)
	require.NoError(t, err)
	_, err = registered.Wait(t.Context(), nil)
	require.NoError(t, err)
	request := new(extensionpb.HostRequest)
	request.SetExecute(
		extensionpb.ExecuteRequest_builder{
			ToolName:      new("contract"),
			ArgumentsJson: []byte(`{}`),
			Context:       testInvocationIdentity(),
		}.Build(),
	)

	// Act: return the nested failure from an accepted tool operation.
	invoked, err := connection.Start(t.Context(), "first", request)
	require.NoError(t, err)
	_, err = invoked.Wait(t.Context(), nil)

	// Assert: the outer operation keeps its closed category and complete nested cause without losing the connection.
	var failure *FailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, "INTERNAL", failure.Code())
	require.ErrorContains(t, err, "STALE_CONTEXT")
	require.ErrorContains(t, err, "session incarnation changed during catalog read")
	require.NoError(t, connection.connectionError())
	invoked, err = connection.Start(t.Context(), "second", request)
	require.NoError(t, err)
	_, err = invoked.Wait(t.Context(), nil)
	require.NoError(t, err)
}
