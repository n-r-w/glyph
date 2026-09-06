package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/samber/mo"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
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
)

// Service maps requests from one connected runtime to Host context operations.
type Service struct {
	// contexts owns issued binding validation and context capabilities.
	contexts ContextOperations
	// runtime owns active-operation accounting for the connected process.
	runtime RuntimeOperations
	// extensionID comes from process discovery, never request payloads.
	extensionID string
	// runtimeID identifies the exact process incarnation connected to this controller.
	runtimeID string
}

var _ extensionsdk.HostService = (*Service)(nil)

// New binds dispatch to the extension and runtime identity supplied by process composition.
func New(contexts ContextOperations, runtime RuntimeOperations, extensionID, runtimeID string) *Service {
	return &Service{contexts: contexts, runtime: runtime, extensionID: extensionID, runtimeID: runtimeID}
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
	return &contextOperation{
		service: s, reference: bound, models: mappedRequest.models, sessionState: mappedRequest.sessionState,
		configured: mappedRequest.configured, appendValue: mappedRequest.appendValue,
		appendMessage: mappedRequest.appendMessage, release: release, id: operationID,
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
}

// mapContextRequest validates the selected request payload before admission.
func mapContextRequest(request *extensionpb.ExtensionRequest) (contextRequest, error) {
	mapped := contextRequest{
		reference: nil, models: false, sessionState: false,
		configured: mo.None[configuredRequest](), appendValue: mo.None[appendRequest](),
		appendMessage: mo.None[appendMessageRequest](),
	}
	if request == nil {
		return mapped, errors.New("extension context request is required")
	}
	switch request.WhichRequest() {
	case extensionpb.ExtensionRequest_GetModels_case:
		mapped.reference, mapped.models = request.GetGetModels().GetContext(), true
	case extensionpb.ExtensionRequest_GetProviders_case:
		mapped.reference = request.GetGetProviders().GetContext()
	case extensionpb.ExtensionRequest_ConfiguredModel_case:
		configured, err := mapConfiguredRequest(request.GetConfiguredModel())
		if err != nil {
			return contextRequest{}, err
		}
		mapped.configured, mapped.reference = mo.Some(configured), request.GetConfiguredModel().GetContext()
	case extensionpb.ExtensionRequest_AppendExtension_case:
		appendValue, err := mapAppendRequest(request.GetAppendExtension())
		if err != nil {
			return contextRequest{}, err
		}
		mapped.appendValue, mapped.reference = mo.Some(appendValue), request.GetAppendExtension().GetContext()
	case extensionpb.ExtensionRequest_GetSessionState_case:
		mapped.sessionState, mapped.reference = true, request.GetGetSessionState().GetContext()
	case extensionpb.ExtensionRequest_AppendExtensionMessage_case:
		message, err := mapAppendMessageRequest(request.GetAppendExtensionMessage())
		if err != nil {
			return contextRequest{}, err
		}
		mapped.appendMessage, mapped.reference = mo.Some(message), request.GetAppendExtensionMessage().GetContext()
	case extensionpb.ExtensionRequest_Request_not_set_case, extensionpb.ExtensionRequest_Cancel_case:
		return contextRequest{}, errors.New("extension context operation request is required")
	default:
		return contextRequest{}, errors.New("unsupported extension context request")
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
	// release returns the runtime operation reservation.
	release func()
}

var _ extensionsdk.HostOperation = (*contextOperation)(nil)

// Run executes the typed read and preserves every added error cause.
//
//nolint:gocyclo // The closed operation union requires one explicit branch for each request.
func (o *contextOperation) Run(ctx context.Context) (*extensionpb.HostCompleted, error) {
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
	switch {
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
		response, err := o.service.contexts.Request(
			ctx, o.service.extensionID, o.service.runtimeID, o.reference,
			configured.selection, configured.instructions, configured.history,
		)
		if err != nil {
			return nil, mapContextFailure("request configured model", err)
		}
		mapped, err := mapConfiguredResponse(response)
		if err != nil {
			return nil, extensionsdk.Fail(internalFailureCode, err)
		}
		result.SetConfiguredModel(mapped)
	case o.models:
		catalog, err := o.service.contexts.ReadModels(ctx, o.service.extensionID, o.service.runtimeID, o.reference)
		if err != nil {
			return nil, mapContextFailure("read model catalog", err)
		}
		result.SetGetModels(mapModelCatalog(catalog))
	default:
		providers, err := o.service.contexts.ReadProviders(ctx, o.service.extensionID, o.service.runtimeID, o.reference)
		if err != nil {
			return nil, mapContextFailure("read provider catalog", err)
		}
		result.SetGetProviders(mapProviderCatalog(providers))
	}
	return result, nil
}

// Release returns the reservation to the runtime accounting owner.
func (o *contextOperation) Release() { o.release() }

// contextFailureCode extracts the closed owner category without replacing the error text.
func contextFailureCode(err error) string {
	if failure, found := errors.AsType[ContextFailure](err); found {
		return failure.ContextCode()
	}
	return internalFailureCode
}

// mapContextFailure preserves cancellation as cancellation and classified failures as complete causes.
func mapContextFailure(action string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", action, err)
	}
	return extensionsdk.Fail(contextFailureCode(err), fmt.Errorf("%s: %w", action, err))
}
