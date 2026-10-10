package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/samber/mo"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/errtree"
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

// contextRequest contains one validated operation selector and its public reference.
type contextRequest struct {
	// reference identifies the issued binding.
	reference *extensionpb.ExtensionContextRef
	// prepare binds the one selected executable action.
	prepare prepareContextAction
	// admission identifies required gate or parent ownership.
	admission contextAdmission
}

// mapContextRequest validates the selected request payload before admission.
func mapContextRequest(request *extensionpb.ExtensionRequest) (contextRequest, error) {
	// mapped contains the selected request payload and execution binding.
	mapped := contextRequest{reference: nil, prepare: nil, admission: contextAdmissionRegular}
	if request == nil {
		return mapped, errors.New("extension context request is required")
	}
	selection, reference, selected, err := mapSelectionContextRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	if selected {
		mapped.reference = reference
		mapped.prepare = func(o *contextOperation) (contextAction, error) { return o.prepareSelectionAction(selection) }
		return mapped, nil
	}
	switch request.WhichRequest() {
	case extensionpb.ExtensionRequest_GetModels_case:
		mapped.reference = request.GetGetModels().GetContext()
		mapped.prepare = func(o *contextOperation) (contextAction, error) {
			return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
				return o.runModelCatalog(ctx)
			}), nil
		}
	case extensionpb.ExtensionRequest_GetProviders_case:
		mapped.reference = request.GetGetProviders().GetContext()
		mapped.prepare = func(o *contextOperation) (contextAction, error) {
			return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
				return o.runProviderCatalog(ctx)
			}), nil
		}
	case extensionpb.ExtensionRequest_ConfiguredModel_case:
		return mapConfiguredContextRequest(mapped, request.GetConfiguredModel())
	case extensionpb.ExtensionRequest_AppendExtension_case:
		return mapAppendContextRequest(mapped, request.GetAppendExtension())
	case extensionpb.ExtensionRequest_GetSessionState_case:
		mapped.reference = request.GetGetSessionState().GetContext()
		mapped.prepare = func(o *contextOperation) (contextAction, error) {
			return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
				return o.runSessionState(ctx)
			}), nil
		}
	case extensionpb.ExtensionRequest_AppendExtensionMessage_case:
		return mapAppendMessageContextRequest(mapped, request.GetAppendExtensionMessage())
	case extensionpb.ExtensionRequest_ProjectCompaction_case:
		return mapPolicyCandidate(mapped, request.GetProjectCompaction(), policyCandidateProjection)
	case extensionpb.ExtensionRequest_ReplaceCompaction_case:
		return mapPolicyCandidate(mapped, request.GetReplaceCompaction(), policyCandidateReplacement)
	case extensionpb.ExtensionRequest_Compact_case:
		// compact contains the manual compaction request.
		compact := request.GetCompact()
		mapped.reference = compact.GetContext()
		// instructions contains optional user guidance for manual compaction.
		instructions := mo.TupleToOption(compact.GetInstructions(), compact.HasInstructions())
		mapped.admission = contextAdmissionManual
		mapped.prepare = func(o *contextOperation) (contextAction, error) {
			return contextProgressAction(
				func(ctx context.Context, reporter *extensionsdk.HostProgressReporter) (*extensionpb.HostCompleted, error) {
					return o.runManualCompaction(ctx, reporter, instructions)
				},
			), nil
		}
	case extensionpb.ExtensionRequest_Request_not_set_case, extensionpb.ExtensionRequest_Cancel_case:
		return contextRequest{}, errors.New("extension context operation request is required")
	case extensionpb.ExtensionRequest_SelectModel_case, extensionpb.ExtensionRequest_SelectReasoning_case:
		return contextRequest{}, errors.New("selection context request mapping did not complete")
	default:
		return contextRequest{}, errors.New("unsupported extension context request")
	}
	return mapped, nil
}

// mapSelectionContextRequest maps either active-selection request before the general context-operation union.
func mapSelectionContextRequest(
	request *extensionpb.ExtensionRequest,
) (SelectionCommand, *extensionpb.ExtensionContextRef, bool, error) {
	switch request.WhichRequest() {
	case extensionpb.ExtensionRequest_SelectModel_case:
		command, err := mapModelSelectionRequest(request.GetSelectModel())
		return command, request.GetSelectModel().GetContext(), true, err
	case extensionpb.ExtensionRequest_SelectReasoning_case:
		command, err := mapReasoningSelectionRequest(request.GetSelectReasoning())
		return command, request.GetSelectReasoning().GetContext(), true, err
	case extensionpb.ExtensionRequest_Request_not_set_case,
		extensionpb.ExtensionRequest_GetModels_case,
		extensionpb.ExtensionRequest_GetProviders_case,
		extensionpb.ExtensionRequest_Cancel_case,
		extensionpb.ExtensionRequest_ConfiguredModel_case,
		extensionpb.ExtensionRequest_AppendExtension_case,
		extensionpb.ExtensionRequest_GetSessionState_case,
		extensionpb.ExtensionRequest_AppendExtensionMessage_case,
		extensionpb.ExtensionRequest_Compact_case,
		extensionpb.ExtensionRequest_ProjectCompaction_case, extensionpb.ExtensionRequest_ReplaceCompaction_case:
		return SelectionCommand{}, nil, false, nil
	default:
		return SelectionCommand{}, nil, false, nil
	}
}

// mapConfiguredContextRequest maps one configured-model request into the general context selector.
func mapConfiguredContextRequest(
	mapped contextRequest,
	request *extensionpb.ConfiguredModelRequest,
) (contextRequest, error) {
	configured, err := mapConfiguredRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	mapped.reference = request.GetContext()
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextProgressAction(
			func(ctx context.Context, reporter *extensionsdk.HostProgressReporter) (*extensionpb.HostCompleted, error) {
				return o.runConfiguredModel(ctx, reporter, configured)
			},
		), nil
	}
	return mapped, nil
}

// mapAppendContextRequest maps one hidden extension append into the general context selector.
func mapAppendContextRequest(
	mapped contextRequest,
	request *extensionpb.AppendExtensionRequest,
) (contextRequest, error) {
	appendValue, err := mapAppendRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	mapped.reference = request.GetContext()
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
			return o.runAppendValue(ctx, appendValue)
		}), nil
	}
	return mapped, nil
}

// mapAppendMessageContextRequest maps one visible extension message into the general context selector.
func mapAppendMessageContextRequest(
	mapped contextRequest,
	request *extensionpb.AppendExtensionMessageRequest,
) (contextRequest, error) {
	message, err := mapAppendMessageRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	mapped.reference = request.GetContext()
	mapped.prepare = func(o *contextOperation) (contextAction, error) {
		return contextReadAction(func(ctx context.Context) (*extensionpb.HostCompleted, error) {
			return o.runAppendMessage(ctx, message)
		}), nil
	}
	return mapped, nil
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
	if childRelease, present := o.childRelease.Get(); present {
		defer func() { childRelease(returnErr) }()
	}
	if parentID, present := o.parentID.Get(); present {
		// childContext links this Run to its admitted parent; unlink ends that linkage.
		childContext, unlink, err := o.service.compactionPolicy.LinkCompactionChild(
			ctx,
			o.service.extensionID,
			o.service.runtimeID,
			parentID,
		)
		if err != nil {
			return nil, extensionsdk.Fail(contextFailureCode(err), err)
		}
		defer unlink()
		ctx = childContext
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
	// releaseSelection and present identify selection admission ownership.
	if releaseSelection, present := o.selectionRelease.Get(); present {
		releaseSelection()
	}
	o.release()
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

// isPureOwningCancellation reports a no-commit failure caused only by the canceled operation owner.
func isPureOwningCancellation(ctx context.Context, err error) bool {
	if ctx.Err() == nil {
		return false
	}
	if _, typed := errors.AsType[interface {
		error
		CompactionFailureCode() string
	}](err); typed {
		return false
	}
	ownerCause := context.Cause(ctx)
	return errtree.AllLeavesMatch(err, func(cause error) bool {
		return errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) ||
			errors.Is(cause, ownerCause)
	})
}

// isPureCancellation reports whether every acquired error leaf is a cancellation sentinel.
func isPureCancellation(err error) bool {
	return errtree.AllLeavesMatch(err, func(cause error) bool {
		return errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)
	})
}
