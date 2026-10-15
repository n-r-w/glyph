//go:build integration

// Package extensionv1 supplies the public Extension SDK.
package extensionv1

import (
	"context"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestScenario checks policy registration, invocation authority, and detached input.
func (s *compactionPolicyReceivesScopedInputSuite) TestScenario() {
	// manualPolicyReasoningOff selects disabled reasoning in public policy fixtures.
	const manualPolicyReasoningOff = "off"

	t := s.T()
	// Arrange one policy handler on the public asynchronous connection.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	handler := NewMockHandleOperation(controller)
	hostService := NewMockHostService(controller)
	projectionOperation := NewMockHostOperation(controller)
	replacementOperation := NewMockHostOperation(controller)
	modelOperation := NewMockHostOperation(controller)
	source := new(extensionpb.BranchSummarySource)
	source.SetExtensionId("extension")
	candidate := extensionpb.CompactionResult_builder{
		Summary: new("generated"), FirstKeptEntryId: new("kept"), Source: source, Details: nil,
	}.Build()
	expectScopedPolicyHosts(t, hostService, projectionOperation, replacementOperation, modelOperation, candidate)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(extensionpb.RegisterResponse_builder{
		Tools: nil, Handlers: []*extensionpb.HandlerDescriptor{extensionpb.HandlerDescriptor_builder{
			Id: new("policy"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_POLICY),
		}.Build()},
	}.Build(), nil)
	registration.EXPECT().Release()
	input := extensionpb.CompactionPolicyInvocation_builder{
		Purpose:            extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_MANUAL.Enum(),
		OverflowError:      nil,
		OperationId:        new("parent"),
		SessionId:          new("session"),
		SessionIncarnation: new(int64(1)),
		ActiveLeafId:       new("leaf"),
		Entries:            nil,
		History:            nil,
		Previous:           nil,
		Model: extensionpb.ModelDescriptor_builder{
			ProviderId: new("provider"), ModelId: new("model"), InputModalities: nil,
			ContextWindow: new(int64(1000)), MaxTokens: new(int64(100)), Reasoning: nil, Tools: nil, Pricing: nil,
		}.Build(),
		ReasoningChoice:     new(manualPolicyReasoningOff),
		Instructions:        new(""),
		Tools:               nil,
		UserInstructions:    new("guidance"),
		RequestParticipants: nil, GeneratorParticipants: nil, ResultParticipants: nil, PolicyRuntimeId: new("runtime"),
	}.Build()
	service.EXPECT().PrepareHandle(gomock.Any(), gomock.Any()).Return(handler, nil)
	handler.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) (*extensionpb.HandleResponse, error) {
		return runScopedPolicyHandler(t, ctx, input, candidate, manualPolicyReasoningOff)
	})
	handler.EXPECT().Release()
	connection := openContextTestConnection(t, service, hostService)
	registerRequest := new(extensionpb.HostRequest)
	registerRequest.SetRegister(new(extensionpb.RegisterRequest))
	registered, err := connection.Start(t.Context(), "register", registerRequest)
	require.NoError(t, err)
	_, err = registered.Wait(t.Context(), nil)
	require.NoError(t, err)
	contextIdentity := extensionpb.ExtensionContext_builder{
		ContextId: new("context"), ExtensionId: new("extension"), RuntimeInstanceId: new("runtime"),
		SessionId: new("session"), Cwd: new("/project"),
	}.Build()
	//nolint:exhaustruct_v5 // The handler builder sets only the active policy payload.
	request := extensionpb.HandleRequest_builder{
		CompletedConversation: nil,
		HandlerId:             new("policy"), Context: contextIdentity,
		ParentCompactionId: new("lifetime"), CompactionPolicy: input,
	}.Build()
	hostRequest := new(extensionpb.HostRequest)
	hostRequest.SetHandle(request)

	// Act by starting and awaiting the registered policy through the public transport.
	started, err := connection.Start(t.Context(), "invoke-policy", hostRequest)
	require.NoError(t, err)
	completed, err := started.Wait(t.Context(), nil)

	// Assert successful policy completion with no cancellation.
	require.NoError(t, err)
	policyResult := completed.GetHandle().GetCompactionPolicy()
	require.NotNil(t, policyResult)
	require.Empty(t, policyResult.GetError())
	require.Equal(
		t,
		extensionpb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_REPLACED,
		policyResult.GetDisposition(),
	)
}
