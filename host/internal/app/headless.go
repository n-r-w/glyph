package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/n-r-w/glyph/host/internal/config/codingagent"

	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	headlessoutput "github.com/n-r-w/glyph/host/internal/infra/headless"

	"github.com/n-r-w/glyph/host/internal/infra/persistence"

	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"

	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/interactions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"

	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// runHeadlessWithPaths preserves the accepted one-shot Host composition.
func runHeadlessWithPaths(
	ctx context.Context,
	paths persistence.Paths,
	command headless.Command,
	stdout, stderr io.Writer,
) error {
	slog.InfoContext(ctx, "starting headless Glyph application")
	configured, err := settingstore.New(paths.SettingsFile).Load()
	if err != nil {
		return fmt.Errorf("load Glyph settings: %w", err)
	}

	renderer := headlessoutput.NewRenderer(stdout, stderr)
	extensionFactory := extensionruntime.NewFactory()
	extensions := extensionmanager.New(catalog.New(), extensionFactory, renderer.ReportRuntimeFailure)
	tools := toolservice.New(extensions)
	sessionServices, err := newSessionComposition(ctx, paths, extensions)
	if err != nil {
		extensions.Close()
		return fmt.Errorf("initialize Host sessions: %w", err)
	}
	defer func() {
		extensions.Close()
		slog.DebugContext(context.WithoutCancel(ctx), "closed extension runtimes")
	}()
	contexts := bindExtensionContexts(extensionFactory, extensions, tools, sessionServices)
	sessionServices.active.BindEntryPublisher(renderer)
	lifecycleObservers := lifecycle.New(extensions, contexts)
	lifecycleObservers.BindIssueDelivery(renderer)
	startupService := startup.New(extensions, tools, sessionServices.tree, lifecycleObservers)
	_, startupErr := startupService.Start(ctx, startup.Request{
		DataDirectory: paths.Directory, ExtensionDirectory: command.ExtensionDirectory,
	}, renderer)
	if startupErr != nil {
		return fmt.Errorf("start headless Host: %w", startupErr)
	}
	extensions.Activate(ctx)

	providerCatalog, err := newProviderCatalog(configured, paths, interactions.New())
	if err != nil {
		return fmt.Errorf("create provider catalog: %w", err)
	}
	contexts.BindCatalog(providerCatalog)
	sessionServices.pricing.Bind(providerCatalog)
	sessionServices.modelRequester.Bind(providerCatalog)
	dispatcher := events.NewDispatcher(renderer, lifecycleObservers)
	agentCore := agentrun.New(
		codingagent.Instructions(), providerCatalog, tools, dispatcher, sessionServices.active,
	)
	coordinator := runcontrol.NewCoordinator(agentCore, dispatcher, sessionServices.gate)
	controller := headless.New(coordinator)
	executionErr := controller.Execute(ctx, command.UserText)
	if executionErr != nil {
		slog.ErrorContext(context.WithoutCancel(ctx), "headless Glyph application failed", "error", executionErr)
		return executionErr
	}
	slog.InfoContext(context.WithoutCancel(ctx), "completed headless Glyph application")
	return nil
}
