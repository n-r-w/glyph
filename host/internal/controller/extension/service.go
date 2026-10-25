package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/samber/mo"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/internal/errtree"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// invalidArgumentCode identifies malformed context references or request payloads.
	invalidArgumentCode = "INVALID_ARGUMENT"
	// internalFailureCode identifies unclassified Host failures.
	internalFailureCode = "INTERNAL"
	// deliveryFailedIssueCode identifies post-commit client publication failure.
	deliveryFailedIssueCode = "DELIVERY_FAILED"
	// compactionFailedCode identifies an unclassified compaction failure.
	compactionFailedCode = "COMPACTION_FAILED"
	// compactionRunningStage identifies active compaction work.
	compactionRunningStage = "running"
)

// Service maps requests from one connected runtime to Host context operations.
type Service struct {
	// models owns extension-facing catalog reads and configured model requests.
	models ModelOperations
	// contexts owns issued binding validation and session capabilities.
	contexts ContextOperations
	// runtime owns active-operation accounting for the connected process.
	runtime RuntimeOperations
	// selection owns shared active-selection admission and execution.
	selection ModelSelection
	// compaction owns session-bound manual compaction.
	compaction CompactionOperations
	// compactionGate owns admission against active agent and session operations.
	compactionGate CompactionGate
	// compactionPolicy supplies scoped projection, persistence, and nested lifetimes.
	compactionPolicy CompactionPolicyOperations
	// extensionID comes from process discovery, never request payloads.
	extensionID string
	// runtimeID identifies the exact process incarnation connected to this controller.
	runtimeID string
}

var _ extensionsdk.HostService = (*Service)(nil)

// New binds dispatch to the extension and runtime identity supplied by process composition.
func New(
	models ModelOperations,
	contexts ContextOperations,
	runtime RuntimeOperations,
	extensionID, runtimeID string,
) *Service {
	return &Service{
		models:           models,
		contexts:         contexts,
		runtime:          runtime,
		selection:        nil,
		compaction:       nil,
		compactionGate:   nil,
		compactionPolicy: nil,
		extensionID:      extensionID,
		runtimeID:        runtimeID,
	}
}

// BindSelection binds shared selection before this controller becomes available to its stream.
func (s *Service) BindSelection(selection ModelSelection) {
	s.selection = selection
}

// BindCompaction binds session-bound manual compaction before this controller becomes available.
func (s *Service) BindCompaction(compaction CompactionOperations, gate CompactionGate) {
	s.compaction = compaction
	s.compactionGate = gate
}

// BindPolicyOperations supplies operation-scoped compaction services.
func (s *Service) BindPolicyOperations(policy CompactionPolicyOperations) {
	s.compactionPolicy = policy
}

// contextOperation maps one admitted context request and owns its accounting release.
type contextOperation struct {
	// id identifies the extension-initiated operation in diagnostics.
	id string
	// service supplies the connected runtime identity and capability interface.
	service *Service
	// reference retains the exact admitted context reference.
	reference extensiondomain.ContextRef
	// action executes the selected request with explicit Run context.
	action contextAction
	// selectionRelease owns selection-specific admission when present.
	selectionRelease mo.Option[func()]
	// invocation owns the accepted child independently of compaction mutation authority.
	invocation InvocationChild
	// releaseOnce protects every accepted-operation cleanup path.
	releaseOnce sync.Once
	// parentID identifies the admitted parent lifetime.
	parentID mo.Option[string]
	// childRelease reports nested completion to the parent.
	childRelease mo.Option[func(error)]
	// release returns the runtime operation reservation.
	release func()
}

var _ extensionsdk.HostOperation = (*contextOperation)(nil)

// Run executes the typed read and preserves every added error cause.
func (o *contextOperation) Run(
	ctx context.Context,
	reporter *extensionsdk.HostProgressReporter,
) (completed *extensionpb.HostCompleted, returnErr error) {
	linked, unlinkInvocation := o.invocation.Link(ctx)
	defer unlinkInvocation()
	ctx = linked
	if childRelease, present := o.childRelease.Get(); present {
		defer func() { childRelease(returnErr) }()
	}
	if parentID, present := o.parentID.Get(); present {
		childContext, unlink := o.service.compactionPolicy.LinkCompactionChild(ctx, parentID)
		defer unlink()
		ctx = childContext
	}

	defer func() { o.invocation.Fail(qualifyingExtensionFailure(returnErr)) }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slog.DebugContext(
		ctx,
		"execute extension context operation",
		"operation_id",
		o.id,
		"extension_id",
		o.service.extensionID,
		"runtime_instance_id",
		o.service.runtimeID,
		"session_id",
		o.reference.SessionID,
	)
	return o.action.Run(ctx, reporter)
}

// Release returns the reservation to the runtime accounting owner.
func (o *contextOperation) Release() {
	o.releaseOnce.Do(func() {
		if releaseSelection, present := o.selectionRelease.Get(); present {
			releaseSelection()
		}
		o.release()
		o.invocation.Release()
	})
}

// mapCompactionCompletion maps one committed Host result into the Extension Contract.
func mapCompactionCompletion(
	ctx context.Context,
	result CompactionResult,
	compactionErr error,
) (*extensionpb.CompactResult, error) {
	if compactionErr != nil && result.Committed.IsNone() {
		if isPureOwningCancellation(ctx, compactionErr) {
			return nil, compactionErr
		}
		return nil, extensionsdk.Fail(compactionFailureCode(compactionErr), compactionErr)
	}
	builder := extensionpb.CompactResult_builder{
		Committed: nil, Canceled: new(result.Canceled), Error: nil, FailureCode: nil,
	}
	if committed, present := result.Committed.Get(); present {
		mapped, err := mapCommittedCompaction(committed)
		if err != nil {
			return nil, extensionsdk.Fail(internalFailureCode, errors.Join(compactionErr, err))
		}
		builder.Committed = mapped
	}
	if compactionErr != nil {
		builder.Error = new(compactionErr.Error())
		builder.FailureCode = new(compactionFailureCode(compactionErr))
		if !isPureOwningCancellation(ctx, compactionErr) {
			return builder.Build(), extensionsdk.Fail(compactionFailureCode(compactionErr), compactionErr)
		}
	}
	return builder.Build(), nil
}

// compactionFailureCode preserves typed compaction categories through the public contract.
func compactionFailureCode(err error) string {
	failure, found := errors.AsType[interface {
		error
		CompactionFailureCode() string
	}](err)
	if found {
		return failure.CompactionFailureCode()
	}
	return compactionFailedCode
}

// mapModelFailure preserves source identity before reducing pure unclassified cancellation.
func mapModelFailure(action string, err error) error {
	cause := fmt.Errorf("%s: %w", action, err)
	if failure, found := errors.AsType[ModelFailure](err); found {
		return extensionsdk.Fail(failure.ModelCode(), cause)
	}
	if failure, found := errors.AsType[ContextFailure](err); found {
		return extensionsdk.Fail(failure.ContextCode(), cause)
	}
	if isPureCancellation(err) {
		return cause
	}
	return extensionsdk.Fail(internalFailureCode, cause)
}

// contextFailureCode extracts the closed owner category without replacing the error text.
func contextFailureCode(err error) string {
	if failure, found := errors.AsType[ContextFailure](err); found {
		return failure.ContextCode()
	}
	return internalFailureCode
}

// mapContextFailure preserves context identity before reducing pure unclassified cancellation.
func mapContextFailure(action string, err error) error {
	cause := fmt.Errorf("%s: %w", action, err)
	if failure, found := errors.AsType[ContextFailure](err); found {
		return extensionsdk.Fail(failure.ContextCode(), cause)
	}
	if isPureCancellation(err) {
		return cause
	}
	return extensionsdk.Fail(internalFailureCode, cause)
}

// isPureOwningCancellation reports an error caused only by cancellation of the operation owner.
func isPureOwningCancellation(ctx context.Context, err error) bool {
	if ctx.Err() == nil {
		return false
	}
	ownerCause := context.Cause(ctx)
	return errtree.IsCurrentCancellation(err, func(cause error) bool {
		return errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) ||
			errors.Is(cause, ownerCause)
	})
}

// isPureCancellation checks current causes for caller cancellation markers.
func isPureCancellation(err error) bool {
	return errtree.IsCurrentCancellation(err, func(cause error) bool {
		return errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)
	})
}
