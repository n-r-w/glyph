//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// expectNestedCompactionHost supplies progress and the committed observer-failure result.
func expectNestedCompactionHost(
	t *testing.T,
	host *MockHostService,
	compaction *MockHostOperation,
	runningCompactionStage string,
) {
	host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "binding", request.GetCompact().GetContext().GetContextId())
			assert.Equal(t, "preserve decisions", request.GetCompact().GetInstructions())
			return compaction, nil
		},
	)
	compaction.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, reporter *HostProgressReporter) (*extensionpb.HostCompleted, error) {
			progress := new(extensionpb.HostProgress)
			progress.SetCompaction(extensionpb.CompactionProgress_builder{Stage: new(runningCompactionStage)}.Build())
			if err := reporter.Report(ctx, progress); err != nil {
				return nil, err
			}
			source := new(extensionpb.BranchSummarySource)
			source.SetExtensionId("extension")
			return extensionpb.HostCompleted_builder{
				Cancel: nil, GetModels: nil, GetProviders: nil, ConfiguredModel: nil,
				AppendExtension: nil, GetSessionState: nil, AppendExtensionMessage: nil, Selection: nil,
				Compact: extensionpb.CompactResult_builder{
					Committed: extensionpb.CommittedCompaction_builder{
						EntryId: new("compaction-entry"),
						Compaction: extensionpb.SessionTreeCompaction_builder{
							Summary: new("summary"), FirstKeptEntryId: new("kept"), Source: source,
							EstimatedCost: nil, Details: nil,
						}.Build(),
					}.Build(),
					Canceled: new(false), Error: new("observer failed after commit"),
					FailureCode: new(hostFailureCodeExtensionFailed),
				}.Build(),

				ProjectCompaction: nil,
				ReplaceCompaction: nil, InvokeCompactionRequest: nil,
			}.Build(), nil
		},
	)
	compaction.EXPECT().Release()
}

// expectNestedRecoveryHost holds session-state completion across local wait cancellation.
func expectNestedRecoveryHost(
	t *testing.T,
	host *MockHostService,
	recovery *MockHostOperation,
	recoveryEntered, recoveryRelease chan struct{},
	largePayload []byte,
) {
	host.EXPECT().
		Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "binding", request.GetGetSessionState().GetContext().GetContextId())
			return recovery, nil
		})
	recovery.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ *HostProgressReporter) (*extensionpb.HostCompleted, error) {
			close(recoveryEntered)
			<-recoveryRelease
			entry := extensionpb.SessionStateEntry_builder{
				Id: new("entry"), ParentId: new("parent"), CreatedTime: nil,
				ExtensionId: new("extension"), EntryType: new("checkpoint"), Data: largePayload, Message: nil,
			}.Build()
			return extensionpb.HostCompleted_builder{
				Compact: nil,
				Cancel:  nil, GetModels: nil, GetProviders: nil, ConfiguredModel: nil, AppendExtension: nil,
				AppendExtensionMessage: nil, Selection: nil, GetSessionState: extensionpb.GetSessionStateResult_builder{
					SessionId: new(
						"session",
					),
					ActiveLeafId: new("entry"),
					Entries:      []*extensionpb.SessionStateEntry{entry},
				}.Build(),

				ProjectCompaction: nil,
				ReplaceCompaction: nil, InvokeCompactionRequest: nil,
			}.Build(), nil
		})
	recovery.EXPECT().Release()
}

// expectNestedModelMessageHosts supplies the configured answer and persisted message.
func expectNestedModelMessageHosts(t *testing.T, host *MockHostService, configured, message *MockHostOperation) {
	host.EXPECT().
		Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "binding", request.GetConfiguredModel().GetContext().GetContextId())
			assert.Equal(t, "question", request.GetConfiguredModel().GetMessages()[0].GetContent()[0].GetText())
			return configured, nil
		})
	content := new(extensionpb.ConfiguredModelContent)
	content.SetText(extensionpb.ConfiguredModelText_builder{Text: new("answer")}.Build())
	configured.EXPECT().Run(gomock.Any(), gomock.Any()).Return(extensionpb.HostCompleted_builder{
		Compact: nil,
		Cancel:  nil, GetModels: nil, GetProviders: nil, AppendExtension: nil, GetSessionState: nil,
		AppendExtensionMessage: nil, Selection: nil, ConfiguredModel: extensionpb.ConfiguredModelResult_builder{
			Outcome: nil, ErrorMessage: nil, ProviderId: nil, ModelId: nil,
			ResponseModelId: nil, ResponseId: nil, Usage: nil, Diagnostics: nil,
			Content: []*extensionpb.ConfiguredModelContent{content},
		}.Build(),

		ProjectCompaction: nil,
		ReplaceCompaction: nil, InvokeCompactionRequest: nil,
	}.Build(), nil)
	configured.EXPECT().Release()
	host.EXPECT().
		Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "binding", request.GetAppendExtensionMessage().GetContext().GetContextId())
			assert.Equal(t, "exact text", request.GetAppendExtensionMessage().GetText())
			return message, nil
		})
	message.EXPECT().Run(gomock.Any(), gomock.Any()).Return(extensionpb.HostCompleted_builder{
		Compact:         nil,
		Cancel:          nil,
		GetModels:       nil,
		GetProviders:    nil,
		ConfiguredModel: nil,
		AppendExtension: nil,
		GetSessionState: nil,
		Selection:       nil,
		AppendExtensionMessage: extensionpb.AppendExtensionMessageResult_builder{
			Entry: extensionpb.SessionStateEntry_builder{
				Id: new("message"), ParentId: new("entry"), CreatedTime: nil,
				ExtensionId: new("extension"), EntryType: new("note"), Data: nil,
				Message: extensionpb.ExtensionMessage_builder{
					Text: new("exact text"), Visibility: new(extensionpb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN),
				}.Build(),
			}.Build(), Issues: nil,
		}.Build(),

		ProjectCompaction: nil,
		ReplaceCompaction: nil, InvokeCompactionRequest: nil,
	}.Build(), nil)
	message.EXPECT().Release()
}

// expectNestedCatalogueHosts supplies both catalog responses for nested tool calls.
func expectNestedCatalogueHosts(
	t *testing.T,
	host *MockHostService,
	models, providers *MockHostOperation,
	disabledReasoningChoice string,
) {
	host.EXPECT().
		Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "binding", request.GetGetModels().GetContext().GetContextId())
			return models, nil
		})
	models.EXPECT().Run(gomock.Any(), gomock.Any()).Return(extensionpb.HostCompleted_builder{
		Compact: nil,
		Cancel:  nil, GetProviders: nil, ConfiguredModel: nil,
		AppendExtension: nil, GetSessionState: nil, AppendExtensionMessage: nil, Selection: nil,

		GetModels: extensionpb.GetModelsResult_builder{Models: nil, ActiveSelection: extensionpb.ModelSelection_builder{
			ProviderId: new("provider"), ModelId: new("model"), ReasoningChoice: new(disabledReasoningChoice),
		}.Build()}.Build(),

		ProjectCompaction: nil,
		ReplaceCompaction: nil, InvokeCompactionRequest: nil,
	}.Build(), nil)
	models.EXPECT().Release()
	host.EXPECT().
		Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "binding", request.GetGetProviders().GetContext().GetContextId())
			return providers, nil
		})
	providers.EXPECT().Run(gomock.Any(), gomock.Any()).Return(extensionpb.HostCompleted_builder{
		Compact: nil,
		Cancel:  nil, GetModels: nil, ConfiguredModel: nil,
		AppendExtension: nil, GetSessionState: nil, AppendExtensionMessage: nil, Selection: nil,

		GetProviders: extensionpb.GetProvidersResult_builder{Providers: []*extensionpb.ProviderDescriptor{
			extensionpb.ProviderDescriptor_builder{ProviderId: new("provider"), ModelIds: []string{"model"}}.Build(),
		}}.Build(),

		ProjectCompaction: nil,
		ReplaceCompaction: nil, InvokeCompactionRequest: nil,
	}.Build(), nil)
	providers.EXPECT().Release()
}

// runNestedCompaction checks progress and the independent durable receipt.
func runNestedCompaction(
	t *testing.T,
	ctx context.Context,
	bound *ExtensionContext,
	runningCompactionStage string,
) error {
	compact, err := bound.StartCompaction(ctx, extensionpb.CompactRequest_builder{
		Context: nil, Instructions: new("preserve decisions"),
	}.Build())
	if err != nil {
		return err
	}
	var stages []string
	compacted, err := compact.WaitWithProgress(ctx, func(progress *extensionpb.CompactionProgress) error {
		stages = append(stages, progress.GetStage())
		return nil
	})
	if err != nil {
		return err
	}
	assert.Equal(t, []string{runningCompactionStage}, stages)
	assert.Equal(t, "compaction-entry", compacted.GetCommitted().GetEntryId())
	assert.Equal(t, "observer failed after commit", compacted.GetError())
	assert.Equal(t, hostFailureCodeExtensionFailed, compacted.GetFailureCode())
	return nil
}

// runNestedSessionRecovery checks successful delivery after a canceled local wait.
func runNestedSessionRecovery(
	t *testing.T,
	ctx context.Context,
	bound *ExtensionContext,
	recoveryEntered, recoveryRelease chan struct{},
	largePayload []byte,
) error {
	// stateRead owns the session-state read across both local waits.
	stateRead, err := bound.StartGetSessionState(ctx)
	if err != nil {
		return err
	}
	<-recoveryEntered
	localWait, cancelWait := context.WithCancel(ctx)
	cancelWait()
	_, err = stateRead.Wait(localWait)
	if !errors.Is(err, context.Canceled) {
		return fmt.Errorf("local wait cancellation: %w", err)
	}
	close(recoveryRelease)
	state, err := stateRead.Wait(ctx)
	if err != nil {
		return err
	}
	assert.Equal(t, largePayload, state.GetEntries()[0].GetData())
	return nil
}

// runNestedMessageAppend checks persisted message text and visibility.
func runNestedMessageAppend(t *testing.T, ctx context.Context, bound *ExtensionContext) error {
	messageAppend, err := bound.StartAppendExtensionMessage(
		ctx,
		extensionpb.AppendExtensionMessageRequest_builder{
			Context: nil, EntryType: new("note"), Text: new("exact text"),
			Visibility: new(extensionpb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN),
		}.Build(),
	)
	if err != nil {
		return err
	}
	messageResult, err := messageAppend.Wait(ctx)
	if err != nil {
		return err
	}
	assert.Equal(t, "exact text", messageResult.GetEntry().GetMessage().GetText())
	assert.Equal(
		t,
		extensionpb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN,
		messageResult.GetEntry().GetMessage().GetVisibility(),
	)
	return nil
}

// runNestedConfiguredModel checks the answer from a session-bound model call.
func runNestedConfiguredModel(
	t *testing.T,
	ctx context.Context,
	bound *ExtensionContext,
	disabledReasoningChoice string,
) error {
	request := extensionpb.ConfiguredModelRequest_builder{
		Context: nil,
		Selection: extensionpb.ModelSelection_builder{
			ProviderId: new("provider"), ModelId: new("model"), ReasoningChoice: new(disabledReasoningChoice),
		}.Build(),
		Instructions: new(""),
		Messages: []*extensionpb.ConfiguredModelMessage{
			extensionpb.ConfiguredModelMessage_builder{
				Role: new(
					extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER,
				),
				Content: []*extensionpb.ConfiguredModelInputContent{
					extensionpb.ConfiguredModelInputContent_builder{Text: new("question"), Image: nil}.Build(),
				},
			}.Build(),
		},
	}.Build()
	modelRequest, err := bound.StartConfiguredModel(ctx, request)
	if err != nil {
		return err
	}
	configuredResult, err := modelRequest.Wait(ctx)
	if err != nil {
		return err
	}
	assert.Equal(t, "answer", configuredResult.GetContent()[0].GetText().GetText())
	return nil
}

// runNestedCatalogueReads checks both catalogs while tool execution remains active.
func runNestedCatalogueReads(t *testing.T, ctx context.Context, bound *ExtensionContext) error {
	modelRead, err := bound.StartGetModels(ctx)
	if err != nil {
		return err
	}
	modelResult, err := modelRead.Wait(ctx)
	if err != nil {
		return err
	}
	assert.Equal(t, "model", modelResult.GetActiveSelection().GetModelId())
	providerRead, err := bound.StartGetProviders(ctx)
	if err != nil {
		return err
	}
	providerResult, err := providerRead.Wait(ctx)
	if err != nil {
		return err
	}
	assert.Equal(t, []string{"model"}, providerResult.GetProviders()[0].GetModelIds())
	return nil
}

// expectNestedToolExecution arranges tool work that waits on both receive loops.
func expectNestedToolExecution(
	t *testing.T,
	execution *MockExecuteOperation,
	identity *extensionpb.ExtensionContext,
	disabledReasoningChoice, runningCompactionStage string,
	recoveryEntered, recoveryRelease chan struct{},
	largePayload []byte,
) {
	execution.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
			bound, err := ContextFrom(ctx)
			if err != nil {
				return nil, err
			}
			assert.Equal(t, identity.GetContextId(), bound.Identity().GetContextId())
			if err = runNestedCatalogueReads(t, ctx, bound); err != nil {
				return nil, err
			}
			if err = runNestedConfiguredModel(t, ctx, bound, disabledReasoningChoice); err != nil {
				return nil, err
			}
			if err = runNestedMessageAppend(t, ctx, bound); err != nil {
				return nil, err
			}
			if err = runNestedSessionRecovery(
				t,
				ctx,
				bound,
				recoveryEntered,
				recoveryRelease,
				largePayload,
			); err != nil {
				return nil, err
			}
			if err = runNestedCompaction(t, ctx, bound, runningCompactionStage); err != nil {
				return nil, err
			}
			return extensionpb.ToolResult_builder{Contents: nil, IsError: new(false)}.Build(), nil
		})
}
