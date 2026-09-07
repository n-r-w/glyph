//go:build !integration

package extensionruntime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// reportingRuntime starts the real monitor against a generated runtime and reporter dependency.
func reportingRuntime(t *testing.T, report func(context.Context, extension.RuntimeFailure) error) (*Service, func()) {
	t.Helper()
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	factory := NewMockRuntimeFactory(controller)
	runtime := NewMockExtensionRuntime(controller)
	done := make(chan struct{})
	exit := sync.OnceFunc(func() { close(done) })
	catalog.EXPECT().Discover(gomock.Any(), gomock.Any()).Return(Discovery{
		DirectoryError: nil, Candidates: []Executable{{ID: "tools", Path: "/tools"}}, Issues: nil,
	}, nil)
	factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil)
	runtime.EXPECT().Register(gomock.Any()).Return(Registration{Tools: nil, Handlers: nil}, nil)
	runtime.EXPECT().Done().Return(done)
	runtime.EXPECT().Close().Do(exit)
	service := New(catalog, factory, newRuntimeReporter(t, report))
	_, err := service.LoadPending(t.Context(), startup.Directory{})
	require.NoError(t, err)
	service.Accept([]startup.AcceptedRegistration{{ID: "tools", Path: "/tools", Tools: nil, Handlers: nil}})
	service.Activate(t.Context())
	return service, exit
}

// TestReportingStopRetainsLateRuntimeFailure verifies reporting can stop without stopping the process monitor.
func TestReportingStopRetainsLateRuntimeFailure(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange an accepted live runtime whose report callback records any attempted output.
		reports := 0
		service, exit := reportingRuntime(t, func(context.Context, extension.RuntimeFailure) error {
			reports++
			return nil
		})
		failure := extension.RuntimeFailure{PluginID: "tools", Condition: extension.RuntimeUnavailableProcessExited}
		message, err := failure.Message()
		require.NoError(t, err)

		// Act by closing reporting admission, then detecting a real monitor exit before runtime cleanup.
		service.StopReporting()
		require.True(t, service.ToolRuntimeAvailable("tools"))
		exit()
		synctest.Wait()
		err = service.Close()

		// Assert the late source reaches cleanup without output or a fabricated delivery failure.
		require.EqualError(t, err, message)
		require.EqualError(t, service.Close(), message)
		assert.Zero(t, reports)
		assert.False(t, service.ToolRuntimeAvailable("tools"))
	})
}

// TestReportingStopJoinsAdmittedReport verifies the barrier waits for enqueue and preserves returned reporting failure.
func TestReportingStopJoinsAdmittedReport(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange an admitted callback still performing its output enqueue.
		started := make(chan struct{})
		release := make(chan struct{})
		deliveryErr := errors.New("runtime reporter enqueue failed")
		service, exit := reportingRuntime(t, func(context.Context, extension.RuntimeFailure) error {
			close(started)
			<-release
			return deliveryErr
		})
		exit()
		<-started
		stopped := make(chan struct{})

		// Act while the admitted report is blocked, then let it finish its enqueue step.
		go func() { service.StopReporting(); close(stopped) }()
		synctest.Wait()
		stoppedEarly := false
		select {
		case <-stopped:
			stoppedEarly = true
		default:
		}
		close(release)
		<-stopped
		err := service.Close()

		// Assert source ownership transfers to output and runtime retains only the reporting result.
		assert.False(t, stoppedEarly)
		require.ErrorIs(t, err, deliveryErr)
		require.EqualError(t, err, deliveryErr.Error())
	})
}

// TestRuntimeReportingSuccessAndIntentionalCloseRemainNonfatal preserves notification policy.
func TestRuntimeReportingSuccessAndIntentionalCloseRemainNonfatal(t *testing.T) {
	t.Parallel()
	for _, intentional := range []bool{false, true} {
		name := "reported exit"
		if intentional {
			name = "intentional close"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Arrange a live runtime with successful reporting.
				reports := 0
				service, exit := reportingRuntime(t, func(context.Context, extension.RuntimeFailure) error {
					reports++
					return nil
				})

				// Act through either an observed exit or the intentional process-close path.
				if !intentional {
					exit()
					synctest.Wait()
				}
				err := service.Close()

				// Assert success stays nonfatal and intentional shutdown emits no failure notification.
				require.NoError(t, err)
				if intentional {
					require.Zero(t, reports)
				} else {
					require.Equal(t, 1, reports)
				}
			})
		})
	}
}
