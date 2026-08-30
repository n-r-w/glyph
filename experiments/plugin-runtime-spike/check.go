package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// automatedReport records observable outcomes from the non-terminal runtime checks.
type automatedReport struct {
	protocolVersion    bool
	multipleExtensions bool
	streaming          bool
	cancellation       bool
	crashIsolation     bool
	collisionCleanup   bool
	probeWithoutUI     bool
	bidirectionalUI    bool
	uiStreamCompletion bool
}

// passed reports whether every non-terminal process contract was observed.
func (r automatedReport) passed() bool {
	return r.protocolVersion &&
		r.multipleExtensions &&
		r.streaming &&
		r.cancellation &&
		r.crashIsolation &&
		r.collisionCleanup &&
		r.probeWithoutUI &&
		r.bidirectionalUI &&
		r.uiStreamCompletion
}

// runAutomatedChecks verifies process contracts that do not require a controlling terminal.
func runAutomatedChecks(ctx context.Context, executable string) (automatedReport, error) {
	var report automatedReport
	if err := checkExtensionRuntimes(ctx, executable, &report); err != nil {
		return report, err
	}
	if err := checkUIRuntime(ctx, executable, &report); err != nil {
		return report, err
	}
	return report, nil
}

// checkExtensionRuntimes verifies versioning, streaming, cancellation, isolation, and conflicts.
func checkExtensionRuntimes(
	ctx context.Context,
	executable string,
	report *automatedReport,
) error {
	alpha, err := startExtension(ctx, executable, "alpha", "read")
	if err != nil {
		return err
	}
	defer alpha.close()
	beta, err := startExtension(ctx, executable, "beta", "bash")
	if err != nil {
		return err
	}
	defer beta.close()

	alpha.tools, err = alpha.client.listTools(ctx)
	if err != nil {
		return err
	}
	beta.tools, err = beta.client.listTools(ctx)
	if err != nil {
		return err
	}

	registry := newToolRegistry()
	if removed := registry.add(alpha); len(removed) != 0 {
		return fmt.Errorf("register alpha extension: unexpected collision")
	}
	if removed := registry.add(beta); len(removed) != 0 {
		return fmt.Errorf("register beta extension: unexpected collision")
	}

	report.protocolVersion = alpha.version == protocolVersion && beta.version == protocolVersion
	report.multipleExtensions = !alpha.exited() &&
		!beta.exited() &&
		alpha.process.ID() != beta.process.ID()

	progress, result, err := executeAndCollect(ctx, alpha, "read", "file.txt")
	if err != nil {
		return err
	}
	report.streaming = len(progress) == 1 &&
		progress[0] == "alpha:read:started" &&
		result.content == "alpha:read:file.txt" &&
		!result.isError

	cancelCtx, cancel := context.WithCancel(ctx)
	stream, err := beta.client.execute(cancelCtx, "bash", executionInputWait)
	if err != nil {
		cancel()
		return err
	}
	progressEvent, err := stream.recv()
	if err != nil {
		cancel()
		return fmt.Errorf("receive cancellable progress: %w", err)
	}
	cancel()
	_, cancelErr := stream.recv()
	report.cancellation = progressEvent.progress == "beta:bash:started" &&
		status.Code(cancelErr) == codes.Canceled

	crashStream, err := alpha.client.execute(ctx, "read", executionInputCrash)
	if err != nil {
		return err
	}
	_, crashErr := crashStream.recv()
	if crashErr == nil {
		return fmt.Errorf("crash extension: expected stream error")
	}
	if err := waitForExit(ctx, alpha.exited); err != nil {
		return err
	}
	registry.remove(alpha)
	lateResult, err := registry.execute(ctx, "read", "late")
	if err != nil {
		return err
	}
	betaResult, err := registry.execute(ctx, "bash", "still-alive")
	if err != nil {
		return err
	}
	report.crashIsolation = alpha.exited() &&
		lateResult.isError &&
		betaResult.content == "beta:bash:still-alive" &&
		!betaResult.isError

	collisionA, err := startExtension(ctx, executable, "collision-a", "shared")
	if err != nil {
		return err
	}
	defer collisionA.close()
	collisionB, err := startExtension(ctx, executable, "collision-b", "shared")
	if err != nil {
		return err
	}
	defer collisionB.close()
	collisionA.tools, err = collisionA.client.listTools(ctx)
	if err != nil {
		return err
	}
	collisionB.tools, err = collisionB.client.listTools(ctx)
	if err != nil {
		return err
	}
	if removed := registry.add(collisionA); len(removed) != 0 {
		return fmt.Errorf("register first conflicting extension: premature collision")
	}
	removed := registry.add(collisionB)
	sharedResult, err := registry.execute(ctx, "shared", "late")
	if err != nil {
		return err
	}
	betaAfterCollision, err := registry.execute(ctx, "bash", "after-collision")
	if err != nil {
		return err
	}
	report.collisionCleanup = len(removed) == 2 &&
		collisionA.exited() &&
		collisionB.exited() &&
		sharedResult.isError &&
		betaAfterCollision.content == "beta:bash:after-collision" &&
		!betaAfterCollision.isError
	return nil
}

// executeAndCollect opens and consumes one complete extension execution.
func executeAndCollect(
	ctx context.Context,
	runtime *extensionRuntime,
	toolName string,
	input string,
) ([]string, toolOutcome, error) {
	stream, err := runtime.client.execute(ctx, toolName, input)
	if err != nil {
		return nil, toolOutcome{}, err
	}
	return stream.collectExecution()
}

// checkUIRuntime verifies handshake-only probing and the persistent bidirectional lifecycle stream.
func checkUIRuntime(ctx context.Context, executable string, report *automatedReport) error {
	probe, err := startUI(ctx, executable, false)
	if err != nil {
		return err
	}
	probe.close()
	report.probeWithoutUI = probe.version == protocolVersion &&
		!probe.usesTerminal &&
		probe.exited()

	active, err := startUI(ctx, executable, false)
	if err != nil {
		return err
	}
	defer active.close()
	stream, err := active.client.open(ctx)
	if err != nil {
		return err
	}
	if err := stream.expectEvent(uiEventReady, ""); err != nil {
		return err
	}
	if err := stream.send(uiCommandEcho); err != nil {
		return err
	}
	if err := stream.expectEvent(uiEventEchoed, "echo"); err != nil {
		return err
	}
	report.bidirectionalUI = true

	if err := stream.send(uiCommandQuit); err != nil {
		return err
	}
	if err := stream.expectEvent(uiEventExited, ""); err != nil {
		return err
	}
	_, streamErr := stream.recv()
	report.uiStreamCompletion = errors.Is(streamErr, io.EOF)
	return nil
}
