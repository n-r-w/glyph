package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"google.golang.org/grpc"

	"github.com/n-r-w/glyph/host/internal/config/codingagent"
	"github.com/n-r-w/glyph/host/internal/controller/cli"

	controllerprogrammatic "github.com/n-r-w/glyph/host/internal/controller/programmatic"

	"github.com/n-r-w/glyph/host/internal/domain/tool"

	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"

	"github.com/n-r-w/glyph/host/internal/infra/persistence"

	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"

	programmaticsocket "github.com/n-r-w/glyph/host/internal/infra/programmatic/socket"

	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/events"
	"github.com/n-r-w/glyph/host/internal/usecase/host/interactions"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"

	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

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

	extensions := extensionservice.New(catalog.New(), extensionruntime.NewFactory(), func(
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
	sessionServices, err := newSessionComposition(ctx, paths, extensions)
	if err != nil {
		extensions.Close()
		return fmt.Errorf("initialize Host sessions: %w", err)
	}
	extensionsClosed := false
	closeExtensions := func() {
		if extensionsClosed {
			return
		}
		extensions.Close()
		extensionsClosed = true
		slog.DebugContext(context.WithoutCancel(ctx), "closed extension runtimes")
	}
	defer closeExtensions()
	startupService := startup.New(extensions.Load)
	if _, err = startupService.Load(ctx, startup.Request{
		DataDirectory: paths.Directory, ExtensionDirectory: command.ExtensionDirectory,
	}); err != nil {
		return fmt.Errorf("start programmatic Host extensions: %w", err)
	}
	extensions.Activate(ctx)

	hookRunner := hookrunner.New(nil, nil, nil)
	providerCatalog, err := newProviderCatalog(configured, paths, interactions.New(), hookRunner)
	if err != nil {
		return fmt.Errorf("create provider catalog: %w", err)
	}
	sessionServices.pricing.Bind(providerCatalog)
	sessionServices.models.Bind(providerCatalog)
	delivery := hostprogrammatic.NewDelivery()
	dispatcher := events.NewDispatcher(delivery.DeliverAgent, delivery.DeliverSettled)
	agentCore := agentrun.New(
		codingagent.Instructions(), providerCatalog, hookRunner, extensions, dispatcher, sessionServices.active,
	)
	coordinator := events.NewCoordinator(agentCore.Run, agentCore.Settle, dispatcher, sessionServices.gate)
	session := hostprogrammatic.New(
		coordinator, providerCatalog, agentCore.State, agentCore.History, sessionServices.control, delivery,
	)
	controller := controllerprogrammatic.New(ctx, session)
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	programmaticv1.RegisterProgrammaticControlServiceServer(server, controller)

	socketService, err := programmaticsocket.New(ctx, command.SocketPath)
	if err != nil {
		return fmt.Errorf("open Programmatic Control socket: %w", err)
	}
	defer func() {
		closeExtensions()
		returnErr = errors.Join(returnErr, socketService.Close())
	}()
	if err = json.MarshalWrite(stdout, struct {
		Socket string `json:"socket"`
	}{Socket: socketService.Path()}); err != nil {
		return fmt.Errorf("write Programmatic Control socket announcement: %w", err)
	}
	if _, err = io.WriteString(stdout, "\n"); err != nil {
		return fmt.Errorf("terminate Programmatic Control socket announcement: %w", err)
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
	completionRead := false
	serveResultRead := false
	select {
	case completion := <-completions:
		result = joinCompletionError(result, completion)
		completionRead = true
	case serveErr := <-serveResults:
		result = joinServeError(result, serveErr, false)
		serveResultRead = true
	case <-ctx.Done():
		result = joinIndependentError(result, ctx.Err())
	}

	// Collect terminal sources that became ready together with the selected result.
	result = joinIndependentError(result, ctx.Err())
	completionRead, result = collectPendingCompletion(result, completions, completionRead)
	serveResultRead, result = collectPendingServeResult(result, serveResults, serveResultRead, false)
	collector := programmaticShutdownCollector{
		completions: completions, serveResults: serveResults,
		completionRead: completionRead, serveResultRead: serveResultRead,
	}
	return collector.finish(
		result, ctx.Err(), session.CancelAndWait(context.WithoutCancel(ctx)), server.Stop,
	)
}

// programmaticShutdownCollector owns non-blocking terminal collection around explicit server Stop.
type programmaticShutdownCollector struct {
	// completions receives the controller session result.
	completions <-chan controllerprogrammatic.SessionCompletion
	// serveResults receives the programmatic server result.
	serveResults <-chan error
	// completionRead reports whether the controller result was collected.
	completionRead bool
	// serveResultRead reports whether the server result was collected.
	serveResultRead bool
}

// finish collects ready shutdown causes before and after explicit server Stop.
func (c *programmaticShutdownCollector) finish(result, contextErr, cleanupErr error, stopServer func()) error {
	result = joinIndependentError(result, cleanupErr)
	c.completionRead, result = collectPendingCompletion(result, c.completions, c.completionRead)
	c.serveResultRead, result = collectPendingServeResult(result, c.serveResults, c.serveResultRead, false)

	stopServer()
	if !c.serveResultRead {
		result = joinServeError(result, <-c.serveResults, true)
	}
	_, result = collectPendingCompletion(result, c.completions, c.completionRead)
	return joinIndependentError(result, contextErr)
}

// joinCompletionError adds the controller terminal and cleanup errors once.
func joinCompletionError(current error, completion controllerprogrammatic.SessionCompletion) error {
	current = joinIndependentError(current, completion.Err)
	return joinIndependentError(current, completion.CleanupErr)
}

// collectPendingCompletion adds one ready controller completion without blocking shutdown.
func collectPendingCompletion(
	current error,
	completions <-chan controllerprogrammatic.SessionCompletion,
	alreadyRead bool,
) (bool, error) {
	if alreadyRead {
		return true, current
	}
	select {
	case completion := <-completions:
		return true, joinCompletionError(current, completion)
	default:
		return false, current
	}
}

// collectPendingServeResult adds one ready serve result without blocking shutdown.
func collectPendingServeResult(
	current error,
	serveResults <-chan error,
	alreadyRead bool,
	stopIssued bool,
) (bool, error) {
	if alreadyRead {
		return true, current
	}
	select {
	case serveErr := <-serveResults:
		return true, joinServeError(current, serveErr, stopIssued)
	default:
		return false, current
	}
}

// joinServeError ignores only the result caused by this function's explicit server Stop.
func joinServeError(current, serveErr error, stopIssued bool) error {
	if serveErr == nil || stopIssued && errors.Is(serveErr, grpc.ErrServerStopped) {
		return current
	}
	return joinIndependentError(current, fmt.Errorf("serve Programmatic Control: %w", serveErr))
}

// joinIndependentError keeps the broader chain when either error already contains the other.
func joinIndependentError(current, candidate error) error {
	if candidate == nil {
		return current
	}
	if current == nil || errors.Is(candidate, current) {
		return candidate
	}
	if errors.Is(current, candidate) {
		return current
	}
	return errors.Join(current, candidate)
}
