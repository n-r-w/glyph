//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestContextSelectionStartAndWait verifies typed model and reasoning starts, nonblocking admission,
// and local wait independence.
func TestContextSelectionStartAndWait(t *testing.T) {
	t.Parallel()

	// Arrange: block model admission and return controlled typed results for both selection kinds.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	execution := NewMockExecuteOperation(controller)
	host := NewMockHostService(controller)
	modelOperation := NewMockHostOperation(controller)
	reasoningOperation := NewMockHostOperation(controller)
	admissionEntered := make(chan struct{})
	admissionGate := make(chan struct{})
	modelRunning := make(chan struct{})
	startReturned := make(chan struct{})
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(contractRegistration(), nil)
	registration.EXPECT().Release()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execution, nil)
	execution.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "provider", request.GetSelectModel().GetProviderId())
			assert.Equal(t, "model", request.GetSelectModel().GetModelId())
			assert.NotEmpty(t, request.GetSelectModel().GetContext().GetContextId())
			close(admissionEntered)
			<-admissionGate
			return modelOperation, nil
		},
	)
	modelOperation.EXPECT().Run(gomock.Any()).DoAndReturn(
		func(context.Context) (*extensionpb.HostCompleted, error) {
			close(modelRunning)
			return selectionCompleted("provider", "model", "low", true), nil
		},
	)
	modelOperation.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			assert.Equal(t, "high", request.GetSelectReasoning().GetReasoningChoice())
			assert.NotEmpty(t, request.GetSelectReasoning().GetContext().GetContextId())
			return reasoningOperation, nil
		},
	)
	reasoningOperation.EXPECT().Run(gomock.Any()).Return(selectionCompleted("provider", "model", "high", false), nil)
	reasoningOperation.EXPECT().Release()
	execution.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
			binding, err := ContextFrom(ctx)
			if err != nil {
				return nil, err
			}
			modelSelection, err := binding.StartModelSelection(ctx, extensionpb.SelectModelRequest_builder{
				Context: nil, ProviderId: new("provider"), ModelId: new("model"),
			}.Build())
			if err != nil {
				return nil, err
			}
			close(startReturned)
			waitContext, cancelWait := context.WithCancel(ctx)
			cancelWait()
			_, err = modelSelection.Wait(waitContext)
			if !errors.Is(err, context.Canceled) {
				return nil, errors.New("local selection wait did not observe its own cancellation")
			}
			<-modelRunning
			modelResult, err := modelSelection.Wait(ctx)
			if err != nil {
				return nil, err
			}
			if modelResult.GetSelection().GetReasoningChoice() != "low" || len(modelResult.GetIssues()) != 1 {
				return nil, errors.New("model selection returned an unexpected typed result")
			}
			reasoningSelection, err := binding.StartReasoningSelection(ctx, extensionpb.SelectReasoningRequest_builder{
				Context: nil, ReasoningChoice: new("high"),
			}.Build())
			if err != nil {
				return nil, err
			}
			reasoningResult, err := reasoningSelection.Wait(ctx)
			if err != nil {
				return nil, err
			}
			if reasoningResult.GetSelection().GetReasoningChoice() != "high" {
				return nil, errors.New("reasoning selection returned an unexpected typed result")
			}
			return extensionpb.ToolResult_builder{Contents: nil, IsError: new(false)}.Build(), nil
		},
	)
	connection := openContextTestConnection(t, service, host)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	registrationRequest := new(extensionpb.HostRequest)
	registrationRequest.SetRegister(new(extensionpb.RegisterRequest))
	registered, err := connection.Start(ctx, "register", registrationRequest)
	require.NoError(t, err)
	_, err = registered.Wait(ctx, nil)
	require.NoError(t, err)
	request := new(extensionpb.HostRequest)
	request.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new("contract"), ArgumentsJson: []byte(`{}`), Context: testInvocationIdentity(),
	}.Build())

	// Act: start an invocation whose model selection remains blocked before Host acceptance.
	invoked, err := connection.Start(ctx, "invoke", request)
	require.NoError(t, err)
	go func() {
		select {
		case <-admissionEntered:
		case <-ctx.Done():
			return
		}
		select {
		case <-startReturned:
			close(admissionGate)
		case <-ctx.Done():
		}
	}()
	_, err = invoked.Wait(ctx, nil)

	// Assert: both typed operations complete and canceling the first local wait does not cancel remote work.
	require.NoError(t, err)
	assert.NoError(t, connection.Close())
}

// selectionCompleted creates one typed selection completion for the public stream test.
func selectionCompleted(providerID, modelID, reasoningChoice string, withIssue bool) *extensionpb.HostCompleted {
	issues := []*extensionpb.SelectionIssue(nil)
	if withIssue {
		issues = []*extensionpb.SelectionIssue{extensionpb.SelectionIssue_builder{
			Code:        new(extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_HANDLER_ERROR),
			ExtensionId: new("extension"), HandlerId: new("handler"), Message: new("handler diagnostic"),
		}.Build()}
	}
	result := new(extensionpb.HostCompleted)
	result.SetSelection(extensionpb.SelectionResult_builder{
		Selection: extensionpb.ModelSelection_builder{
			ProviderId: new(providerID), ModelId: new(modelID), ReasoningChoice: new(reasoningChoice),
		}.Build(),
		Issues: issues,
	}.Build())
	return result
}
