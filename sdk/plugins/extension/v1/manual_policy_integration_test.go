//go:build integration

// Package extensionv1 supplies the public Extension SDK.
package extensionv1

import (
	"context"
	"errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestScenario checks policy registration, invocation authority, and detached input.
func (s *compactionPolicyReceivesScopedInputSuite) TestScenario() {
	// manualPolicyReasoningOff selects disabled reasoning in public policy fixtures.
	const manualPolicyReasoningOff = "off"

	t := s.T()
	// Arrange one policy handler on the public asynchronous connection.
	// controller owns mock expectations.
	controller := gomock.NewController(t)
	// service owns fixture capabilities and the retained caller binding.
	service := NewMockService(controller)
	// registration supplies the expected capability catalog.
	registration := NewMockRegisterOperation(controller)
	// handler supplies the policy callback.
	handler := NewMockHandleOperation(controller)
	// hostService supplies nested host operations.
	hostService := NewMockHostService(controller)
	// projectionOperation supplies candidate history.
	projectionOperation := NewMockHostOperation(controller)
	// replacementOperation supplies the durable receipt.
	replacementOperation := NewMockHostOperation(controller)
	// modelOperation supplies nested model completion.
	modelOperation := NewMockHostOperation(controller)
	// modeled contains nested model completion.
	modeled := new(extensionpb.HostCompleted)
	modeled.SetConfiguredModel(extensionpb.ConfiguredModelResult_builder{
		Content:      nil,
		Outcome:      new(extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_STOP),
		ErrorMessage: nil,
		ProviderId: new(
			"provider",
		),
		ModelId:         new("model"),
		ResponseModelId: nil,
		ResponseId:      nil,
		Usage:           nil,
		Diagnostics:     nil,
	}.Build())
	modelOperation.EXPECT().Run(gomock.Any(), gomock.Any()).Return(modeled, nil)
	modelOperation.EXPECT().Release()
	// source identifies the candidate's supplying extension.
	source := new(extensionpb.BranchSummarySource)
	source.SetExtensionId("extension")
	// candidate contains the proposed replacement.
	candidate := extensionpb.CompactionResult_builder{
		Summary: new("generated"), FirstKeptEntryId: new("kept"), Source: source, Details: nil,
	}.Build()
	hostService.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "lifetime", request.GetParentCompactionId())
			if request.GetConfiguredModel() != nil {
				return modelOperation, nil
			}
			// projection contains the candidate projection request.
			if projection := request.GetProjectCompaction(); projection != nil {
				assert.Equal(t, "parent", projection.GetOperationId())
				assert.True(t, proto.Equal(candidate, projection.GetResult()))
				return projectionOperation, nil
			}
			// replacement owns the pending durable replacement.
			replacement := request.GetReplaceCompaction()
			if replacement == nil {
				return nil, errors.New("replacement payload is missing")
			}
			assert.Equal(t, "parent", replacement.GetOperationId())
			assert.True(t, proto.Equal(candidate, replacement.GetResult()))
			return replacementOperation, nil
		},
	).Times(3)
	// projected contains the public projection result.
	projected := new(extensionpb.HostCompleted)
	projected.SetProjectCompaction(extensionpb.CompactionProjection_builder{History: nil}.Build())
	projectionOperation.EXPECT().Run(gomock.Any(), gomock.Any()).Return(projected, nil)
	projectionOperation.EXPECT().Release()
	// replaced contains the durable replacement completion.
	replaced := new(extensionpb.HostCompleted)
	replaced.SetReplaceCompaction(extensionpb.CompactResult_builder{
		Committed: extensionpb.CommittedCompaction_builder{
			EntryId: new("receipt"),
			Compaction: extensionpb.SessionTreeCompaction_builder{
				Summary: new(candidate.GetSummary()), FirstKeptEntryId: new(candidate.GetFirstKeptEntryId()),
				Source: candidate.GetSource(), EstimatedCost: nil, Details: nil,
			}.Build(),
		}.Build(),
		Canceled:    new(false),
		Error:       nil,
		FailureCode: nil,
	}.Build())
	replacementOperation.EXPECT().Run(gomock.Any(), gomock.Any()).Return(replaced, nil)
	replacementOperation.EXPECT().Release()
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(extensionpb.RegisterResponse_builder{
		Tools: nil, Handlers: []*extensionpb.HandlerDescriptor{extensionpb.HandlerDescriptor_builder{
			Id: new("policy"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_POLICY),
		}.Build()},
	}.Build(), nil)
	registration.EXPECT().Release()
	// input contains captured policy invocation data.
	input := extensionpb.CompactionPolicyInvocation_builder{
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
		ReasoningChoice:  new(manualPolicyReasoningOff),
		Instructions:     new(""),
		Tools:            nil,
		UserInstructions: new("guidance"),
		PolicyRuntimeId:  new("runtime"),
	}.Build()
	service.EXPECT().PrepareHandle(gomock.Any(), gomock.Any()).Return(handler, nil)
	handler.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) (*extensionpb.HandleResponse, error) {
		// policy contains invocation-scoped replacement authority.
		policy, present := FindCompactionPolicy(ctx)
		if !present {
			return nil, errors.New("policy authority is missing")
		}
		// captured contains a detached invocation snapshot.
		captured := policy.Input()
		if captured == nil {
			return nil, errors.New("policy input is missing")
		}
		assert.True(t, proto.Equal(input, captured))
		captured.SetSessionId("changed by handler")
		assert.Equal(t, "session", policy.Input().GetSessionId())
		assert.Equal(t, "runtime", policy.Input().GetPolicyRuntimeId())
		// extensionContext provides ordinary nested operations.
		extensionContext, contextErr := ContextFrom(ctx)
		if contextErr != nil {
			return nil, contextErr
		}
		// text contains nested model input.
		text := new(extensionpb.ConfiguredModelInputContent)
		text.SetText("nested model input")
		// nested owns the pending nested model request.
		nested, err := extensionContext.StartConfiguredModel(ctx, extensionpb.ConfiguredModelRequest_builder{
			Context: nil,
			Selection: extensionpb.ModelSelection_builder{
				ProviderId:      new("provider"),
				ModelId:         new("model"),
				ReasoningChoice: new(manualPolicyReasoningOff),
			}.Build(),
			Instructions: new(
				"",
			),
			Messages: []*extensionpb.ConfiguredModelMessage{extensionpb.ConfiguredModelMessage_builder{
				Role: new(
					extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER,
				),
				Content: []*extensionpb.ConfiguredModelInputContent{text},
			}.Build()},
		}.Build())
		if err != nil {
			return nil, err
		}
		// nestedResult contains the collected model outcome.
		nestedResult, err := nested.Wait(ctx)
		if err != nil {
			return nil, err
		}
		assert.Equal(t, extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_STOP, nestedResult.GetOutcome())
		// projection owns the pending candidate projection.
		projection, err := policy.StartProjectReplacement(ctx, candidate)
		if err != nil {
			return nil, err
		}
		if projection == nil {
			return nil, errors.New("candidate projection operation is missing")
		}
		// history contains the projected candidate history.
		history, err := projection.Wait(ctx)
		if err != nil {
			return nil, err
		}
		assert.NotNil(t, history)
		// replacement owns the pending durable replacement.
		replacement, err := policy.StartReplaceHistory(ctx, candidate)
		if err != nil {
			return nil, err
		}
		if replacement == nil {
			return nil, errors.New("replacement operation is missing")
		}
		// receipt contains the durable replacement outcome.
		receipt, err := replacement.Wait(ctx)
		if err != nil {
			return nil, err
		}
		assert.Equal(t, "receipt", receipt.GetCommitted().GetEntryId())
		// response contains the acquired terminal completion.
		response := new(extensionpb.HandleResponse)
		response.SetCompactionPolicy(
			extensionpb.CompactionPolicyResult_builder{State: nil, Error: nil, Canceled: new(false)}.Build(),
		)
		return response, nil
	})
	handler.EXPECT().Release()
	// connection owns the caller coordination stream.
	connection := openContextTestConnection(t, service, hostService)
	// registerRequest contains capability registration input.
	registerRequest := new(extensionpb.HostRequest)
	registerRequest.SetRegister(new(extensionpb.RegisterRequest))
	// registered owns pending capability registration.
	registered, err := connection.Start(t.Context(), "register", registerRequest)
	require.NoError(t, err)
	_, err = registered.Wait(t.Context(), nil)
	require.NoError(t, err)
	// contextIdentity identifies the public session binding.
	contextIdentity := extensionpb.ExtensionContext_builder{
		ContextId: new("context"), ExtensionId: new("extension"), RuntimeInstanceId: new("runtime"),
		SessionId: new("session"), Cwd: new("/project"),
	}.Build()
	//nolint:exhaustruct_v5 // The handler builder sets only the active policy payload.
	// request contains the public operation input.
	request := extensionpb.HandleRequest_builder{
		HandlerId: new("policy"), Context: contextIdentity,
		ParentCompactionId: new("lifetime"), CompactionPolicy: input,
	}.Build()
	// hostRequest contains policy dispatch input.
	hostRequest := new(extensionpb.HostRequest)
	hostRequest.SetHandle(request)

	// Act by starting and awaiting the registered policy through the public transport.
	// started owns the pending policy invocation.
	started, err := connection.Start(t.Context(), "invoke-policy", hostRequest)
	require.NoError(t, err)
	// completed contains the collected policy completion.
	completed, err := started.Wait(t.Context(), nil)

	// Assert successful policy completion with no cancellation.
	require.NoError(t, err)
	require.False(t, completed.GetHandle().GetCompactionPolicy().GetCanceled())
}
