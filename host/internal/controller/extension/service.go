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
	var reference *extensionpb.ExtensionContextRef
	models := false
	configured := mo.None[configuredRequest]()
	if request != nil {
		switch request.WhichRequest() {
		case extensionpb.ExtensionRequest_GetModels_case:
			reference, models = request.GetGetModels().GetContext(), true
		case extensionpb.ExtensionRequest_GetProviders_case:
			reference = request.GetGetProviders().GetContext()
		case extensionpb.ExtensionRequest_ConfiguredModel_case:
			mapped, mapErr := mapConfiguredRequest(request.GetConfiguredModel())
			if mapErr != nil {
				return nil, extensionsdk.Reject(invalidArgumentCode, mapErr)
			}
			configured = mo.Some(mapped)
			reference = request.GetConfiguredModel().GetContext()
		case extensionpb.ExtensionRequest_Request_not_set_case, extensionpb.ExtensionRequest_Cancel_case:
			return nil, extensionsdk.Reject(
				invalidArgumentCode,
				errors.New("catalog or configured model request is required"),
			)
		default:
			return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("unsupported extension context request"))
		}
	}
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
	if err := s.contexts.ValidateContext(s.extensionID, s.runtimeID, bound); err != nil {
		return nil, extensionsdk.Reject(contextFailureCode(err), err)
	}
	release, err := s.runtime.BeginContextOperation(ctx, s.extensionID, s.runtimeID)
	if err != nil {
		return nil, extensionsdk.Reject(contextFailureCode(err), err)
	}
	return &contextOperation{
		service: s, reference: bound, models: models, configured: configured,
		release: release, id: operationID,
	}, nil
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
	// release returns the runtime operation reservation.
	release func()
}

var _ extensionsdk.HostOperation = (*contextOperation)(nil)

// Run executes the typed read and preserves every added error cause.
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
	switch {
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
