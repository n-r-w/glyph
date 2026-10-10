//go:build integration

package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	manager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestManagerCloseStartsAllRuntimeShutdowns checks independent shutdown initiation before owner joins.
func TestManagerCloseStartsAllRuntimeShutdowns(t *testing.T) {
	t.Parallel()

	// Arrange two real runtimes with nested Host workers whose releases remain blocked.
	controller := gomock.NewController(t)
	reporter := manager.NewMockFailureReporter(controller)
	factory := NewFactory()
	service := manager.New(catalog.New(), factory, reporter)
	allowRelease := make(chan struct{})
	release := sync.OnceFunc(func() { close(allowRelease) })
	t.Cleanup(func() { release(); _ = service.Close() })
	// releaseStarted exposes each independent connection's cleanup entry.
	releaseStarted := map[string]chan struct{}{"first": make(chan struct{}), "second": make(chan struct{})}
	// sources distinguish acquired failures from both runtime owners.
	sources := map[string]error{
		"first":  errors.New("complete first runtime Host source"),
		"second": errors.New("complete second runtime Host source"),
	}
	factory.BindHostServiceFactory(func(extensionID, instanceID string) extensionsdk.HostService {
		host := NewMockHostService(controller)
		operation := NewMockHostOperation(controller)
		host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(
			ctx context.Context, _ string, _ *extensionpb.ExtensionRequest,
		) (extensionsdk.HostOperation, error) {
			finish, err := service.BeginContextOperation(ctx, extensionID, instanceID)
			if err != nil {
				return nil, err
			}
			operation.EXPECT().Release().Do(func() {
				close(releaseStarted[extensionID])
				<-allowRelease
				finish()
			})
			return operation, nil
		})
		operation.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(func(
			ctx context.Context, _ *extensionsdk.HostProgressReporter,
		) (*extensionpb.HostCompleted, error) {
			<-ctx.Done()
			return nil, sources[extensionID]
		})
		return host
	})
	directory := t.TempDir()
	accepted := make([]startup.AcceptedRegistration, 0, 2)
	for _, extensionID := range []string{"first", "second"} {
		path := filepath.Join(directory, extensionID)
		// script keeps the child invocation active until manager Close.
		script := fmt.Sprintf(shutdownScriptTemplate,
			runtimeHelperEnvironment, shutdownHostWorkActiveMode, os.Args[0])
		require.NoError(t, writeRuntimeExecutable(path, script))
		accepted = append(
			accepted,
			startup.AcceptedRegistration{ID: extensionID, Path: path, Tools: nil, Handlers: nil},
		)
	}
	pending, err := service.LoadPending(t.Context(), startup.Directory{Path: directory, Explicit: true})
	require.NoError(t, err)
	require.Empty(t, pending.Issues)
	require.Len(t, pending.Registrations, 2)
	service.Accept(accepted)
	service.Activate(t.Context())
	outcomes := make(chan error, 2)
	for _, extensionID := range []string{"first", "second"} {
		instanceID, available := service.ContextRuntime(extensionID)
		require.True(t, available)
		binding := runtimeTestContext()
		binding.ExtensionID = extensionID
		binding.RuntimeInstanceID = instanceID
		started := make(chan struct{})
		go func() {
			_, executeErr := service.ExecuteTool(
				t.Context(),
				extensionID,
				fixtureToolName,
				[]byte(`{}`),
				func(tool.Progress) error {
					close(started)
					return nil
				},
				binding,
			)
			outcomes <- executeErr
		}()
		select {
		case <-started:
		case <-time.After(processOperationTimeout):
			t.Fatal("runtime invocation did not start")
		}
	}

	// Act through manager close while each connection still owns its blocked Host worker.
	closed := make(chan error, 1)
	go func() { closed <- service.Close() }()

	// Assert every shutdown starts before the manager joins either blocked release.
	for extensionID, started := range releaseStarted {
		select {
		case <-started:
		case <-time.After(time.Second):
			assert.Fail(t, "manager joined one runtime before starting all shutdowns", extensionID)
		}
	}
	for range 2 {
		select {
		case outcome := <-outcomes:
			require.ErrorIs(t, outcome, manager.ErrExtensionUnavailable)
		case <-time.After(time.Second):
			assert.Fail(t, "invocation did not finish before its Host release")
		}
	}
	release()
	select {
	case closeErr := <-closed:
		for _, sourceErr := range sources {
			require.ErrorIs(t, closeErr, sourceErr)
			require.ErrorContains(t, closeErr, sourceErr.Error())
		}
	case <-time.After(processOperationTimeout):
		t.Fatal("manager did not join all runtime shutdowns")
	}
}
