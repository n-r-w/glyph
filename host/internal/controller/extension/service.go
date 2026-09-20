package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

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
		models:         models,
		contexts:       contexts,
		runtime:        runtime,
		selection:      nil,
		compaction:     nil,
		compactionGate: nil,
		extensionID:    extensionID,
		runtimeID:      runtimeID,
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

// Prepare validates the binding and reserves runtime accounting before acceptance.
func (s *Service) Prepare(
	ctx context.Context,
	operationID string,
	request *extensionpb.ExtensionRequest,
) (extensionsdk.HostOperation, error) {
	mappedRequest, err := mapContextRequest(request)
	if err != nil {
		return nil, extensionsdk.Reject(invalidArgumentCode, err)
	}
	reference := mappedRequest.reference
	if reference == nil || reference.GetContextId() == "" || reference.GetRuntimeInstanceId() == "" ||
		reference.GetSessionId() == "" {
		return nil, extensionsdk.Reject(
			invalidArgumentCode,
			errors.New("complete extension context reference is required"),
		)
	}
	bound := extensiondomain.ContextRef{
		ID:                reference.GetContextId(),
		RuntimeInstanceID: reference.GetRuntimeInstanceId(),
		SessionID:         reference.GetSessionId(),
	}
	if validationErr := s.contexts.ValidateContext(s.extensionID, s.runtimeID, bound); validationErr != nil {
		return nil, extensionsdk.Reject(contextFailureCode(validationErr), validationErr)
	}
	release, err := s.runtime.BeginContextOperation(ctx, s.extensionID, s.runtimeID)
	if err != nil {
		return nil, extensionsdk.Reject(contextFailureCode(err), err)
	}
	if mappedRequest.compactionInstructions.IsSome() {
		if s.compaction == nil || s.compactionGate == nil {
			release()
			return nil, extensionsdk.Reject(internalFailureCode, errors.New("manual compaction is not bound"))
		}
		gateRelease, acquired := s.compactionGate.TryAcquire()
		if !acquired {
			release()
			return nil, extensionsdk.Reject("BUSY", errors.New("another operation is active"))
		}
		runtimeRelease := release
		release = sync.OnceFunc(func() {
			gateRelease()
			runtimeRelease()
		})
	}
	preparedSelection := mo.None[PreparedSelection]()
	selectionCommand, hasSelection := mappedRequest.selection.Get()
	if hasSelection {
		if s.selection == nil {
			release()
			return nil, extensionsdk.Reject(internalFailureCode, errors.New("model selection is not bound"))
		}
		selectionCommand.ExtensionID = s.extensionID
		selectionCommand.RuntimeID = s.runtimeID
		selectionCommand.Context = bound
		prepared, prepareErr := s.selection.PrepareExtensionSelection(selectionCommand)
		if prepareErr != nil {
			release()
			return nil, mapSelectionRejection(prepareErr)
		}
		preparedSelection = mo.Some(prepared)
	}
	return &contextOperation{
		service: s, reference: bound, models: mappedRequest.models, sessionState: mappedRequest.sessionState,
		configured: mappedRequest.configured, appendValue: mappedRequest.appendValue,
		appendMessage: mappedRequest.appendMessage, selection: preparedSelection,
		compactionInstructions: mappedRequest.compactionInstructions,
		release:                release, id: operationID,
	}, nil
}

// contextRequest contains one validated operation selector and its public reference.
type contextRequest struct {
	// reference identifies the issued binding.
	reference *extensionpb.ExtensionContextRef
	// models selects a model catalog instead of a provider catalog.
	models bool
	// sessionState selects active-branch recovery.
	sessionState bool
	// configured contains a configured-model request when selected.
	configured mo.Option[configuredRequest]
	// appendValue contains a hidden append when selected.
	appendValue mo.Option[appendRequest]
	// appendMessage contains a model-visible append when selected.
	appendMessage mo.Option[appendMessageRequest]
	// selection contains one active-selection command when selected.
	selection mo.Option[SelectionCommand]
	// compactionInstructions is present when manual compaction is selected.
	compactionInstructions mo.Option[mo.Option[string]]
}

// mapContextRequest validates the selected request payload before admission.
func mapContextRequest(request *extensionpb.ExtensionRequest) (contextRequest, error) {
	mapped := contextRequest{
		reference: nil, models: false, sessionState: false,
		configured: mo.None[configuredRequest](), appendValue: mo.None[appendRequest](),
		appendMessage: mo.None[appendMessageRequest](), selection: mo.None[SelectionCommand](),
		compactionInstructions: mo.None[mo.Option[string]](),
	}
	if request == nil {
		return mapped, errors.New("extension context request is required")
	}
	selection, reference, selected, err := mapSelectionContextRequest(request)
	if err != nil {
		return contextRequest{}, err
	}
	if selected {
		mapped.selection, mapped.reference = mo.Some(selection), reference
		return mapped, nil
	}
	switch request.WhichRequest() {
	case extensionpb.ExtensionRequest_GetModels_case:
		mapped.reference, mapped.models = request.GetGetModels().GetContext(), true
	case extensionpb.ExtensionRequest_GetProviders_case:
		mapped.reference = request.GetGetProviders().GetContext()
	case extensionpb.ExtensionRequest_ConfiguredModel_case:
		return mapConfiguredContextRequest(mapped, request.GetConfiguredModel())
	case extensionpb.ExtensionRequest_AppendExtension_case:
		return mapAppendContextRequest(mapped, request.GetAppendExtension())
	case extensionpb.ExtensionRequest_GetSessionState_case:
		mapped.sessionState, mapped.reference = true, request.GetGetSessionState().GetContext()
	case extensionpb.ExtensionRequest_AppendExtensionMessage_case:
		return mapAppendMessageContextRequest(mapped, request.GetAppendExtensionMessage())
	case extensionpb.ExtensionRequest_Compact_case:
		compact := request.GetCompact()
		mapped.reference = compact.GetContext()
		instructions := mo.None[string]()
		if compact.HasInstructions() {
			instructions = mo.Some(compact.GetInstructions())
		}
		mapped.compactionInstructions = mo.Some(instructions)
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
		extensionpb.ExtensionRequest_Compact_case:
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
	mapped.configured, mapped.reference = mo.Some(configured), request.GetContext()
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
	mapped.appendValue, mapped.reference = mo.Some(appendValue), request.GetContext()
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
	mapped.appendMessage, mapped.reference = mo.Some(message), request.GetContext()
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
	// models selects the model rather than provider catalog result.
	models bool
	// configured contains an explicit model request when this is not a catalog read.
	configured mo.Option[configuredRequest]
	// appendValue contains a hidden append when selected.
	appendValue mo.Option[appendRequest]
	// appendMessage contains a model-visible append when selected.
	appendMessage mo.Option[appendMessageRequest]
	// sessionState selects active-branch recovery.
	sessionState bool
	// selection owns an accepted shared selection operation when selected.
	selection mo.Option[PreparedSelection]
	// compactionInstructions is present when this is manual compaction.
	compactionInstructions mo.Option[mo.Option[string]]
	// release returns the runtime operation reservation.
	release func()
}

var _ extensionsdk.HostOperation = (*contextOperation)(nil)

// Run executes the typed read and preserves every added error cause.
//
//nolint:gocyclo,gocognit // The closed operation union requires one explicit branch for each request.
func (o *contextOperation) Run(
	ctx context.Context,
	reporter *extensionsdk.HostProgressReporter,
) (*extensionpb.HostCompleted, error) {
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
	result := new(extensionpb.HostCompleted)
	configured, hasConfigured := o.configured.Get()
	appendValue, hasAppend := o.appendValue.Get()
	appendMessage, hasAppendMessage := o.appendMessage.Get()
	preparedSelection, hasSelection := o.selection.Get()
	compactionInstructions, hasCompaction := o.compactionInstructions.Get()
	switch {
	case hasCompaction:
		if o.service.compaction == nil {
			return nil, extensionsdk.Fail(internalFailureCode, errors.New("manual compaction is not bound"))
		}
		if reporter == nil {
			return nil, extensionsdk.Fail(
				internalFailureCode,
				errors.New("compaction progress reporter is unavailable"),
			)
		}
		progress := new(extensionpb.HostProgress)
		progress.SetCompaction(extensionpb.CompactionProgress_builder{Stage: new(compactionRunningStage)}.Build())
		if err := reporter.Report(ctx, progress); err != nil {
			return nil, extensionsdk.Fail(internalFailureCode, fmt.Errorf("report compaction progress: %w", err))
		}
		compacted, compactionErr := o.service.compaction.CompactExtension(
			ctx, o.service.extensionID, o.service.runtimeID, o.reference, compactionInstructions,
		)
		mapped, err := mapCompactionCompletion(ctx, compacted, compactionErr)
		if err != nil {
			return nil, err
		}
		result.SetCompact(mapped)
	case hasSelection:
		selectionResult := preparedSelection.Run(ctx)
		mapped, err := mapSelectionResult(selectionResult)
		if err != nil {
			return nil, err
		}
		result.SetSelection(mapped)
	case hasAppendMessage:
		appended, err := o.service.contexts.AppendExtensionMessage(
			ctx, o.service.extensionID, o.service.runtimeID, o.reference,
			appendMessage.entryType, appendMessage.text, appendMessage.visibility,
		)
		if err != nil {
			return nil, mapContextFailure("append extension message", err)
		}
		mapped, err := mapSessionEntry(appended.Entry)
		if err != nil {
			return nil, extensionsdk.Fail(internalFailureCode, err)
		}
		issues := make([]*extensionpb.AppendExtensionMessageIssue, 0, len(appended.Issues))
		for issueIndex := range appended.Issues {
			issue := &appended.Issues[issueIndex]
			if issue.Code != deliveryFailedIssueCode {
				return nil, extensionsdk.Fail(
					internalFailureCode,
					fmt.Errorf("unknown append message issue %q", issue.Code),
				)
			}
			issues = append(issues, extensionpb.AppendExtensionMessageIssue_builder{
				Code: new(
					extensionpb.AppendExtensionMessageIssueCode_APPEND_EXTENSION_MESSAGE_ISSUE_CODE_DELIVERY_FAILED,
				),
				Message: new(issue.Message), ExtensionId: new(issue.ExtensionID),
			}.Build())
		}
		result.SetAppendExtensionMessage(extensionpb.AppendExtensionMessageResult_builder{
			Entry: mapped, Issues: issues,
		}.Build())
	case hasAppend:
		entry, err := o.service.contexts.AppendExtension(
			ctx, o.service.extensionID, o.service.runtimeID, o.reference,
			appendValue.entryType, appendValue.data,
		)
		if err != nil {
			return nil, mapContextFailure("append extension entry", err)
		}
		mapped, err := mapSessionEntry(entry)
		if err != nil {
			return nil, extensionsdk.Fail(internalFailureCode, err)
		}
		result.SetAppendExtension(extensionpb.AppendExtensionResult_builder{Entry: mapped}.Build())
	case o.sessionState:
		snapshot, err := o.service.contexts.ReadSessionState(
			ctx, o.service.extensionID, o.service.runtimeID, o.reference,
		)
		if err != nil {
			return nil, mapContextFailure("read extension session state", err)
		}
		mapped, err := mapSessionState(snapshot)
		if err != nil {
			return nil, extensionsdk.Fail(internalFailureCode, err)
		}
		result.SetGetSessionState(mapped)
	case hasConfigured:
		response, err := o.service.models.Request(
			ctx, o.service.extensionID, o.service.runtimeID, o.reference,
			configured.selection, configured.instructions, configured.history,
			func(progress ConfiguredRetryProgress) error {
				if reporter == nil {
					return errors.New("configured-model retry reporter is unavailable")
				}
				value := extensionpb.ConfiguredModelRetryProgress_builder{
					CompletedAttempts: new(progress.CompletedAttempts), AttemptLimit: new(progress.AttemptLimit),
					DelayMilliseconds: new(progress.Delay.Milliseconds()), Error: new(progress.Error),
				}.Build()
				frame := new(extensionpb.HostProgress)
				frame.SetConfiguredModelRetry(value)
				return reporter.Report(ctx, frame)
			},
		)
		if err != nil {
			return nil, mapModelFailure("request configured model", err)
		}
		mapped, err := mapConfiguredResponse(response)
		if err != nil {
			return nil, extensionsdk.Fail(internalFailureCode, err)
		}
		result.SetConfiguredModel(mapped)
	case o.models:
		catalog, err := o.service.models.ReadModels(ctx, o.service.extensionID, o.service.runtimeID, o.reference)
		if err != nil {
			return nil, mapModelFailure("read model catalog", err)
		}
		result.SetGetModels(mapModelCatalog(catalog))
	default:
		providers, err := o.service.models.ReadProviders(ctx, o.service.extensionID, o.service.runtimeID, o.reference)
		if err != nil {
			return nil, mapModelFailure("read provider catalog", err)
		}
		result.SetGetProviders(mapProviderCatalog(providers))
	}
	return result, nil
}

// Release returns the reservation to the runtime accounting owner.
func (o *contextOperation) Release() {
	if selection, present := o.selection.Get(); present {
		selection.Release()
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
