package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/n-r-w/glyph/host/internal/config/codingagent"
	"github.com/n-r-w/glyph/host/internal/controller/cli"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/n-r-w/glyph/host/internal/infra/browser"
	uilogging "github.com/n-r-w/glyph/host/internal/infra/logging"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"

	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	uicatalog "github.com/n-r-w/glyph/host/internal/infra/plugins/ui/catalog"
	uiruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/ui/runtime"

	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"

	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// runUIWithPaths assembles one selected UI lifecycle and explicit shutdown order.
func runUIWithPaths(
	ctx context.Context,
	paths persistence.Paths,
	command cli.Command,
	stderr io.Writer,
) (returnErr error) {
	configured, err := settingstore.New(paths.SettingsFile).Load()
	if err != nil {
		return fmt.Errorf("load Glyph settings: %w", err)
	}
	logger, logFile, err := uilogging.OpenUI(paths.LogsDirectory, paths.LogFile)
	if err != nil {
		return fmt.Errorf("initialize UI logging: %w", err)
	}
	previousLogger := slog.Default()
	slog.SetDefault(logger)
	defer func() {
		slog.SetDefault(previousLogger)
		returnErr = errors.Join(returnErr, logFile.Close())
	}()
	slog.InfoContext(ctx, "starting UI Glyph application")

	uiDirectory := command.UIDirectory
	if uiDirectory == "" {
		uiDirectory = filepath.Join(paths.Directory, "plugins", "ui")
	}
	transport := uiruntime.New()
	// extensions becomes available after the selected UI stream opens.
	var extensions *extensionmanager.Service
	selector := hostui.NewSelector(uicatalog.New(), transport)
	selection, err := selector.Select(ctx, hostui.SelectionRequest{
		Directory:  hostui.Directory{Path: uiDirectory},
		ExplicitUI: command.UIID,
		ActiveUI:   configured.ActiveUI,
	})
	transport.BindSelection(selection, stderr)
	defer func() {
		returnErr = errors.Join(returnErr, transport.Close())
		if extensions != nil {
			extensions.Close()
		}
	}()
	if err != nil {
		return fmt.Errorf("select UI plugin: %w", err)
	}

	controller := controllerui.New(transport)
	err = controller.Open(ctx)
	if err != nil {
		return fmt.Errorf("open selected UI: %w", err)
	}

	extensionFactory := extensionruntime.NewFactory()
	extensions = extensionmanager.New(catalog.New(), extensionFactory, transport)
	tools := toolservice.New(extensions)
	sessionServices, err := newSessionComposition(ctx, paths, extensions)
	if err != nil {
		return fmt.Errorf("initialize Host sessions: %w", err)
	}
	contexts := bindExtensionContexts(extensionFactory, extensions, tools, sessionServices)
	lifecycleObservers := lifecycle.New(extensions, contexts)
	lifecycleObservers.BindIssueDelivery(transport)
	startupService := startup.New(extensions, tools, sessionServices.tree, lifecycleObservers)
	_, err = startupService.Start(ctx, startup.Request{
		DataDirectory: paths.Directory, ExtensionDirectory: command.ExtensionDirectory,
	}, transport)
	if err != nil {
		return fmt.Errorf("start UI Host extensions: %w", err)
	}

	transport.BindBrowser(browser.New())
	providerCatalog, err := newProviderCatalog(configured, paths, transport)
	if err != nil {
		return fmt.Errorf("create provider catalog: %w", err)
	}
	contexts.BindCatalog(providerCatalog)
	sessionServices.active.BindPricingCatalog(providerCatalog)
	sessionServices.tree.BindModelRequester(providerCatalog)
	dispatcher := events.NewDispatcher(transport, lifecycleObservers)
	agentCore := agentrun.New(
		codingagent.Instructions(), providerCatalog, tools, dispatcher, sessionServices.active,
	)
	coordinator := runcontrol.NewCoordinator(agentCore, dispatcher, sessionServices.gate)

	session := hostui.NewSession(
		transport,
		coordinator,
		providerCatalog,
		providerCatalog,
		sessionServices.active, sessionServices.tree, sessionServices.gate,
		extensions,
	)
	sessionServices.active.BindEntryPublisher(transport)
	executionErr := controller.Execute(ctx, session)

	slog.InfoContext(context.WithoutCancel(ctx), "completed UI Glyph application")
	return executionErr
}
