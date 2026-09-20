//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestCompactionHandlersExchangeSuppliedAndGeneratedResults verifies real gRPC public handler variants.
func TestCompactionHandlersExchangeSuppliedAndGeneratedResults(t *testing.T) {
	t.Parallel()
	// Arrange one extension registration with request and generation capabilities.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	requestOperation := NewMockHandleOperation(controller)
	generationOperation := NewMockHandleOperation(controller)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(extensionpb.RegisterResponse_builder{
		Tools: nil,
		Handlers: []*extensionpb.HandlerDescriptor{
			extensionpb.HandlerDescriptor_builder{
				Id: new("supply"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_REQUEST),
			}.Build(),
			extensionpb.HandlerDescriptor_builder{
				Id: new("generate"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_GENERATE),
			}.Build(),
		},
	}.Build(), nil)
	registration.EXPECT().Release()
	service.EXPECT().PrepareHandle(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request *extensionpb.HandleRequest) (HandleOperation, error) {
			switch request.GetHandlerId() {
			case "supply":
				require.NotNil(t, request.GetCompactionRequest())
				return requestOperation, nil
			case "generate":
				require.NotNil(t, request.GetCompactionGenerate())
				return generationOperation, nil
			default:
				return nil, errors.New("unexpected compaction handler")
			}
		}).Times(2)
	requestOperation.EXPECT().Run(gomock.Any()).Return(compactionHandlerResponse(true), nil)
	requestOperation.EXPECT().Release()
	generationOperation.EXPECT().Run(gomock.Any()).Return(compactionHandlerResponse(false), nil)
	generationOperation.EXPECT().Release()
	connection := openContextTestConnection(t, service, NewMockHostService(controller))
	register, err := connection.Start(t.Context(), "register", extensionpb.HostRequest_builder{
		Cancel: nil, Execute: nil, Handle: nil, Register: new(extensionpb.RegisterRequest),
	}.Build())
	require.NoError(t, err)
	_, err = register.Wait(t.Context(), nil)
	require.NoError(t, err)

	// Act through both Host-initiated public handler variants.
	supplied := invokeCompactionHandler(t, connection, "supply", true)
	generated := invokeCompactionHandler(t, connection, "generate", false)

	// Assert custom results retain exact public summary data.
	require.Equal(t, "supplied", supplied.GetCompactionRequest().GetResult().GetSummary())
	require.Equal(t, "generated", generated.GetCompactionGenerate().GetSummary())
}

// invokeCompactionHandler invokes one registered handler through the real stream.
func invokeCompactionHandler(
	t *testing.T,
	connection *Connection,
	handlerID string,
	requestHandler bool,
) *extensionpb.HandleResponse {
	t.Helper()
	invocation := extensionpb.CompactionRequestInvocation_builder{
		Original: compactionPublicRequest(), Current: compactionPublicRequest(), CurrentResult: nil,
	}.Build()
	handleBuilder := extensionpb.HandleRequest_builder{
		HandlerId: new(handlerID), Context: testInvocationIdentity(),
		SessionBeforeTreeRequest: nil, SessionBeforeTreeResult: nil, SessionTree: nil, Lifecycle: nil,
		ModelSelection: nil, ReasoningSelection: nil, Retry: nil,
		CompactionRequest: nil, CompactionGenerate: nil, CompactionResult: nil,
		CompactionSuccess: nil, CompactionFailure: nil,
	}
	if requestHandler {
		handleBuilder.CompactionRequest = invocation
	} else {
		handleBuilder.CompactionGenerate = invocation
	}
	request := new(extensionpb.HostRequest)
	request.SetHandle(handleBuilder.Build())
	started, err := connection.Start(t.Context(), "handle-"+handlerID, request)
	require.NoError(t, err)
	completed, err := started.Wait(t.Context(), nil)
	require.NoError(t, err)
	return completed.GetHandle()
}

// compactionPublicRequest creates one complete public request fixture.
func compactionPublicRequest() *extensionpb.CompactionRequest {
	return extensionpb.CompactionRequest_builder{
		Trigger: new(extensionpb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL), RetryIntent: new(false),
		Instructions: nil,
		Model: extensionpb.ModelDescriptor_builder{
			ProviderId: new("provider"), ModelId: new("model"), InputModalities: nil,
			ContextWindow: new(int64(1000)), MaxTokens: new(int64(100)), Reasoning: nil, Tools: nil, Pricing: nil,
		}.Build(),
		ReasoningChoice: new("off"), Prefix: nil, Suffix: nil, Previous: nil,
		ContextTokens: new(int64(10)), ContextTokensEstimated: new(true),
		ContextWindow: new(int64(1000)), ResponseBudget: new(int64(100)), RetainedBudget: new(int64(20)),
	}.Build()
}

// compactionHandlerResponse creates either a supplied request result or generated result.
func compactionHandlerResponse(requestHandler bool) *extensionpb.HandleResponse {
	source := new(extensionpb.BranchSummarySource)
	source.SetExtensionId("extension")
	text := "generated"
	if requestHandler {
		text = "supplied"
	}
	result := extensionpb.CompactionResult_builder{
		Summary: new(text), FirstKeptEntryId: new("kept"), Source: source, Details: nil,
	}.Build()
	builder := extensionpb.HandleResponse_builder{
		SessionBeforeTreeRequest: nil, SessionBeforeTreeResult: nil, SessionTree: nil, Error: nil, Lifecycle: nil,
		ModelSelection: nil, ReasoningSelection: nil, Retry: nil,
		CompactionRequest: nil, CompactionGenerate: nil, CompactionResult: nil,
		CompactionSuccess: nil, CompactionFailure: nil,
	}
	if requestHandler {
		builder.CompactionRequest = extensionpb.CompactionRequestAction_builder{
			Cancel:        new(false),
			RequestAction: new(extensionpb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE),
			Request:       nil,
			ResultAction:  new(extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE),
			Result:        result,
		}.Build()
	} else {
		builder.CompactionGenerate = result
	}
	return builder.Build()
}
