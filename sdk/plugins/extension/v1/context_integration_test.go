//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestNestedCatalogueReadsKeepBothReceiveLoopsLive verifies contextual tool work can await both catalog kinds.
func TestNestedCatalogueReadsKeepBothReceiveLoopsLive(t *testing.T) {
	t.Parallel()

	// Arrange: connect both production SDK peers over real gRPC with isolated service implementations.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	execution := NewMockExecuteOperation(controller)
	host := NewMockHostService(controller)
	models := NewMockHostOperation(controller)
	providers := NewMockHostOperation(controller)
	configured := NewMockHostOperation(controller)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(contractRegistration(), nil)
	registration.EXPECT().Release()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execution, nil)
	execution.EXPECT().Release()
	identity := extensionpb.ExtensionContext_builder{
		ContextId: new("binding"), ExtensionId: new("extension"), RuntimeInstanceId: new("runtime"),
		SessionId: new("session"), Cwd: new("/project"),
	}.Build()
	execution.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
			bound, err := ContextFrom(ctx)
			if err != nil {
				return nil, err
			}
			assert.Equal(t, identity.GetContextId(), bound.Identity().GetContextId())
			modelRead, err := bound.StartGetModels(ctx)
			if err != nil {
				return nil, err
			}
			modelResult, err := modelRead.Wait(ctx)
			if err != nil {
				return nil, err
			}
			assert.Equal(t, "model", modelResult.GetActiveSelection().GetModelId())
			providerRead, err := bound.StartGetProviders(ctx)
			if err != nil {
				return nil, err
			}
			providerResult, err := providerRead.Wait(ctx)
			if err != nil {
				return nil, err
			}
			assert.Equal(t, []string{"model"}, providerResult.GetProviders()[0].GetModelIds())
			request := extensionpb.ConfiguredModelRequest_builder{
				Context: nil,
				Selection: extensionpb.ModelSelection_builder{
					ProviderId: new("provider"), ModelId: new("model"), ReasoningChoice: new("off"),
				}.Build(),
				Instructions: new(""),
				Messages: []*extensionpb.ConfiguredModelMessage{
					extensionpb.ConfiguredModelMessage_builder{
						Role: new(extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER), Text: new("question"),
					}.Build(),
				},
			}.Build()
			modelRequest, err := bound.StartConfiguredModel(ctx, request)
			if err != nil {
				return nil, err
			}
			configuredResult, err := modelRequest.Wait(ctx)
			if err != nil {
				return nil, err
			}
			assert.Equal(t, "answer", configuredResult.GetContent()[0].GetText().GetText())
			return extensionpb.ToolResult_builder{Contents: nil, IsError: new(false)}.Build(), nil
		})
	host.EXPECT().
		Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "binding", request.GetGetModels().GetContext().GetContextId())
			return models, nil
		})
	models.EXPECT().Run(gomock.Any()).Return(extensionpb.HostCompleted_builder{
		Cancel:          nil,
		GetProviders:    nil,
		ConfiguredModel: nil,

		GetModels: extensionpb.GetModelsResult_builder{Models: nil, ActiveSelection: extensionpb.ModelSelection_builder{
			ProviderId: new("provider"), ModelId: new("model"), ReasoningChoice: new("off"),
		}.Build()}.Build(),
	}.Build(), nil)
	models.EXPECT().Release()
	host.EXPECT().
		Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "binding", request.GetGetProviders().GetContext().GetContextId())
			return providers, nil
		})
	providers.EXPECT().Run(gomock.Any()).Return(extensionpb.HostCompleted_builder{
		Cancel:          nil,
		GetModels:       nil,
		ConfiguredModel: nil,

		GetProviders: extensionpb.GetProvidersResult_builder{Providers: []*extensionpb.ProviderDescriptor{
			extensionpb.ProviderDescriptor_builder{ProviderId: new("provider"), ModelIds: []string{"model"}}.Build(),
		}}.Build(),
	}.Build(), nil)
	providers.EXPECT().Release()
	host.EXPECT().
		Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "binding", request.GetConfiguredModel().GetContext().GetContextId())
			assert.Equal(t, "question", request.GetConfiguredModel().GetMessages()[0].GetText())
			return configured, nil
		})
	configured.EXPECT().Run(gomock.Any()).Return(extensionpb.HostCompleted_builder{
		Cancel: nil, GetModels: nil, GetProviders: nil,
		ConfiguredModel: extensionpb.ConfiguredModelResult_builder{
			Outcome: nil, ErrorMessage: nil, ProviderId: nil, ModelId: nil,
			ResponseModelId: nil, ResponseId: nil, Usage: nil, Diagnostics: nil,
			Content: []*extensionpb.ConfiguredModelContent{func() *extensionpb.ConfiguredModelContent {
				content := new(extensionpb.ConfiguredModelContent)
				content.SetText(extensionpb.ConfiguredModelText_builder{Text: new("answer")}.Build())
				return content
			}()},
		}.Build(),
	}.Build(), nil)
	configured.EXPECT().Release()
	connection := openContextTestConnection(t, service, host)
	register, err := connection.Start(t.Context(), "register", extensionpb.HostRequest_builder{
		Cancel:   nil,
		Execute:  nil,
		Handle:   nil,
		Register: new(extensionpb.RegisterRequest),
	}.Build())
	require.NoError(t, err)
	_, err = register.Wait(t.Context(), nil)
	require.NoError(t, err)

	// Act: use the first extension-originated identifier for the opposite Host initiator as well.
	started, err := connection.Start(t.Context(), "extension-host-1", extensionpb.HostRequest_builder{
		Cancel:   nil,
		Handle:   nil,
		Register: nil,

		Execute: extensionpb.ExecuteRequest_builder{
			ToolName:      new("contract"),
			ArgumentsJson: []byte(`{}`),
			Context:       identity,
		}.Build(),
	}.Build())
	require.NoError(t, err)
	_, err = started.Wait(t.Context(), nil)

	// Assert: nested reads finish while their invoking tool operation remains active.
	require.NoError(t, err)
	require.NoError(t, connection.Close())
}

// TestHostPeerProtocolErrorsRetainCompleteCause verifies malformed lifecycles retain unbounded Host-owned text.
func TestHostPeerProtocolErrorsRetainCompleteCause(t *testing.T) {
	t.Parallel()
	for _, known := range []bool{false, true} {
		t.Run(fmt.Sprintf("known_%t", known), func(t *testing.T) {
			t.Parallel()
			// Arrange: provide a Host failure before acceptance or for an unknown identifier.
			initiator := newContextInitiator(t.Context(), nil, func(error) {})
			t.Cleanup(func() { initiator.close(context.Canceled) })
			if known {
				events, err := initiator.tracker.Track("operation")
				require.NoError(t, err)
				initiator.pending["operation"] = &contextOperation{
					initiator: initiator,
					id:        "operation",
					kind:      hostRequestModels,
					events:    events,
					done:      make(chan struct{}),
				}
			}
			text := strings.Repeat("Ω", 40000) + " complete Host cause"
			payload := new(extensionpb.HostEvent)
			payload.SetFailed(operationpb.Failed_builder{Code: new("INTERNAL"), Message: new(text)}.Build())

			// Act: reject the illegal lifecycle at the initiating peer.
			err := initiator.handle("operation", payload)

			// Assert: protocol context supplements the complete category and original Host cause.
			require.Error(t, err)
			require.ErrorContains(t, err, "INTERNAL")
			assert.Contains(t, err.Error(), text, "complete Host-owned cause was not preserved")
		})
	}
}

// TestClosedInvocationContextRejectsNewOperations verifies a retained context never starts on a replacement stream.
func TestClosedInvocationContextRejectsNewOperations(t *testing.T) {
	t.Parallel()

	// Arrange: retain an invocation context after its owning stream is closed.
	initiator := newContextInitiator(t.Context(), nil, func(error) {})
	binding, err := invocationContext(testInvocationIdentity(), initiator)
	require.NoError(t, err)
	cause := errors.New("runtime instance was replaced")
	initiator.close(cause)

	// Act: attempt another operation through the preceding invocation binding.
	_, err = binding.StartGetModels(t.Context())

	// Assert: local rejection exposes stale identity and the complete stream cause without sending.
	var failure *FailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, "STALE_CONTEXT", failure.Code())
	require.ErrorIs(t, err, cause)
}

// TestInvalidHostLifecycleKeepsProtocolStatus verifies symmetric routing classifies peer lifecycle violations.
func TestInvalidHostLifecycleKeepsProtocolStatus(t *testing.T) {
	t.Parallel()

	// Arrange: send a Host error for an identifier that this extension did not initiate.
	service := NewMockService(gomock.NewController(t))
	client := newContextTestClient(t, service)
	connection, err := client.Open(t.Context())
	require.NoError(t, err)
	payload := new(extensionpb.HostEvent)
	payload.SetFailed(operationpb.Failed_builder{Code: new("INTERNAL"), Message: new("complete peer cause")}.Build())
	require.NoError(t, connection.writer.Enqueue(hostEventEnvelope("unknown", payload)))

	// Act: join the stream after its server detects the invalid lifecycle.
	err = connection.Close()

	// Assert: both the protocol category and the complete incoming cause survive the round trip.
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.ErrorContains(t, err, "complete peer cause")
	require.ErrorContains(t, err, "INTERNAL")
}

// openContextTestConnection connects the two production SDK peers without starting an external process.
func openContextTestConnection(t *testing.T, service Service, host HostService) *Connection {
	t.Helper()
	client := newContextTestClient(t, service)
	connection, err := client.Open(t.Context())
	require.NoError(t, err)
	connection.BindHostService(host)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) })
	return connection
}

// newContextTestClient connects an SDK client to a real server and leaves stream cleanup to its caller.
func newContextTestClient(t *testing.T, service Service) *Client {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	extensionpb.RegisterExtensionServiceServer(server, newServer(service))
	stopped := make(chan error, 1)
	go func() { stopped <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-stopped })
	transport, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, transport.Close()) })
	return &Client{
		process:   nil,
		service:   extensionpb.NewExtensionServiceClient(transport),
		done:      nil,
		version:   ProtocolVersion,
		closeOnce: sync.Once{},
	}
}
