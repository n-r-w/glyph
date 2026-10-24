//go:build integration

package app

import (
	"bufio"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/n-r-w/glyph/host/internal/config/codingagent"
	controllerprogrammatic "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	runtimeadapter "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	programmaticoutput "github.com/n-r-w/glyph/host/internal/infra/programmatic/output"
	programmaticsocket "github.com/n-r-w/glyph/host/internal/infra/programmatic/socket"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionmodels"
	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// liveCallerFixture combines public control with direct Host invocation of the ordinary fixture tool.
type liveCallerFixture struct {
	// programmaticFixture owns the public control client and application completion.
	*programmaticFixture
	// tools invokes the ordinary caller while the application operation gate is idle.
	tools *toolservice.Service
}

// startLiveCallerFixture assembles production components with access to the existing Host tool entry point.
func startLiveCallerFixture(t *testing.T, paths persistence.Paths, directory string) *liveCallerFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	configured, err := settingstore.New(paths.SettingsFile).Load()
	require.NoError(t, err)
	delivery := programmaticoutput.New()
	factory := runtimeadapter.NewFactory()
	runtimes := extensionmanager.New(catalog.New(), factory, delivery)
	tools := toolservice.New(runtimes)
	sessionServices, err := newSessionComposition(ctx, paths, runtimes)
	require.NoError(t, err)
	contexts := bindExtensionContexts(runtimes, tools, sessionServices, configured.Compaction.ParticipantTimeout)
	observers := lifecycle.New(runtimes, contexts)
	providerCatalog, err := newProviderCatalog(configured, paths, nil)
	require.NoError(t, err)
	modelExecution := modelexecution.New(providerCatalog, retryPolicy(configured), runtimes, delivery, contexts)
	modelExecution.BindManualCompaction(contexts, tools, codingagent.Instructions())
	extensionModels := extensionmodels.New(providerCatalog, modelExecution, contexts)
	require.NoError(t, sessionServices.active.BindPricingCatalog(providerCatalog))
	require.NoError(t, sessionServices.tree.BindModels(providerCatalog, modelExecution))
	selection := modelselection.New(providerCatalog, delivery)
	selection.BindHandlers(runtimes, contexts, delivery)
	selection.BindProtection(contexts)
	selection.BindObserver(observers)
	bindExtensionHostFactory(
		factory,
		runtimes,
		extensionModels,
		contexts,
		selection,
		modelExecution,
		sessionServices.gate,
	)
	startupService := startup.New(runtimes, tools, sessionServices.tree, observers, selection)
	require.NoError(t, startupService.BindCompaction(contexts))
	_, err = startupService.Load(ctx, startup.Request{DataDirectory: paths.Directory, ExtensionDirectory: directory})
	require.NoError(t, err)
	runtimes.Activate(ctx)
	dispatcher := events.NewDispatcher(delivery, observers)
	core := agentrun.New(codingagent.Instructions(), modelExecution, tools, dispatcher, sessionServices.active)
	coordinator := runcontrol.NewCoordinator(core, dispatcher, sessionServices.gate)
	session := hostprogrammatic.New(
		coordinator, providerCatalog, core, sessionServices.active, sessionServices.tree, sessionServices.gate,
		delivery, selection, modelExecution,
	)
	require.NoError(t, session.BindCompactor(modelExecution))
	controller := controllerprogrammatic.New(ctx, session, delivery)
	observers.BindIssueDelivery(delivery)
	sessionServices.active.BindEntryPublisher(delivery)
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	programmaticpb.RegisterProgrammaticControlServiceServer(server, controller)
	socketService, err := programmaticsocket.New(ctx, filepath.Join(t.TempDir(), "control.sock"))
	require.NoError(t, err)
	results := make(chan error, 1)
	go func() {
		serveErr := runProgrammaticServer(ctx, server, socketService, controller.Completions())
		results <- errors.Join(serveErr, runtimes.Close(), socketService.Close())
	}()
	connection, err := grpc.NewClient(
		"unix://"+socketService.Path(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	stream, err := programmaticpb.NewProgrammaticControlServiceClient(connection).Open(t.Context())
	require.NoError(t, err)
	return &liveCallerFixture{
		programmaticFixture: &programmaticFixture{
			cancel: cancel, connection: connection, stream: stream, result: results,
			stdoutReader: bufio.NewReader(strings.NewReader("")), socketPath: socketService.Path(),
		},
		tools: tools,
	}
}

// startCaller starts one live ordinary invocation before the test sends its socket command.
func (f *liveCallerFixture) startCaller(t *testing.T) <-chan error {
	t.Helper()
	const callerTool = "reference_manual_caller"
	arguments, err := model.NewToolCallArguments([]byte(`{}`))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, executeErr := f.tools.Execute(t.Context(), model.ToolCall{
			ID: "live-caller", Name: callerTool, Arguments: arguments,
		}, nil)
		done <- executeErr
	}()
	return done
}
