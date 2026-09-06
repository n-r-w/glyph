//go:build integration

package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
)

// runTestOperations exercises the actual controller against a supplied runtime stream.
func runTestOperations(
	t *testing.T,
	transport *Service,
	ctx context.Context,
	activate func(),
	prepare func(
		context.Context,
		controllerui.Command,
	) (operation.Prepared[controllerui.Frame, controllerui.Frame], error),
) error {
	t.Helper()
	mockController := gomock.NewController(t)
	source := controllerui.NewMockStreamSource(mockController)
	source.EXPECT().Open(ctx).Return(transport, nil)
	session := controllerui.NewMockSession(mockController)
	session.EXPECT().Initialize(ctx).Return(nil)
	session.EXPECT().Activate(ctx).DoAndReturn(func(context.Context) func() {
		activate()
		return func() {}
	})
	session.EXPECT().Prepare(gomock.Any(), gomock.Any()).DoAndReturn(prepare).AnyTimes()
	controller := controllerui.New(source)
	if err := controller.Open(ctx); err != nil {
		return err
	}
	return controller.Execute(ctx, session)
}

// testProgressOutput binds real operation progress to a generated delivery mock.
func testProgressOutput(
	t *testing.T,
) (*Service, *operationmock.MockOperationDelivery[controllerui.Frame, controllerui.Frame]) {
	t.Helper()
	mockController := gomock.NewController(t)
	service := New()
	fixtureContext, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	writer := operation.NewWriter(func(string) error { return nil })
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.Run(fixtureContext) }()
	delivery := operationmock.NewMockOperationDelivery[controllerui.Frame, controllerui.Frame](mockController)
	delivery.EXPECT().Accepted("operation").DoAndReturn(func(string) (*operation.Acknowledgement, error) {
		return writer.EnqueueAcknowledged("accepted")
	})
	delivery.EXPECT().Running("operation").Return(nil)
	delivery.EXPECT().
		Terminal("operation", gomock.Any()).
		DoAndReturn(func(string, operation.Outcome[controllerui.Frame]) (*operation.Acknowledgement, error) {
			return writer.EnqueueAcknowledged("terminal")
		})
	prepared := operationmock.NewMockOperationPrepared[controllerui.Frame, controllerui.Frame](mockController)
	bound := make(chan struct{})
	stop := make(chan struct{})
	prepared.EXPECT().
		Run(gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context,
			reporter operation.Reporter[controllerui.Frame],
		) operation.Outcome[controllerui.Frame] {
			unbind := service.BindProgress(reporter)
			defer unbind()
			close(bound)
			<-stop
			return operation.Completed(controllerui.NewFrame(controllerui.FrameSubmitCompleted))
		})
	prepared.EXPECT().Release()
	owner := operation.NewOwner(fixtureContext, delivery)
	require.NoError(
		t,
		owner.Start(
			"operation",
			func() (operation.Prepared[controllerui.Frame, controllerui.Frame], error) { return prepared, nil },
		),
	)
	<-bound
	t.Cleanup(func() {
		defer cancel()
		close(stop)
		owner.Wait()
		require.NoError(t, owner.Err())
		writer.Close()
		require.NoError(t, <-writerDone)
	})
	return service, delivery
}
