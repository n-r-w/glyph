// Package main provides an external UI command built only from public Glyph packages.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	// signalsEnvironment names the directory used for process synchronization.
	signalsEnvironment = "GLYPH_EXTERNAL_SIGNALS"
	// ordinaryOperationID identifies the ordinary Host request.
	ordinaryOperationID = "ordinary"
	// rejectedOperationID identifies the rejected Host request.
	rejectedOperationID = "rejected"
	// failedOperationID identifies the failed Host request.
	failedOperationID = "failed"
	// canceledOperationID identifies the blocked Host request canceled by the UI.
	canceledOperationID = "blocked-cancel"
	// cancellationOperationID identifies the targeted cancellation request.
	cancellationOperationID = "cancel-blocked"
	// shutdownOperationID identifies the Host request active during shutdown.
	shutdownOperationID = "blocked-shutdown"
	// invalidArgumentCode identifies the expected public rejection category.
	invalidArgumentCode = "INVALID_ARGUMENT"
	// internalFailureCode identifies the expected public failure category.
	internalFailureCode = "INTERNAL"
	// rejectionText is the complete expected remote rejection text.
	rejectionText = "complete external UI rejection"
	// failureText is the complete expected remote failure text.
	failureText = "complete external UI failure"
	// signalFileMode restricts process synchronization files to their owner.
	signalFileMode = 0o600
	// gatePollInterval bounds process cleanup gate observation latency.
	gatePollInterval = 10 * time.Millisecond
	// shutdownWaitTimeout bounds the cancellation result wait after peer closure.
	shutdownWaitTimeout = time.Minute
)

// service implements the public UI SDK contract.
type service struct {
	// signals stores the process synchronization directory.
	signals string
}

// initializeOperation completes public UI initialization.
type initializeOperation struct{}

// cancellationResult carries one public cancellation Wait result.
type cancellationResult struct {
	// completed is the typed cancellation result.
	completed *operationv1.CancelCompleted
	// err is the complete public cancellation error.
	err error
}

var (
	// Compile-time assertions prove the fixture implements only public SDK interfaces.
	_ uisdk.Service             = (*service)(nil)
	_ uisdk.InitializeOperation = (*initializeOperation)(nil)
)

// main serves the external UI fixture through the public SDK.
func main() {
	uisdk.Serve(&service{signals: os.Getenv(signalsEnvironment)})
}

// PrepareInitialize admits one initialization operation.
func (*service) PrepareInitialize(
	context.Context,
	*uiv1.Initialization,
) (uisdk.InitializeOperation, error) {
	return &initializeOperation{}, nil
}

// Run exercises ordinary Host work, public errors, targeted cancellation, and shutdown joining.
func (s *service) Run(ctx context.Context, host *uisdk.Host) error {
	ordinary, err := host.Start(ctx, ordinaryOperationID, submitRequest(ordinaryOperationID))
	if err != nil {
		return fmt.Errorf("start ordinary Host request: %w", err)
	}
	completed, err := ordinary.Wait(ctx, nil)
	if err != nil {
		return fmt.Errorf("wait for ordinary Host request: %w", err)
	}
	if completed.GetSubmit() == nil {
		return errors.New("ordinary Host request returned no Submit result")
	}

	if publicErr := verifyPublicErrors(ctx, host); publicErr != nil {
		return publicErr
	}

	if cancellationErr := s.verifyTargetedCancellation(ctx, host); cancellationErr != nil {
		return cancellationErr
	}

	shutdown, err := host.Start(ctx, shutdownOperationID, submitRequest("shutdown"))
	if err != nil {
		return fmt.Errorf("start shutdown Host request: %w", err)
	}
	shutdownWaitCtx, cancelShutdownWait := context.WithTimeout(context.WithoutCancel(ctx), shutdownWaitTimeout)
	defer cancelShutdownWait()
	shutdownDone := make(chan error, 1)
	go func() {
		signal(s.signals, "shutdown-target-wait-started")
		_, waitErr := shutdown.Wait(shutdownWaitCtx, nil)
		shutdownDone <- checkCancellation(waitErr)
		signal(s.signals, "shutdown-target-wait-finished")
	}()
	signal(s.signals, "shutdown-started")
	<-ctx.Done()
	signal(s.signals, "run-cleanup-started")
	waitForSignal(s.signals, "run-cleanup-gate")
	if shutdownErr := <-shutdownDone; shutdownErr != nil {
		return fmt.Errorf("wait for shutdown Host request: %w", shutdownErr)
	}
	signal(s.signals, "run-cleanup-finished")
	return context.Cause(ctx)
}

// Close records its ordered start and completion after Service.Run cleanup.
func (s *service) Close() error {
	signal(s.signals, "service-close-started")
	signal(s.signals, "service-close-finished")
	return nil
}

// Run returns successful initialization.
func (*initializeOperation) Run(context.Context) (*uiv1.Initialized, error) {
	return new(uiv1.Initialized), nil
}

// Release frees the initialization operation, which owns no reservation.
func (*initializeOperation) Release() {}

// verifyPublicErrors checks public rejection and failure contracts from Host operations.
func verifyPublicErrors(ctx context.Context, host *uisdk.Host) error {
	rejected, err := host.Start(ctx, rejectedOperationID, submitRequest(rejectedOperationID))
	if err != nil {
		return fmt.Errorf("start rejected Host request: %w", err)
	}
	_, err = rejected.Wait(ctx, nil)
	if checkErr := checkRejection(err); checkErr != nil {
		return checkErr
	}

	failed, err := host.Start(ctx, failedOperationID, submitRequest(failedOperationID))
	if err != nil {
		return fmt.Errorf("start failed Host request: %w", err)
	}
	_, err = failed.Wait(ctx, nil)
	return checkFailure(err)
}

// checkRejection checks the concrete public rejection type, code, text, and cause.
func checkRejection(err error) error {
	rejection, ok := errors.AsType[*uisdk.RejectionError](err)
	if !ok {
		return fmt.Errorf("wait for rejected Host request: expected RejectionError: %w", err)
	}
	if rejection.Code() != invalidArgumentCode {
		return fmt.Errorf("wait for rejected Host request: unexpected code %q: %w", rejection.Code(), err)
	}
	if err.Error() != rejectionText {
		return fmt.Errorf("wait for rejected Host request: unexpected text %q: %w", err.Error(), err)
	}
	cause := errors.Unwrap(err)
	if cause == nil {
		return fmt.Errorf("wait for rejected Host request: cause is required: %w", err)
	}
	if cause.Error() != rejectionText {
		return fmt.Errorf("wait for rejected Host request: unexpected cause %q: %w", cause.Error(), err)
	}
	return nil
}

// checkFailure checks the concrete public failure type, code, text, and cause.
func checkFailure(err error) error {
	failure, ok := errors.AsType[*uisdk.FailureError](err)
	if !ok {
		return fmt.Errorf("wait for failed Host request: expected FailureError: %w", err)
	}
	if failure.Code() != internalFailureCode {
		return fmt.Errorf("wait for failed Host request: unexpected code %q: %w", failure.Code(), err)
	}
	if err.Error() != failureText {
		return fmt.Errorf("wait for failed Host request: unexpected text %q: %w", err.Error(), err)
	}
	cause := errors.Unwrap(err)
	if cause == nil {
		return fmt.Errorf("wait for failed Host request: cause is required: %w", err)
	}
	if cause.Error() != failureText {
		return fmt.Errorf("wait for failed Host request: unexpected cause %q: %w", cause.Error(), err)
	}
	return nil
}

// verifyTargetedCancellation waits concurrently for target and cancellation terminal results.
func (s *service) verifyTargetedCancellation(ctx context.Context, host *uisdk.Host) error {
	blocked, err := host.Start(ctx, canceledOperationID, submitRequest("cancel"))
	if err != nil {
		return fmt.Errorf("start blocked Host request: %w", err)
	}
	cancellation, err := host.Cancel(ctx, cancellationOperationID, canceledOperationID)
	if err != nil {
		return fmt.Errorf("cancel blocked Host request: %w", err)
	}

	targetDone := make(chan error, 1)
	go func() {
		signal(s.signals, "target-wait-started")
		_, waitErr := blocked.Wait(ctx, nil)
		targetDone <- checkCancellation(waitErr)
		signal(s.signals, "target-wait-finished")
	}()
	cancellationDone := make(chan cancellationResult, 1)
	go func() {
		signal(s.signals, "cancellation-wait-started")
		result, waitErr := cancellation.Wait(ctx)
		signal(s.signals, "cancellation-wait-finished")
		cancellationDone <- cancellationResult{completed: result, err: waitErr}
	}()

	targetErr := <-targetDone
	if targetErr != nil {
		return fmt.Errorf("wait for canceled Host request: %w", targetErr)
	}
	canceled := <-cancellationDone
	if canceled.err != nil {
		return fmt.Errorf("wait for Host cancellation: %w", canceled.err)
	}
	if canceled.completed.GetTargetState() != operationv1.TerminalState_TERMINAL_STATE_CANCELED {
		return fmt.Errorf("wait for Host cancellation: unexpected target state %s",
			canceled.completed.GetTargetState())
	}
	signal(s.signals, "cancel-joined")
	return nil
}

// checkCancellation checks the public cancellation type and context cancellation cause.
func checkCancellation(err error) error {
	if _, ok := errors.AsType[*uisdk.CanceledError](err); !ok {
		return fmt.Errorf("expected CanceledError: %w", err)
	}
	if !errors.Is(err, context.Canceled) {
		return fmt.Errorf("expected context cancellation cause: %w", err)
	}
	return nil
}

// submitRequest creates one public Submit Host request.
func submitRequest(text string) *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new(text)}.Build())
	return request
}

// signal writes one child-process synchronization marker.
func signal(directory, name string) {
	if err := os.WriteFile(filepath.Join(directory, name), nil, signalFileMode); err != nil {
		panic(err)
	}
}

// waitForSignal holds Service.Run cleanup until the root test opens its gate.
func waitForSignal(directory, name string) {
	path := filepath.Join(directory, name)
	ticker := time.NewTicker(gatePollInterval)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			panic(err)
		}
		<-ticker.C
	}
}
