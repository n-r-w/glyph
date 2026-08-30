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

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"

	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"
	"github.com/n-r-w/glyph/host/internal/infra/browser"
	uilogging "github.com/n-r-w/glyph/host/internal/infra/logging"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"

	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	uicatalog "github.com/n-r-w/glyph/host/internal/infra/plugins/ui/catalog"
	uiruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/ui/runtime"

	"github.com/n-r-w/glyph/host/internal/infra/terminal"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/interactions"

	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// runUIWithPaths assembles one selected UI lifecycle and explicit shutdown order.
func runUIWithPaths(
	ctx context.Context,
	paths persistence.Paths,
	command cli.Command,
	captureTerminal func() (*terminal.Recovery, error),
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
	selector := hostui.NewSelector(uicatalog.New(), uiruntime.NewFactory())
	selection, err := selector.Select(ctx, hostui.SelectionRequest{
		Directory:  domainui.Directory{Path: uiDirectory},
		ExplicitUI: command.UIID,
		ActiveUI:   configured.ActiveUI,
	})
	if err != nil {
		return errors.Join(fmt.Errorf("select UI plugin: %w", err), writeSelectionWarnings(stderr, selection.Issues))
	}

	// Selection warnings fall back to stderr until the initialization frame owns their delivery.
	selectionWarningsDelivered := false
	defer func() {
		if !selectionWarningsDelivered {
			returnErr = errors.Join(returnErr, writeSelectionWarnings(stderr, selection.Issues))
		}
	}()

	var recovery *terminal.Recovery
	if selection.Capabilities.ControlsTerminal {
		recovery, err = captureTerminal()
		if err != nil {
			selection.Runtime.Close()
			return fmt.Errorf("capture selected UI terminal: %w", err)
		}
	}
	channel, err := selection.Runtime.Open(ctx)
	if err != nil {
		selection.Runtime.Close()
		recoveryErr := recovery.Restore()
		return errors.Join(fmt.Errorf("open selected UI: %w", err), recoveryErr)
	}

	delivery := hostui.NewDelivery(channel)
	extensions := extensionservice.New(catalog.New(), extensionruntime.NewFactory(), delivery.ReportRuntimeFailure)
	sessionServices, err := newSessionComposition(ctx, paths, extensions)
	if err != nil {
		selection.Runtime.Close()
		recoveryErr := recovery.Restore()
		extensions.Close()
		return errors.Join(fmt.Errorf("initialize Host sessions: %w", err), recoveryErr)
	}
	startupService := startup.New(extensions.Load)
	report, err := startupService.Load(ctx, startup.Request{
		DataDirectory: paths.Directory, ExtensionDirectory: command.ExtensionDirectory,
	})
	if err != nil {
		selection.Runtime.Close()
		recoveryErr := recovery.Restore()
		extensions.Close()
		return errors.Join(fmt.Errorf("start UI Host extensions: %w", err), recoveryErr)
	}

	hookRunner := hookrunner.New(nil, nil, nil)
	interaction := interactions.NewUI(delivery.PresentAuthorizationURL, browser.New())
	providerCatalog, err := newProviderCatalog(configured, paths, interaction, hookRunner)
	if err != nil {
		selection.Runtime.Close()
		recoveryErr := recovery.Restore()
		extensions.Close()
		return errors.Join(fmt.Errorf("create provider catalog: %w", err), recoveryErr)
	}
	sessionServices.pricing.Bind(providerCatalog)
	sessionServices.models.Bind(providerCatalog)
	dispatcher := events.NewDispatcher(delivery.DeliverAgent, delivery.DeliverSettled)
	agentCore := agentrun.New(
		codingagent.Instructions(), providerCatalog, hookRunner, extensions, dispatcher, sessionServices.active,
	)
	coordinator := events.NewCoordinator(agentCore.Run, agentCore.Settle, dispatcher, sessionServices.gate)
	session := hostui.NewSession(
		channel,
		coordinator,
		providerCatalog,
		providerCatalog,
		sessionServices.control,
		func(activationContext context.Context) {
			selectionWarningsDelivered = true
			extensions.Activate(activationContext)
		},
	)
	controller := controllerui.New(session)
	initialization := hostui.BuildInitialization(
		selection.ID,
		mapUIExtensionLoadReport(report),
		selection.Issues,
		providerCatalog,
	)
	initialization.SessionInfo = sessionServices.active.ActiveInfo()
	executionErr := controller.Execute(ctx, initialization)

	// The selected process stops before terminal recovery; extensions stop after recovery.
	selection.Runtime.Close()
	recoveryErr := recovery.Restore()
	extensions.Close()
	slog.InfoContext(context.WithoutCancel(ctx), "completed UI Glyph application")
	return errors.Join(executionErr, recoveryErr)
}

// mapUIExtensionLoadReport maps extension startup state to the UI-owned initialization input.
func mapUIExtensionLoadReport(report extensionservice.LoadReport) hostui.ExtensionLoadReport {
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
