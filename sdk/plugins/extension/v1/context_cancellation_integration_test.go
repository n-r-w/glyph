//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/internal/errtree"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestContextStartAndWaitHaveSeparateCancellation checks separate local and received cancellation through the SDK.
func TestContextStartAndWaitHaveSeparateCancellation(t *testing.T) {
	t.Parallel()

	// Arrange withheld admission, plain cancellation and a diagnosed Host target with joined release.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	execution := NewMockExecuteOperation(controller)
	host := NewMockHostService(controller)
	first := NewMockHostOperation(controller)
	second := NewMockHostOperation(controller)
	third := NewMockHostOperation(controller)
	source := diagnostic.NewCurrentCause(context.Canceled,
		errors.New("configured retry diagnostic\nUnicode: λ\n"+strings.Repeat("retained detail ", 512)+"  "))
	thirdRequest := make(chan *extensionpb.ExtensionRequest, 1)
	thirdRunning := make(chan struct{})
	thirdCanceled := make(chan struct{})
	thirdReleased := make(chan struct{})
	thirdGate := make(chan struct{})
	cancelThirdGate := make(chan struct{})
	localThirdWait := make(chan error, 1)
	receivedThirdWait := make(chan error, 1)
	releaseThird := sync.OnceFunc(func() { close(thirdGate) })
	allowThirdCancellation := sync.OnceFunc(func() { close(cancelThirdGate) })
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
	first.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ *HostProgressReporter) (*extensionpb.HostCompleted, error) {
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
	second.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ *HostProgressReporter) (*extensionpb.HostCompleted, error) {
			close(secondRunning)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	second.EXPECT().Release().Do(func() { close(secondReleased) })
	host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
			thirdRequest <- request
			return third, nil
		})
	third.EXPECT().Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ *HostProgressReporter) (*extensionpb.HostCompleted, error) {
			close(thirdRunning)
			<-ctx.Done()
			close(thirdCanceled)
			<-thirdGate
			return nil, source
		})
	third.EXPECT().Release().Do(func() { close(thirdReleased) })
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
					Role: new(
						extensionpb.ConfiguredModelRole_CONFIGURED_MODEL_ROLE_USER,
					),
					Content: []*extensionpb.ConfiguredModelInputContent{
						extensionpb.ConfiguredModelInputContent_builder{Text: new("question"), Image: nil}.Build(),
					},
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
			thirdContext, cancelThird := context.WithCancel(ctx)
			defer cancelThird()
			configured, err = binding.StartConfiguredModel(thirdContext, input)
			if err != nil {
				return nil, err
			}
			<-thirdRunning
			localContext, cancelLocal := context.WithCancel(ctx)
			cancelLocal()
			_, localErr := configured.Wait(localContext)
			localThirdWait <- localErr
			<-cancelThirdGate
			cancelThird()
			_, receivedErr := configured.Wait(ctx)
			receivedThirdWait <- receivedErr
			return extensionpb.ToolResult_builder{Contents: nil, IsError: new(false)}.Build(), nil
		})
	connection := openContextTestConnection(t, service, host)
	t.Cleanup(releaseAdmission)
	t.Cleanup(releaseFirst)
	t.Cleanup(releaseThird)
	t.Cleanup(allowThirdCancellation)
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
	awaitHostDisconnectSignal(t, ctx, thirdRunning, "diagnostic target start")
	localErr := <-localThirdWait
	require.ErrorIs(t, localErr, context.Canceled)
	assert.True(t, errtree.IsPlainCancellation(localErr, func(err error) bool {
		return errors.Is(err, context.Canceled)
	}))
	select {
	case <-thirdCanceled:
		t.Fatal("local Wait interruption canceled the diagnostic target")
	default:
	}
	allowThirdCancellation()
	awaitHostDisconnectSignal(t, ctx, thirdCanceled, "diagnostic target cancellation")
	select {
	case <-thirdReleased:
		t.Fatal("Host target released before its blocked Run returned")
	default:
	}
	releaseThird()
	_, err = invoked.Wait(ctx, nil)

	// Assert successful, plain-canceled and diagnosed-canceled targets join release before connection shutdown.
	require.NoError(t, err)
	awaitHostDisconnectSignal(t, ctx, secondReleased, "canceled target release")
	awaitHostDisconnectSignal(t, ctx, thirdReleased, "diagnostic target release")
	requestReceived := <-thirdRequest
	require.NotNil(t, requestReceived.GetConfiguredModel())
	assert.Empty(t, requestReceived.GetParentCompactionId())
	receivedErr := <-receivedThirdWait
	var canceled *CanceledError
	require.ErrorAs(t, receivedErr, &canceled)
	require.ErrorIs(t, receivedErr, context.Canceled)
	assert.Contains(t, receivedErr.Error(), source.Error())
	assert.True(t, errtree.IsCurrentCancellation(receivedErr, func(err error) bool {
		return errors.Is(err, context.Canceled)
	}))
	assert.False(t, errtree.IsPlainCancellation(receivedErr, func(err error) bool {
		return errors.Is(err, context.Canceled)
	}))
	wrapped := fmt.Errorf("configured receiver: %w", receivedErr)
	var wrappedCanceled *CanceledError
	require.ErrorAs(t, wrapped, &wrappedCanceled)
	assert.Same(t, canceled, wrappedCanceled)
	require.ErrorIs(t, wrapped, receivedErr)
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
