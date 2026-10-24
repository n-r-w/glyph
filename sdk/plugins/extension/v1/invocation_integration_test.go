//go:build integration

package extensionv1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestInvocationContextOwnsDifferentCallerContext verifies concurrent invocations preserve their envelope parents.
func TestInvocationContextOwnsDifferentCallerContext(t *testing.T) {
	t.Parallel()
	// Arrange two SDK invocations sharing one session identity and one bidirectional connection.
	mocks := gomock.NewController(t)
	service := NewMockService(mocks)
	registration := NewMockRegisterOperation(mocks)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(contractRegistration(), nil)
	registration.EXPECT().Release()
	host := NewMockHostService(mocks)
	for _, id := range []string{"first", "second"} {
		execution := NewMockExecuteOperation(mocks)
		service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execution, nil)
		execution.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
				binding, err := ContextFrom(ctx)
				if err != nil {
					return nil, err
				}
				// The caller context has neither invocation values nor the invocation's cancellation.
				child, startErr := binding.StartGetModels(t.Context())
				if startErr != nil {
					return nil, startErr
				}
				_, waitErr := child.Wait(t.Context())
				return contractToolResult(), waitErr
			},
		)
		execution.EXPECT().Release()
		host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Cond(func(request *extensionpb.ExtensionRequest) bool {
			return request.GetParentInvocationId() == id
		})).DoAndReturn(func(context.Context, string, *extensionpb.ExtensionRequest) (HostOperation, error) {
			child := NewMockHostOperation(mocks)
			child.EXPECT().Run(gomock.Any(), gomock.Any()).Return(extensionpb.HostCompleted_builder{
				GetModels: new(extensionpb.GetModelsResult), GetProviders: nil, ConfiguredModel: nil, Cancel: nil,
				AppendExtension: nil, GetSessionState: nil, AppendExtensionMessage: nil, Selection: nil, Compact: nil,
				ProjectCompaction: nil, ReplaceCompaction: nil, InvokeCompactionRequest: nil,
				InvokeCompactionGenerator: nil, InvokeCompactionResult: nil,
			}.Build(), nil)
			child.EXPECT().Release()
			return child, nil
		})
	}
	connection := openContextTestConnection(t, service, host)
	registrationOperation, err := connection.Start(t.Context(), "register", extensionpb.HostRequest_builder{
		Register: new(extensionpb.RegisterRequest), Execute: nil, Handle: nil, Cancel: nil,
	}.Build())
	require.NoError(t, err)
	_, err = registrationOperation.Wait(t.Context(), nil)
	require.NoError(t, err)
	// Act by starting both operations before waiting for either parent terminal.
	var invocations []*Operation
	for _, id := range []string{"first", "second"} {
		started, startErr := connection.Start(t.Context(), id, extensionpb.HostRequest_builder{
			Register: nil, Handle: nil, Cancel: nil,
			Execute: extensionpb.ExecuteRequest_builder{
				ToolName: new("contract"), ArgumentsJson: []byte(`{}`), Context: testInvocationIdentity(),
			}.Build(),
		}.Build())
		require.NoError(t, startErr)
		invocations = append(invocations, started)
	}
	// Assert both child requests reach Host with their own parent despite the unrelated caller context.
	for _, invocation := range invocations {
		_, waitErr := invocation.Wait(t.Context(), nil)
		require.NoError(t, waitErr)
	}
	require.NoError(t, connection.Close())
}
