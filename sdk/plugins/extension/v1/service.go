package extensionv1

import (
	"context"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// rejectionCodeInvalidArgument identifies malformed operation requests.
	rejectionCodeInvalidArgument = "INVALID_ARGUMENT"
	// rejectionCodeOperationIDInUse identifies an active duplicate identifier.
	rejectionCodeOperationIDInUse = "OPERATION_ID_IN_USE"
	// rejectionCodeBusy identifies duplicate startup admission.
	rejectionCodeBusy = "BUSY"
	// rejectionCodeNotReady identifies work requested before registration completes.
	rejectionCodeNotReady = "NOT_READY"
	// rejectionCodeTargetNotActive identifies cancellation of an inactive operation.
	rejectionCodeTargetNotActive = "TARGET_NOT_ACTIVE"
	// failureCodeInternal identifies unclassified operation failures.
	failureCodeInternal = "INTERNAL"
)

//go:generate go tool mockgen -source=service.go -destination=interfaces_mock.go -package=extensionv1

// Service prepares extension-owned operations for asynchronous execution.
type Service interface {
	// PrepareRegister validates and admits startup registration.
	PrepareRegister(context.Context, *extensionpb.RegisterRequest) (RegisterOperation, error)
	// PrepareHandle validates and admits one handler invocation.
	PrepareHandle(context.Context, *extensionpb.HandleRequest) (HandleOperation, error)
	// PrepareExecute validates and admits one tool execution.
	PrepareExecute(context.Context, *extensionpb.ExecuteRequest) (ExecuteOperation, error)
}

// RegisterOperation owns one admitted registration operation.
type RegisterOperation interface {
	// Run returns the complete extension registration.
	Run(context.Context) (*extensionpb.RegisterResponse, error)
	// Release frees the admission reservation.
	Release()
}

// HandleOperation owns one admitted handler operation.
type HandleOperation interface {
	// Run returns the handler action or ordinary HandlerError data.
	Run(context.Context) (*extensionpb.HandleResponse, error)
	// Release frees the admission reservation.
	Release()
}

// ExecuteOperation owns one admitted tool operation.
type ExecuteOperation interface {
	// Run executes the tool and reports ordered progress.
	Run(context.Context, *ProgressReporter) (*extensionpb.ToolResult, error)
	// Release frees the admission reservation.
	Release()
}

// ProgressReporter delivers tool progress through the operation stream.
type ProgressReporter struct {
	// reporter is the transport-neutral progress reporter.
	reporter operation.Reporter[*extensionpb.ToolProgress]
}

// Report queues one progress event without blocking stream receipt.
func (r *ProgressReporter) Report(ctx context.Context, progress *extensionpb.ToolProgress) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("report extension progress: %w", err)
	}
	if progress == nil {
		return errors.New("report extension progress: progress is required")
	}
	if err := validateToolProgress(progress); err != nil {
		return fmt.Errorf("report extension progress: %w", err)
	}
	if err := r.reporter.Report(progress); err != nil {
		return fmt.Errorf("report extension progress: %w", err)
	}
	return nil
}

// extensionResult contains one completed operation payload.
type extensionResult struct {
	// completed is the operation-specific completed payload.
	completed *extensionpb.ExtensionCompleted
}

// registerPrepared adapts public registration work to the shared owner.
type registerPrepared struct {
	// operation owns the plugin registration work.
	operation RegisterOperation
}

var _ operation.Prepared[*extensionpb.ToolProgress, extensionResult] = (*registerPrepared)(nil)

// Run executes registration and maps its terminal result.
func (prepared *registerPrepared) Run(
	ctx context.Context,
	_ operation.Reporter[*extensionpb.ToolProgress],
) operation.Outcome[extensionResult] {
	response, err := prepared.operation.Run(ctx)
	if err != nil {
		return operationOutcome[extensionResult](err)
	}
	if response == nil {
		return operation.Failed[extensionResult](failureCodeInternal, errors.New("registration result is required"))
	}
	completed := new(extensionpb.ExtensionCompleted)
	completed.SetRegister(response)
	return operation.Completed(extensionResult{completed: completed})
}

// Release frees the registration admission reservation.
func (prepared *registerPrepared) Release() { prepared.operation.Release() }

// handlePrepared adapts public handler work to the shared owner.
type handlePrepared struct {
	// operation owns the plugin handler work.
	operation HandleOperation
}

var _ operation.Prepared[*extensionpb.ToolProgress, extensionResult] = (*handlePrepared)(nil)

// Run executes a handler and maps its terminal result.
func (prepared *handlePrepared) Run(
	ctx context.Context,
	_ operation.Reporter[*extensionpb.ToolProgress],
) operation.Outcome[extensionResult] {
	response, err := prepared.operation.Run(ctx)
	if err != nil {
		return operationOutcome[extensionResult](err)
	}
	if response == nil {
		return operation.Failed[extensionResult](failureCodeInternal, errors.New("handler result is required"))
	}
	completed := new(extensionpb.ExtensionCompleted)
	completed.SetHandle(response)
	return operation.Completed(extensionResult{completed: completed})
}

// Release frees the handler admission reservation.
func (prepared *handlePrepared) Release() { prepared.operation.Release() }

// executePrepared adapts public tool work to the shared owner.
type executePrepared struct {
	// operation owns the plugin tool work.
	operation ExecuteOperation
}

var _ operation.Prepared[*extensionpb.ToolProgress, extensionResult] = (*executePrepared)(nil)

// Run executes a tool and maps its terminal result.
func (prepared *executePrepared) Run(
	ctx context.Context,
	reporter operation.Reporter[*extensionpb.ToolProgress],
) operation.Outcome[extensionResult] {
	response, err := prepared.operation.Run(ctx, &ProgressReporter{reporter: reporter})
	if err != nil {
		return operationOutcome[extensionResult](err)
	}
	if response == nil {
		return operation.Failed[extensionResult](failureCodeInternal, errors.New("tool result is required"))
	}
	completed := new(extensionpb.ExtensionCompleted)
	completed.SetTool(response)
	return operation.Completed(extensionResult{completed: completed})
}

// Release frees the tool admission reservation.
func (prepared *executePrepared) Release() { prepared.operation.Release() }

// cancellationPrepared owns one accepted cancellation operation.
type cancellationPrepared struct {
	// cancel requests target cancellation and waits for its terminal state.
	cancel func(context.Context) (operation.TerminalState, error)
}

var _ operation.Prepared[*extensionpb.ToolProgress, extensionResult] = (*cancellationPrepared)(nil)

// Run cancels the target and returns its actual terminal state.
func (prepared *cancellationPrepared) Run(
	ctx context.Context,
	_ operation.Reporter[*extensionpb.ToolProgress],
) operation.Outcome[extensionResult] {
	state, err := prepared.cancel(ctx)
	if err != nil {
		return operationOutcome[extensionResult](err)
	}
	var targetState operationv1.TerminalState
	switch state {
	case operation.TerminalStateCompleted:
		targetState = operationv1.TerminalState_TERMINAL_STATE_COMPLETED
	case operation.TerminalStateCanceled:
		targetState = operationv1.TerminalState_TERMINAL_STATE_CANCELED
	case operation.TerminalStateFailed:
		targetState = operationv1.TerminalState_TERMINAL_STATE_FAILED
	default:
		return operation.Failed[extensionResult](
			failureCodeInternal,
			errors.New("cancellation target state is invalid"),
		)
	}
	completed := new(extensionpb.ExtensionCompleted)
	completed.SetCancel(operationv1.CancelCompleted_builder{TargetState: new(targetState)}.Build())
	return operation.Completed(extensionResult{completed: completed})
}

// Release has no separate cancellation reservation to free.
func (prepared *cancellationPrepared) Release() {}

// operationOutcome maps public operation errors to shared terminal outcomes.
func operationOutcome[R any](err error) operation.Outcome[R] {
	if failure, ok := errors.AsType[*FailureError](err); ok {
		return operation.Failed[R](failure.Code(), failure)
	}
	if errors.Is(err, context.Canceled) {
		return operation.Canceled[R]()
	}
	return operation.Failed[R](failureCodeInternal, err)
}
