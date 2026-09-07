//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestServerRetainsWorkSourceBeforeTerminal exercises both Failed work and ordinary completed HandlerError.
func TestServerRetainsWorkSourceBeforeTerminal(t *testing.T) {
	t.Parallel()
	for _, completed := range []bool{false, true} {
		name := "failed work"
		if completed {
			name = "completed HandlerError"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange a registered handler whose work returns only after Running delivery stops.
				controller := gomock.NewController(t)
				service := NewMockService(controller)
				handler := NewMockHandleOperation(controller)
				source := errors.New("complete handler source after delivery cancellation")
				cleanupErr := errors.New("independent accepted-work cleanup source")
				deliveryErr := errors.New("Running transport failed")
				started := make(chan struct{})
				service.EXPECT().PrepareHandle(gomock.Any(), gomock.Any()).Return(handler, nil)
				handler.EXPECT().
					Run(gomock.Any()).
					DoAndReturn(func(ctx context.Context) (*extensionpb.HandleResponse, error) {
						close(started)
						<-ctx.Done()
						if completed {
							response := new(extensionpb.HandleResponse)
							response.SetError(extensionpb.HandlerError_builder{Message: new(source.Error())}.Build())
							return response, nil
						}
						return nil, fmt.Errorf("outer accepted-work source: %w", errors.Join(
							Fail(failureCodeInternal, source), cleanupErr,
						))
					})
				handler.EXPECT().Release()
				server := newServer(service)
				server.completeRegistration(extensionpb.RegisterResponse_builder{
					Tools: nil,
					Handlers: []*extensionpb.HandlerDescriptor{extensionpb.HandlerDescriptor_builder{
						Id: new("handler"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE),
					}.Build()},
				}.Build())
				request := new(extensionpb.OpenRequest)
				request.SetOperationId("handler-operation")
				payload := new(extensionpb.HostRequest)
				payload.SetHandle(extensionpb.HandleRequest_builder{
					Context: testInvocationIdentity(), HandlerId: new("handler"),
					SessionBeforeTreeRequest: nil, SessionBeforeTreeResult: nil, Lifecycle: nil,
					SessionTree: extensionpb.SessionTreeInvocation_builder{
						SessionId: new("session"), TargetEntryId: new("target"), PrecedingActiveLeafId: nil,
						NavigationDestinationId: nil, CommittedActiveLeafId: nil, CreatedSummary: nil,
					}.Build(),
				}.Build())
				request.SetRequest(payload)
				stream := NewMockExtensionService_OpenServer[extensionpb.OpenRequest, extensionpb.OpenResponse](
					controller,
				)
				stream.EXPECT().Context().Return(t.Context())
				stopReceive := make(chan struct{})
				gomock.InOrder(
					stream.EXPECT().Recv().Return(request, nil),
					stream.EXPECT().Recv().DoAndReturn(func() (*extensionpb.OpenRequest, error) {
						<-stopReceive
						return nil, context.Canceled
					}),
				)
				stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *extensionpb.OpenResponse) error {
					if response.GetEvent().GetRunning() != nil {
						<-started
						return deliveryErr
					}
					return nil
				}).Times(2)

				// Act through the actual SDK connection, prepared producer and operation owner.
				err := server.Open(stream)
				close(stopReceive)
				synctest.Wait()

				// Assert a skipped Terminal cannot discard either typed source kind.
				require.ErrorIs(t, err, deliveryErr)
				require.ErrorContains(t, err, source.Error())
				if !completed {
					require.ErrorIs(t, err, source)
					require.ErrorIs(t, err, cleanupErr)
					require.ErrorContains(t, err, "outer accepted-work source")
				}
			})
		})
	}
}
