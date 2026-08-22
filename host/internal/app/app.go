// Package app assembles and runs the concrete Glyph Host.
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
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	internalhooks "github.com/n-r-w/glyph/host/internal/hooks"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"
	"github.com/n-r-w/glyph/host/internal/infra/browser"
	uilogging "github.com/n-r-w/glyph/host/internal/infra/logging"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	credentialstore "github.com/n-r-w/glyph/host/internal/infra/persistence/credentials"
	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	uicatalog "github.com/n-r-w/glyph/host/internal/infra/plugins/ui/catalog"
	uiruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/ui/runtime"
	"github.com/n-r-w/glyph/host/internal/infra/providers/openai/codex"
	"github.com/n-r-w/glyph/host/internal/infra/terminal"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/interactions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/providers"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// Run initializes user data paths and performs one validated invocation.
func Run(ctx context.Context, command cli.Command, stdout, stderr io.Writer) error {
	paths, err := persistence.Initialize()
	if err != nil {
		return fmt.Errorf("initialize Glyph persistence: %w", err)
	}
	return runWithPaths(ctx, paths, command, stdout, stderr)
}

// runWithPaths selects the isolated headless or UI composition path.
func runWithPaths(
	ctx context.Context,
	paths persistence.Paths,
	command cli.Command,
	stdout, stderr io.Writer,
) error {
	if command.Mode == cli.ModeHeadless {
		return runHeadlessWithPaths(ctx, paths, command.Headless, stdout, stderr)
	}
	return runUIWithPaths(ctx, paths, command, stderr)
}

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
	tools := toolservice.New(catalog.New(), extensionruntime.NewFactory(), renderer.ReportRuntimeFailure)
	defer func() {
		tools.Close()
		slog.DebugContext(context.WithoutCancel(ctx), "closed extension runtimes")
	}()
	startupService := startup.New(tools.Load)
	_, startupErr := startupService.Start(ctx, startup.Request{
		DataDirectory: paths.Directory, ExtensionDirectory: command.ExtensionDirectory,
	}, renderer)
	if startupErr != nil {
		return fmt.Errorf("start headless Host: %w", startupErr)
	}
	tools.Activate(ctx)

	hookRunner := hookrunner.New(nil, nil, nil)
	modelDescriptor := codex.ModelDescriptor(model.ID(configured.DefaultModel))
	provider := newProvider(paths, configured, modelDescriptor, interactions.New(), hookRunner)
	providerCatalog := providers.New(modelDescriptor, provider)
	dispatcher := events.NewDispatcher(renderer.DeliverAgent, renderer.DeliverSettled)
	agentCore := agentrun.New(
		codingagent.Instructions(), providerCatalog.Models()[0], providerCatalog.Provider(), hookRunner, tools, dispatcher,
	)
	coordinator := events.NewCoordinator(agentCore.Run, agentCore.Settle, dispatcher)
	controller := headless.New(coordinator)
	executionErr := controller.Execute(ctx, command.UserText)
	if executionErr != nil {
		slog.ErrorContext(context.WithoutCancel(ctx), "headless Glyph application failed", "error", executionErr)
		return executionErr
	}
	slog.InfoContext(context.WithoutCancel(ctx), "completed headless Glyph application")
	return nil
}

// runUIWithPaths assembles one selected UI lifecycle and explicit shutdown order.
func runUIWithPaths(
	ctx context.Context,
	paths persistence.Paths,
	command cli.Command,
	stderr io.Writer,
) (returnErr error) {
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

	configured, err := settingstore.New(paths.SettingsFile).Load()
	if err != nil {
		return fmt.Errorf("load Glyph settings: %w", err)
	}
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
		recovery, err = terminal.Capture()
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
	tools := toolservice.New(catalog.New(), extensionruntime.NewFactory(), delivery.ReportRuntimeFailure)
	startupService := startup.New(tools.Load)
	report, err := startupService.Load(ctx, startup.Request{
		DataDirectory: paths.Directory, ExtensionDirectory: command.ExtensionDirectory,
	})
	if err != nil {
		selection.Runtime.Close()
		recoveryErr := recovery.Restore()
		tools.Close()
		return errors.Join(fmt.Errorf("start UI Host extensions: %w", err), recoveryErr)
	}

	hookRunner := hookrunner.New(nil, nil, nil)
	modelDescriptor := codex.ModelDescriptor(model.ID(configured.DefaultModel))
	provider := newProvider(
		paths, configured, modelDescriptor,
		interactions.NewUI(delivery.PresentAuthorizationURL, browser.New()), hookRunner,
	)
	providerCatalog := providers.New(modelDescriptor, provider)
	dispatcher := events.NewDispatcher(delivery.DeliverAgent, delivery.DeliverSettled)
	agentCore := agentrun.New(
		codingagent.Instructions(), providerCatalog.Models()[0], providerCatalog.Provider(), hookRunner, tools, dispatcher,
	)
	coordinator := events.NewCoordinator(agentCore.Run, agentCore.Settle, dispatcher)
	session := hostui.NewSession(channel, coordinator, provider, func(activationContext context.Context) {
		selectionWarningsDelivered = true
		tools.Activate(activationContext)
	})
	controller := controllerui.New(session)
	initialization := hostui.BuildInitialization(selection.ID, report, selection.Issues)
	executionErr := controller.Execute(ctx, initialization)

	// The selected process stops before terminal recovery; extensions stop after recovery.
	selection.Runtime.Close()
	recoveryErr := recovery.Restore()
	tools.Close()
	slog.InfoContext(context.WithoutCancel(ctx), "completed UI Glyph application")
	return errors.Join(executionErr, recoveryErr)
}

// writeSelectionWarnings reports excluded UI candidates before a selected UI owns presentation.
func writeSelectionWarnings(stderr io.Writer, issues []hostui.SelectionIssue) error {
	var warningErr error
	for _, issue := range issues {
		warningErr = errors.Join(warningErr, cli.WriteWarning(stderr, issue.Warning().Text))
	}
	return warningErr
}

// newProvider assembles Codex with one mode-specific interaction implementation.
func newProvider(
	paths persistence.Paths,
	configured settingstore.Settings,
	modelDescriptor model.Descriptor,
	interaction codex.Interaction,
	hookRunner internalhooks.ProviderRunner,
) *codex.Service {
	thinkingLevel := ""
	if configured.DefaultThinkingLevel != nil {
		thinkingLevel = string(*configured.DefaultThinkingLevel)
	}
	credentials := credentialstore.New(paths.CredentialsFile, codex.ProviderID)
	return codex.New(codex.Config{
		Model: modelDescriptor, ThinkingLevel: thinkingLevel, Hooks: hookRunner,
	}, credentials, interaction)
}
