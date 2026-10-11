//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// expectScopedPolicyHosts supplies model, projection, and replacement completions.
func expectScopedPolicyHosts(
	t *testing.T,
	hostService *MockHostService,
	projectionOperation, replacementOperation, modelOperation *MockHostOperation,
	candidate *extensionpb.CompactionResult,
) {
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
	hostService.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, id string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "lifetime", request.GetParentCompactionId())
			if request.GetConfiguredModel() != nil {
				return modelOperation, nil
			}
			if projection := request.GetProjectCompaction(); projection != nil {
				assert.Equal(t, "parent", projection.GetOperationId())
				assert.True(t, proto.Equal(candidate, projection.GetResult()))
				return projectionOperation, nil
			}
			replacement := request.GetReplaceCompaction()
			if replacement == nil {
				return nil, errors.New("replacement payload is missing")
			}
			assert.Equal(t, "parent", replacement.GetOperationId())
			assert.True(t, proto.Equal(candidate, replacement.GetResult()))
			return replacementOperation, nil
		},
	).Times(3)
	projected := new(extensionpb.HostCompleted)
	projected.SetProjectCompaction(extensionpb.CompactionProjection_builder{History: nil}.Build())
	projectionOperation.EXPECT().Run(gomock.Any(), gomock.Any()).Return(projected, nil)
	projectionOperation.EXPECT().Release()
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
}

// runScopedPolicyModel checks configured-model access under the policy lifetime.
func runScopedPolicyModel(
	t *testing.T,
	ctx context.Context,
	extensionContext *ExtensionContext,
	manualPolicyReasoningOff string,
) error {
	text := new(extensionpb.ConfiguredModelInputContent)
	text.SetText("nested model input")
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
		return err
	}
	nestedResult, err := nested.Wait(ctx)
	if err != nil {
		return err
	}
	assert.Equal(t, extensionpb.ConfiguredModelOutcome_CONFIGURED_MODEL_OUTCOME_STOP, nestedResult.GetOutcome())
	return nil
}

// runScopedPolicyReplacement checks candidate projection and its durable receipt.
func runScopedPolicyReplacement(
	t *testing.T,
	ctx context.Context,
	policy *CompactionPolicyContext,
	candidate *extensionpb.CompactionResult,
) error {
	projection, err := policy.StartProjectReplacement(ctx, candidate)
	if err != nil {
		return err
	}
	if projection == nil {
		return errors.New("candidate projection operation is missing")
	}
	history, err := projection.Wait(ctx)
	if err != nil {
		return err
	}
	assert.NotNil(t, history)
	replacement, err := policy.StartReplaceHistory(ctx, candidate)
	if err != nil {
		return err
	}
	if replacement == nil {
		return errors.New("replacement operation is missing")
	}
	receipt, err := replacement.Wait(ctx)
	if err != nil {
		return err
	}
	assert.Equal(t, "receipt", receipt.GetCommitted().GetEntryId())
	return nil
}

// runScopedPolicyHandler checks detached input and policy-scoped Host work.
func runScopedPolicyHandler(
	t *testing.T,
	ctx context.Context,
	input *extensionpb.CompactionPolicyInvocation,
	candidate *extensionpb.CompactionResult,
	manualPolicyReasoningOff string,
) (*extensionpb.HandleResponse, error) {
	policy, present := FindCompactionPolicy(ctx)
	if !present {
		return nil, errors.New("policy authority is missing")
	}
	captured := policy.Input()
	if captured == nil {
		return nil, errors.New("policy input is missing")
	}
	assert.True(t, proto.Equal(input, captured))
	captured.SetSessionId("changed by handler")
	assert.Equal(t, "session", policy.Input().GetSessionId())
	assert.Equal(t, "runtime", policy.Input().GetPolicyRuntimeId())
	extensionContext, contextErr := ContextFrom(ctx)
	if contextErr != nil {
		return nil, contextErr
	}
	if err := runScopedPolicyModel(t, ctx, extensionContext, manualPolicyReasoningOff); err != nil {
		return nil, err
	}
	if err := runScopedPolicyReplacement(t, ctx, policy, candidate); err != nil {
		return nil, err
	}
	response := new(extensionpb.HandleResponse)
	response.SetCompactionPolicy(
		extensionpb.CompactionPolicyResult_builder{State: nil, Error: nil, Canceled: new(false)}.Build(),
	)
	return response, nil
}
