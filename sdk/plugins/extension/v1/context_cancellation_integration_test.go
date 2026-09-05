//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestContextStartAndWaitHaveSeparateCancellation verifies nonblocking start, local wait cancellation, and targeted
// cancellation.
func TestContextStartAndWaitHaveSeparateCancellation(t *testing.T) {
	t.Parallel()

	// Arrange: withhold first acceptance, then control two accepted Host reads independently.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	execution := NewMockExecuteOperation(controller)
	host := NewMockHostService(controller)
	first := NewMockHostOperation(controller)
	second := NewMockHostOperation(controller)
	admissionEntered := make(chan struct{})
	admissionGate := make(chan struct{})
	firstRunning := make(chan struct{})
	firstGate := make(chan struct{})
	firstCanceled := make(chan struct{})
	secondRunning := make(chan struct{})
	secondReleased := make(chan struct{})
	startedLocally := make(chan struct{})
	waitStopped := make(chan struct{})
	releaseAdmission := sync.OnceFunc(func() { close(admissionGate) })
	releaseFirst := sync.OnceFunc(func() { close(firstGate) })
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(contractRegistration(), nil)
	registration.EXPECT().Release()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execution, nil)
	execution.EXPECT().Release()
	host.EXPECT().
		Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, string, *extensionpb.ExtensionRequest) (HostOperation, error) {
			close(admissionEntered)
			<-admissionGate
			return first, nil
		})
	first.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
		close(firstRunning)
		select {
		case <-ctx.Done():
			close(firstCanceled)
			return nil, ctx.Err()
		case <-firstGate:
			return emptyConfiguredModelResult(), nil
		}
	})
	first.EXPECT().Release()
	host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(second, nil)
	second.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
		close(secondRunning)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	second.EXPECT().Release().Do(func() { close(secondReleased) })
	execution.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
			binding, err := ContextFrom(ctx)
			if err != nil {
				return nil, err
			}
			input := extensionpb.ConfiguredModelRequest_builder{
				Context: nil,
				Selection: extensionpb.ModelSelection_builder{
					ProviderId: new("provider"), ModelId: new("model"), ReasoningChoice: new("off"),
				}.Build(),
				Instructions: new(""),
				Messages: []*extensionpb.ConfiguredModelMessage{extensionpb.ConfiguredModelMessage_builder{
					Role: new(extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER), Text: new("question"),
				}.Build()},
			}.Build()
			configured, err := binding.StartConfiguredModel(ctx, input)
			if err != nil {
				return nil, err
			}
			close(startedLocally)
			waitContext, cancelWait := context.WithCancel(ctx)
			cancelWait()
			_, err = configured.Wait(waitContext)
			if !errors.Is(err, context.Canceled) {
				return nil, errors.New("local wait did not observe its own cancellation")
			}
			close(waitStopped)
			if _, err = configured.Wait(ctx); err != nil {
				return nil, err
			}
			startContext, cancelStart := context.WithCancel(ctx)
			defer cancelStart()
			configured, err = binding.StartConfiguredModel(startContext, input)
			if err != nil {
				return nil, err
			}
			<-secondRunning
			cancelStart()
			_, err = configured.Wait(ctx)
			if !errors.Is(err, context.Canceled) {
				return nil, errors.New("operation-start cancellation did not cancel the Host target")
			}
			return extensionpb.ToolResult_builder{Contents: nil, IsError: new(false)}.Build(), nil
		})
	connection := openContextTestConnection(t, service, host)
	t.Cleanup(releaseAdmission)
	t.Cleanup(releaseFirst)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	registrationRequest := new(extensionpb.HostRequest)
	registrationRequest.SetRegister(new(extensionpb.RegisterRequest))
	registered, err := connection.Start(ctx, "register", registrationRequest)
	require.NoError(t, err)
	_, err = registered.Wait(ctx, nil)
	require.NoError(t, err)
	request := new(extensionpb.HostRequest)
	request.SetExecute(
		extensionpb.ExecuteRequest_builder{
			ToolName:      new("contract"),
			ArgumentsJson: []byte(`{}`),
			Context:       testInvocationIdentity(),
		}.Build(),
	)

	// Act: start a contextual invocation while its first nested request cannot yet be accepted.
	invoked, err := connection.Start(ctx, "invoke", request)
	require.NoError(t, err)
	awaitHostDisconnectSignal(t, ctx, admissionEntered, "Host admission")
	awaitHostDisconnectSignal(t, ctx, startedLocally, "nonblocking SDK start")
	awaitHostDisconnectSignal(t, ctx, waitStopped, "local wait cancellation")
	releaseAdmission()
	awaitHostDisconnectSignal(t, ctx, firstRunning, "first accepted read")
	select {
	case <-firstCanceled:
		t.Fatal("canceling Wait canceled remote work")
	default:
	}
	releaseFirst()
	_, err = invoked.Wait(ctx, nil)

	// Assert: the first read completed, the second target was canceled, and stream shutdown joins all work.
	require.NoError(t, err)
	awaitHostDisconnectSignal(t, ctx, secondReleased, "canceled target release")
	assert.NoError(t, connection.Close())
}

// emptyModelCatalogue supplies a typed result for lifecycle tests that do not inspect descriptor content.
func emptyModelCatalogue() *extensionpb.HostCompleted {
	result := new(extensionpb.HostCompleted)
	result.SetGetModels(extensionpb.GetModelsResult_builder{Models: nil, ActiveSelection: nil}.Build())
	return result
}

// emptyConfiguredModelResult supplies a typed result for lifecycle tests that inspect only cancellation.
func emptyConfiguredModelResult() *extensionpb.HostCompleted {
	result := new(extensionpb.HostCompleted)
	result.SetConfiguredModel(extensionpb.ConfiguredModelResult_builder{
		Content: nil, Outcome: nil, ErrorMessage: nil, ProviderId: nil, ModelId: nil,
		ResponseModelId: nil, ResponseId: nil, Usage: nil, Diagnostics: nil,
	}.Build())
	return result
}
