package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/n-r-w/glyph/host/internal/config/codingagent"

	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"

	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"

	"github.com/n-r-w/glyph/host/internal/infra/persistence"

	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"

	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/interactions"

	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
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

	renderer := headless.NewRenderer(stdout, stderr)
	extensions := extensionservice.New(catalog.New(), extensionruntime.NewFactory(), renderer.ReportRuntimeFailure)
	sessionServices, err := newSessionComposition(ctx, paths, extensions)
	if err != nil {
		extensions.Close()
		return fmt.Errorf("initialize Host sessions: %w", err)
	}
	defer func() {
		extensions.Close()
		slog.DebugContext(context.WithoutCancel(ctx), "closed extension runtimes")
	}()
	startupService := startup.New(extensions.Load)
	_, startupErr := startupService.Start(ctx, startup.Request{
		DataDirectory: paths.Directory, ExtensionDirectory: command.ExtensionDirectory,
	}, renderer)
	if startupErr != nil {
		return fmt.Errorf("start headless Host: %w", startupErr)
	}
	extensions.Activate(ctx)

	hookRunner := hookrunner.New(nil, nil, nil)
	providerCatalog, err := newProviderCatalog(configured, paths, interactions.New(), hookRunner)
	if err != nil {
		return fmt.Errorf("create provider catalog: %w", err)
	}
	sessionServices.pricing.Bind(providerCatalog)
	sessionServices.modelRequester.Bind(providerCatalog)
	dispatcher := events.NewDispatcher(renderer.DeliverAgent, renderer.DeliverSettled)
	agentCore := agentrun.New(
		codingagent.Instructions(), providerCatalog, hookRunner, extensions, dispatcher, sessionServices.active,
	)
	coordinator := events.NewCoordinator(agentCore.Run, agentCore.Settle, dispatcher, sessionServices.gate.TryAcquire)
	controller := headless.New(coordinator)
	executionErr := controller.Execute(ctx, command.UserText)
	if executionErr != nil {
		slog.ErrorContext(context.WithoutCancel(ctx), "headless Glyph application failed", "error", executionErr)
		return executionErr
	}
	slog.InfoContext(context.WithoutCancel(ctx), "completed headless Glyph application")
	return nil
}
