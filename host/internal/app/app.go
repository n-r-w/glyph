// Package app assembles and runs the concrete Glyph Host.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"google.golang.org/grpc"

	"github.com/n-r-w/glyph/host/internal/config/codingagent"
	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	controllerprogrammatic "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
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
	programmaticsocket "github.com/n-r-w/glyph/host/internal/infra/programmatic/socket"
	"github.com/n-r-w/glyph/host/internal/infra/providers/openai/codex"
	"github.com/n-r-w/glyph/host/internal/infra/providers/openai/compatible"
	"github.com/n-r-w/glyph/host/internal/infra/terminal"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/interactions"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/providers"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// Run initializes user data paths and performs one validated invocation.
func Run(ctx context.Context, command cli.Command, stdout, stderr io.Writer) error {
	paths, err := persistence.Initialize()
	if err != nil {
		return fmt.Errorf("initialize Glyph persistence: %w", err)
	}
	return runWithPaths(ctx, paths, command, stdout, stderr)
}

// runWithPaths selects an isolated composition path for the requested mode.
func runWithPaths(
	ctx context.Context,
	paths persistence.Paths,
	command cli.Command,
	stdout, stderr io.Writer,
) error {
	switch command.Mode {
	case cli.ModeHeadless:
		return runHeadlessWithPaths(ctx, paths, command.Headless, stdout, stderr)
	case cli.ModeRPC:
		return runProgrammaticWithPaths(ctx, paths, command, stdout)
	case cli.ModeUI:
		return runUIWithPaths(ctx, paths, command, stderr)
	}
	return fmt.Errorf("unsupported Glyph application mode %d", command.Mode)
}

// runProgrammaticWithPaths assembles the single-owner RPC Host.
func runProgrammaticWithPaths(
	ctx context.Context,
	paths persistence.Paths,
	command cli.Command,
	stdout io.Writer,
) (returnErr error) {
	slog.InfoContext(ctx, "starting programmatic Glyph application")
	configured, err := settingstore.New(paths.SettingsFile).Load()
	if err != nil {
		return fmt.Errorf("load Glyph settings: %w", err)
	}

	tools := toolservice.New(catalog.New(), extensionruntime.NewFactory(), func(
		reportContext context.Context,
		failure tool.RuntimeFailure,
	) error {
		message, runtimeErr := failure.Message()
		slog.ErrorContext(reportContext, message,
			"plugin_id", failure.PluginID,
			"error", runtimeErr,
		)
		return nil
	})
	toolsClosed := false
	closeTools := func() {
		if toolsClosed {
			return
		}
		tools.Close()
		toolsClosed = true
		slog.DebugContext(context.WithoutCancel(ctx), "closed extension runtimes")
	}
	defer closeTools()
	startupService := startup.New(tools.Load)
	if _, err = startupService.Load(ctx, startup.Request{
		DataDirectory: paths.Directory, ExtensionDirectory: command.ExtensionDirectory,
	}); err != nil {
		return fmt.Errorf("start programmatic Host extensions: %w", err)
	}
	tools.Activate(ctx)

	hookRunner := hookrunner.New(nil, nil, nil)
	providerCatalog, err := newProviderCatalog(configured, paths, interactions.New(), hookRunner)
	if err != nil {
		return fmt.Errorf("create provider catalog: %w", err)
	}
	delivery := hostprogrammatic.NewDelivery()
	dispatcher := events.NewDispatcher(delivery.DeliverAgent, delivery.DeliverSettled)
	agentCore := agentrun.New(codingagent.Instructions(), providerCatalog, hookRunner, tools, dispatcher)
	coordinator := events.NewCoordinator(agentCore.Run, agentCore.Settle, dispatcher)
	session := hostprogrammatic.New(coordinator, providerCatalog, agentCore.State, agentCore.History, delivery)
	controller := controllerprogrammatic.New(ctx, session)
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	programmaticv1.RegisterProgrammaticControlServiceServer(server, controller)

	socketService, err := programmaticsocket.New(ctx, command.SocketPath)
	if err != nil {
		return fmt.Errorf("open Programmatic Control socket: %w", err)
	}
	defer func() {
		closeTools()
		returnErr = errors.Join(returnErr, socketService.Close())
	}()
	if err = json.NewEncoder(stdout).Encode(struct {
		Socket string `json:"socket"`
	}{Socket: socketService.Path()}); err != nil {
		return fmt.Errorf("write Programmatic Control socket announcement: %w", err)
	}

	return runProgrammaticServer(ctx, server, socketService, controller.Completions(), session)
}

// runProgrammaticServer owns server execution and Host-session shutdown.
func runProgrammaticServer(
	ctx context.Context,
	server *grpc.Server,
	socketService *programmaticsocket.Service,
	completions <-chan controllerprogrammatic.SessionCompletion,
	session *hostprogrammatic.Service,
) error {
	serveResults := make(chan error, 1)
	go func() {
		serveResults <- server.Serve(socketService.Listener)
	}()

	var result error
	serveResultRead := false
	select {
	case completion := <-completions:
		result = completion.Err
		if completion.CleanupErr != nil {
			result = errors.Join(result, completion.CleanupErr)
		}
	case serveErr := <-serveResults:
		serveResultRead = true
		if serveErr != nil {
			result = fmt.Errorf("serve Programmatic Control: %w", serveErr)
		}
	case <-ctx.Done():
		result = ctx.Err()
	}

	// Cancellation can become ready together with another terminal result.
	if ctx.Err() != nil {
		result = ctx.Err()
	}
	cleanupErr := session.CancelAndWait(context.WithoutCancel(ctx))
	server.Stop()
	if !serveResultRead {
		serveErr := <-serveResults
		if serveErr != nil && ctx.Err() == nil {
			result = errors.Join(result, fmt.Errorf("serve Programmatic Control: %w", serveErr))
		}
	}
	if ctx.Err() != nil {
		if cleanupErr != nil {
			return errors.Join(ctx.Err(), cleanupErr)
		}
		return ctx.Err()
	}
	if cleanupErr != nil {
		return errors.Join(result, cleanupErr)
	}
	return result
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
	providerCatalog, err := newProviderCatalog(configured, paths, interactions.New(), hookRunner)
	if err != nil {
		return fmt.Errorf("create provider catalog: %w", err)
	}
	dispatcher := events.NewDispatcher(renderer.DeliverAgent, renderer.DeliverSettled)
	agentCore := agentrun.New(codingagent.Instructions(), providerCatalog, hookRunner, tools, dispatcher)
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
		ActiveUI:   configured.ActiveUI.OrEmpty(),
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
	interaction := interactions.NewUI(delivery.PresentAuthorizationURL, browser.New())
	providerCatalog, err := newProviderCatalog(configured, paths, interaction, hookRunner)
	if err != nil {
		selection.Runtime.Close()
		recoveryErr := recovery.Restore()
		tools.Close()
		return errors.Join(fmt.Errorf("create provider catalog: %w", err), recoveryErr)
	}
	dispatcher := events.NewDispatcher(delivery.DeliverAgent, delivery.DeliverSettled)
	agentCore := agentrun.New(codingagent.Instructions(), providerCatalog, hookRunner, tools, dispatcher)
	coordinator := events.NewCoordinator(agentCore.Run, agentCore.Settle, dispatcher)
	session := hostui.NewSession(
		channel,
		coordinator,
		providerCatalog,
		providerCatalog,
		func(activationContext context.Context) {
			selectionWarningsDelivered = true
			tools.Activate(activationContext)
		},
	)
	controller := controllerui.New(session)
	initialization := hostui.BuildInitialization(selection.ID, report, selection.Issues, providerCatalog)
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

// newProviderCatalog maps validated startup settings into the runtime catalog.
func newProviderCatalog(
	configured settingstore.Settings,
	paths persistence.Paths,
	interaction codex.Interaction,
	hookRunner internalhooks.ProviderRunner,
) (*providers.Catalog, error) {
	providerIDs := slices.Collect(maps.Keys(configured.Providers))
	slices.Sort(providerIDs)

	entries := make([]providers.Entry, 0)
	for _, providerID := range providerIDs {
		providerConfig := configured.Providers[providerID]
		switch providerConfig.Type {
		case settingstore.ProviderTypeOpenAICodex:
			credentials := credentialstore.New(paths.CredentialsFile, codex.ProviderID)
			modelIDs := make([]model.ID, len(providerConfig.Models))
			compatibilityKeys := make(map[model.ID]mo.Option[string], len(providerConfig.Models))
			for index, configuredModel := range providerConfig.Models {
				modelID := model.ID(configuredModel.ID)
				modelIDs[index] = modelID
				compatibilityKeys[modelID] = configuredModel.Reasoning.CompatibilityKey
			}
			provider := codex.New(codex.Config{
				Hooks: hookRunner, Models: modelIDs, ReasoningCompatibilityKeys: compatibilityKeys,
			}, credentials, interaction)
			for _, configuredModel := range providerConfig.Models {
				descriptor := codex.ModelDescriptor(model.ID(configuredModel.ID))
				descriptor.ReasoningCapabilities = reasoningCapabilities(configuredModel.Reasoning)
				entries = append(entries, providers.Entry{
					Descriptor: descriptor, Provider: provider,
					SelectionCredentialValidator: nil, Authentication: provider,
				})
			}
		case settingstore.ProviderTypeOpenAICompatible:
			resolver := credentialstore.NewAPIKeyResolver(
				paths.CredentialsFile, apiKeySource(providerConfig.APIKey),
			)
			modelAPIs := make(map[model.ID]compatible.API, len(providerConfig.Models))
			wireFormats := make(map[model.ID]string, len(providerConfig.Models))
			compatibilityKeys := make(map[model.ID]mo.Option[string], len(providerConfig.Models))
			for _, configuredModel := range providerConfig.Models {
				modelID := model.ID(configuredModel.ID)
				modelAPIs[modelID] = compatible.API(configuredModel.API)
				wireFormats[modelID] = string(configuredModel.Reasoning.WireFormat)
				compatibilityKeys[modelID] = configuredModel.Reasoning.CompatibilityKey
			}
			provider, err := compatible.New(compatible.Config{
				ProviderID: model.ProviderID(providerID), BaseURL: providerConfig.BaseURL,
				API: compatible.API(providerConfig.API), Models: modelAPIs,
				ReasoningWireFormats: wireFormats, ReasoningCompatibilityKeys: compatibilityKeys, APIKey: resolver,
			})
			if err != nil {
				return nil, fmt.Errorf("create provider %q: %w", providerID, err)
			}
			for _, configuredModel := range providerConfig.Models {
				entries = append(entries, providers.Entry{
					Descriptor: model.Descriptor{
						Provider: model.ProviderID(providerID), Model: model.ID(configuredModel.ID),
						ReasoningCapabilities: reasoningCapabilities(configuredModel.Reasoning),
						ToolCapabilities: model.ToolCapabilities{
							StrictJSONSchema: false,
							Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
						},
					},
					Provider: provider, SelectionCredentialValidator: resolver, Authentication: nil,
				})
			}
		default:
			return nil, fmt.Errorf("unsupported configured provider type %q", providerConfig.Type)
		}
	}
	defaultProvider := configured.Providers[configured.DefaultProvider]
	defaultModel := settingstore.Model{
		ID: "", API: "", Reasoning: settingstore.Reasoning{
			Supported: false, Choices: nil, Default: "", CompatibilityKey: mo.None[string](), WireFormat: "",
		},
	}
	defaultModelIndex := slices.IndexFunc(defaultProvider.Models, func(configuredModel settingstore.Model) bool {
		return configuredModel.ID == configured.DefaultModel
	})
	if defaultModelIndex >= 0 {
		defaultModel = defaultProvider.Models[defaultModelIndex]
	}
	return providers.New(entries, model.Selection{
		Provider: model.ProviderID(configured.DefaultProvider), Model: model.ID(configured.DefaultModel),
		ReasoningChoice: model.ReasoningChoice(defaultModel.Reasoning.Default),
	})
}

// configuredModel finds one validated model without exposing persistence details outside assembly.
// reasoningCapabilities maps validated persistence values into the model domain.
func reasoningCapabilities(configured settingstore.Reasoning) model.ReasoningCapabilities {
	choices := lo.Map(configured.Choices, func(choice settingstore.ReasoningChoice, _ int) model.ReasoningChoice {
		return model.ReasoningChoice(choice)
	})
	return model.ReasoningCapabilities{
		Supported: configured.Supported, Choices: choices, Default: model.ReasoningChoice(configured.Default),
	}
}

// apiKeySource maps the validated settings union without resolving its secret.
func apiKeySource(configured mo.Option[settingstore.APIKey]) credentialstore.APIKeySource {
	apiKey, present := configured.Get()
	if !present {
		return credentialstore.APIKeySource{Kind: "", Value: ""}
	}
	if literal, selected := apiKey.Literal.Get(); selected {
		return credentialstore.APIKeySource{
			Kind: credentialstore.APIKeySourceLiteral, Value: literal,
		}
	}
	if environment, selected := apiKey.Environment.Get(); selected {
		return credentialstore.APIKeySource{
			Kind: credentialstore.APIKeySourceEnvironment, Value: environment,
		}
	}
	return credentialstore.APIKeySource{
		Kind: credentialstore.APIKeySourceCredential, Value: apiKey.Credential.OrEmpty(),
	}
}
