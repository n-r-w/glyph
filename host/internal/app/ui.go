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
	"github.com/n-r-w/glyph/host/internal/usecase/host/interactions"
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
	selector := hostui.NewSelector(uicatalog.New(), transport)
	selection, err := selector.Select(ctx, hostui.SelectionRequest{
		Directory:  hostui.Directory{Path: uiDirectory},
		ExplicitUI: command.UIID,
		ActiveUI:   configured.ActiveUI,
	})
	if err != nil {
		return errors.Join(fmt.Errorf("select UI plugin: %w", err), writeSelectionWarnings(stderr, selection.Issues))
	}

	// Selection warnings fall back to stderr until the initialization frame owns their transport.
	selectionWarningsDelivered := false
	defer func() {
		if !selectionWarningsDelivered {
			returnErr = errors.Join(returnErr, writeSelectionWarnings(stderr, selection.Issues))
		}
	}()

	controller := controllerui.New(transport)
	err = controller.Open(ctx)
	if err != nil {
		transport.Close()
		return fmt.Errorf("open selected UI: %w", err)
	}

	extensionFactory := extensionruntime.NewFactory()
	extensions := extensionmanager.New(catalog.New(), extensionFactory, transport.ReportRuntimeFailure)
	tools := toolservice.New(extensions)
	sessionServices, err := newSessionComposition(ctx, paths, extensions)
	if err != nil {
		transport.Close()
		extensions.Close()
		return fmt.Errorf("initialize Host sessions: %w", err)
	}
	contexts := bindExtensionContexts(extensionFactory, extensions, tools, sessionServices)
	lifecycleObservers := lifecycle.New(extensions, contexts)
	lifecycleObservers.BindIssueDelivery(transport)
	startupService := startup.New(extensions, tools, sessionServices.tree, lifecycleObservers)
	report, err := startupService.Load(ctx, startup.Request{
		DataDirectory: paths.Directory, ExtensionDirectory: command.ExtensionDirectory,
	})
	if err != nil {
		transport.Close()
		extensions.Close()
		return fmt.Errorf("start UI Host extensions: %w", err)
	}

	interaction := interactions.NewUI(transport.PresentAuthorizationURL, browser.New())
	providerCatalog, err := newProviderCatalog(configured, paths, interaction)
	if err != nil {
		transport.Close()
		extensions.Close()
		return fmt.Errorf("create provider catalog: %w", err)
	}
	contexts.BindCatalog(providerCatalog)
	sessionServices.pricing.Bind(providerCatalog)
	sessionServices.modelRequester.Bind(providerCatalog)
	dispatcher := events.NewDispatcher(transport, lifecycleObservers)
	agentCore := agentrun.New(
		codingagent.Instructions(), providerCatalog, tools, dispatcher, sessionServices.active,
	)
	coordinator := runcontrol.NewCoordinator(agentCore, dispatcher, sessionServices.gate)
	initialization := hostui.BuildInitialization(
		selection.ID,
		mapUIExtensionLoadReport(report),
		selection.Issues,
		providerCatalog,
	)
	initialization.SessionInfo = sessionServices.active.ActiveInfo()
	session := hostui.NewSession(
		transport,
		coordinator,
		providerCatalog,
		providerCatalog,
		sessionServices.control, sessionServices.gate,
		func(activationContext context.Context) {
			selectionWarningsDelivered = true
			extensions.Activate(activationContext)
		},
		initialization,
	)
	contexts.BindMessagePublisher(transport.PublishSessionEntry)
	executionErr := controller.Execute(ctx, session)

	transport.Close()
	extensions.Close()
	slog.InfoContext(context.WithoutCancel(ctx), "completed UI Glyph application")
	return executionErr
}

// mapUIExtensionLoadReport maps extension startup state to the UI-owned initialization input.
func mapUIExtensionLoadReport(report startup.LoadReport) hostui.ExtensionLoadReport {
	issues := make([]hostui.ExtensionLoadIssue, len(report.Issues))
	for index := range report.Issues {
		issues[index] = hostui.ExtensionLoadIssue{
			PluginIDs: report.Issues[index].PluginIDs,
			Path:      report.Issues[index].Path,
			Err:       report.Issues[index].Err,
		}
	}
	extensions := make([]hostui.LoadedExtension, len(report.Extensions))
	for index := range report.Extensions {
		tools := make([]string, len(report.Extensions[index].Tools))
		for toolIndex := range report.Extensions[index].Tools {
			tools[toolIndex] = report.Extensions[index].Tools[toolIndex].Name
		}
		extensions[index] = hostui.LoadedExtension{
			ID: report.Extensions[index].ID, Path: report.Extensions[index].Path, Tools: tools,
		}
	}
	return hostui.ExtensionLoadReport{Issues: issues, Extensions: extensions}
}

// writeSelectionWarnings reports excluded UI candidates before a selected UI owns presentation.
func writeSelectionWarnings(stderr io.Writer, issues []hostui.SelectionIssue) error {
	var warningErr error
	for _, issue := range issues {
		warningErr = errors.Join(warningErr, cli.WriteWarning(stderr, issue.Warning().Text))
	}
	return warningErr
}
