//go:build integration

package runtime

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/internal/operation"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestClientCloseCollectsReceiveCauseAfterSendEOF checks the client-side receive join after request half-close.
func TestClientCloseCollectsReceiveCauseAfterSendEOF(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange failed report delivery and a diagnostic returned by Recv only after CloseSend.
		controller := gomock.NewController(t)
		stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
		stream.EXPECT().Context().Return(t.Context()).AnyTimes()
		closed := make(chan struct{})
		receiveErr := status.Error(codes.Canceled, "complete canceled client stream source")
		stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
			<-closed
			return nil, receiveErr
		})
		stream.EXPECT().Send(gomock.Any()).Return(io.EOF)
		stream.EXPECT().CloseSend().DoAndReturn(func() error {
			synctest.Wait()
			close(closed)
			return nil
		})
		transport := New()
		transport.stream = stream
		source := errors.New("undelivered explicit UI source")

		// Act through failed controller cleanup after the application receiver stops.
		err := runTestOperations(t, transport, t.Context(), func() {
			require.NoError(t, transport.ReportError("INTERNAL", source))
		}, func(context.Context, controllerui.Command) (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
			return nil, errors.New("unexpected request preparation")
		})

		// Assert client transport diagnostics remain observable with the undelivered report.
		require.ErrorIs(t, err, receiveErr)
		require.ErrorIs(t, err, source)
		require.Equal(t, codes.Canceled, status.Code(err))
	})
}

// TestRuntimeReportDrainsBeforeUICompletion verifies activation cleanup joins an admitted report before detachment.
func TestRuntimeReportDrainsBeforeUICompletion(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange real runtime monitoring and UI output with one report paused before its enqueue step.
		controller := gomock.NewController(t)
		catalog := extensionmanager.NewMockCatalog(controller)
		factory := extensionmanager.NewMockRuntimeFactory(controller)
		process := extensionmanager.NewMockExtensionRuntime(controller)
		reporter := extensionmanager.NewMockFailureReporter(controller)
		processDone := make(chan struct{})
		monitorStarted := make(chan struct{})
		reportStarted := make(chan struct{})
		reportRelease := make(chan struct{})
		catalog.EXPECT().Discover(gomock.Any(), gomock.Any()).Return(extensionmanager.Discovery{
			DirectoryError: nil, Candidates: []extensionmanager.Executable{{ID: "tools", Path: "/tools"}}, Issues: nil,
		}, nil)
		factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(process, nil)
		process.EXPECT().Register(gomock.Any()).Return(extensionmanager.Registration{Tools: nil, Handlers: nil}, nil)
		process.EXPECT().Done().DoAndReturn(func() <-chan struct{} { close(monitorStarted); return processDone })
		process.EXPECT().Close()
		deliveryErr := errors.New("transport stopped during runtime reporting")
		streamContext, cancelStream := context.WithCancel(t.Context())
		defer cancelStream()
		stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
		stream.EXPECT().Context().Return(streamContext).AnyTimes()
		stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
			<-reportStarted
			return nil, deliveryErr
		})
		stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(request *uiv1.OpenRequest) error {
			if request.GetConnectionEvent().GetError() != nil {
				return deliveryErr
			}
			return nil
		}).AnyTimes()
		stream.EXPECT().CloseSend().DoAndReturn(func() error { cancelStream(); return nil })
		transport := New()
		transport.stream, transport.cancel, transport.ready = stream, cancelStream, true
		reporter.EXPECT().ReportRuntimeFailure(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, failure extension.RuntimeFailure) error {
				close(reportStarted)
				<-reportRelease
				return transport.ReportRuntimeFailure(ctx, failure)
			},
		)
		runtimes := extensionmanager.New(catalog, factory, reporter)
		_, err := runtimes.LoadPending(t.Context(), startup.Directory{})
		require.NoError(t, err)
		runtimes.Accept([]startup.AcceptedRegistration{{ID: "tools", Path: "/tools", Tools: nil, Handlers: nil}})
		authenticator := hostui.NewMockAuthenticator(controller)
		authenticator.EXPECT().CheckAuthentication(gomock.Any()).Return(nil)
		actualSession := hostui.NewSession(transport, nil, authenticator, nil, nil, nil, nil, runtimes)
		session := controllerui.NewMockSession(controller)
		session.EXPECT().Initialize(t.Context()).Return(nil)
		session.EXPECT().Activate(t.Context()).DoAndReturn(actualSession.Activate)
		streamSource := controllerui.NewMockStreamSource(controller)
		streamSource.EXPECT().Open(t.Context()).Return(transport, nil)
		input := controllerui.New(streamSource)
		require.NoError(t, input.Open(t.Context()))
		result := make(chan error, 1)

		// Act by detecting failure, then letting connection cleanup reach its producer barrier.
		go func() { result <- input.Execute(t.Context(), session) }()
		<-monitorStarted
		close(processDone)
		<-reportStarted
		synctest.Wait()
		returnedEarly := false
		select {
		case err = <-result:
			returnedEarly = true
		default:
		}
		close(reportRelease)
		if !returnedEarly {
			err = <-result
		}
		synctest.Wait()
		runtimeErr := runtimes.Close()

		// Assert the admitted source transferred to UI completion, not a second runtime source copy.
		assert.False(t, returnedEarly)
		failure := extension.RuntimeFailure{PluginID: "tools", Condition: extension.RuntimeUnavailableProcessExited}
		message, messageErr := failure.Message()
		require.NoError(t, messageErr)
		require.ErrorContains(t, err, message)
		require.ErrorIs(t, err, deliveryErr)
		require.NoError(t, runtimeErr)
	})
}

// TestLateAuthenticationFailureReachesControllerCompletion verifies producer joining before output detachment.
func TestLateAuthenticationFailureReachesControllerCompletion(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange real authentication work that returns its source only when activation cleanup cancels it.
		controller := gomock.NewController(t)
		source := errors.New("late authentication source")
		deliveryErr := errors.New("UI transport failed during authentication")
		authenticator := hostui.NewMockAuthenticator(controller)
		started := make(chan struct{})
		authenticator.EXPECT().CheckAuthentication(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return source
		})
		authenticator.EXPECT().IsSignInRequired(source).Return(false)
		runtime := hostui.NewMockRuntimeActivation(controller)
		runtime.EXPECT().Activate(t.Context())
		runtime.EXPECT().StopReporting()
		streamContext, cancelStream := context.WithCancel(t.Context())
		defer cancelStream()
		stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](controller)
		stream.EXPECT().Context().Return(streamContext).AnyTimes()
		stream.EXPECT().Recv().DoAndReturn(func() (*uiv1.OpenResponse, error) {
			<-started
			return nil, deliveryErr
		})
		stream.EXPECT().Send(gomock.Any()).Return(deliveryErr).AnyTimes()
		stream.EXPECT().CloseSend().DoAndReturn(func() error { cancelStream(); return nil })
		transport := New()
		transport.stream = stream
		transport.cancel = cancelStream
		transport.ready = true
		actualSession := hostui.NewSession(transport, nil, authenticator, nil, nil, nil, nil, runtime)
		session := controllerui.NewMockSession(controller)
		session.EXPECT().Initialize(t.Context()).Return(nil)
		session.EXPECT().Activate(t.Context()).DoAndReturn(actualSession.Activate)
		streamSource := controllerui.NewMockStreamSource(controller)
		streamSource.EXPECT().Open(t.Context()).Return(transport, nil)
		input := controllerui.New(streamSource)
		require.NoError(t, input.Open(t.Context()))

		// Act through controller shutdown while the authentication result is still pending.
		err := input.Execute(t.Context(), session)

		// Assert final local completion retains the late original cause and independent delivery cause.
		require.ErrorIs(t, err, source)
		require.ErrorIs(t, err, deliveryErr)
	})
}
