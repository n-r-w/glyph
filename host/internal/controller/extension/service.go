package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
	// contexts owns issued binding validation and catalog reads.
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
	if request != nil {
		switch request.WhichRequest() {
		case extensionpb.ExtensionRequest_GetModels_case:
			reference, models = request.GetGetModels().GetContext(), true
		case extensionpb.ExtensionRequest_GetProviders_case:
			reference = request.GetGetProviders().GetContext()
		case extensionpb.ExtensionRequest_Request_not_set_case, extensionpb.ExtensionRequest_Cancel_case:
			return nil, extensionsdk.Reject(
				invalidArgumentCode,
				errors.New("model or provider catalog request is required"),
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
	return &catalogueOperation{
		service:   s,
		reference: bound,
		models:    models,
		release:   release,
		id:        operationID,
	}, nil
}

// catalogueOperation maps one admitted catalog read and owns its accounting release.
type catalogueOperation struct {
	// id identifies the extension-initiated operation in diagnostics.
	id string
	// service supplies the connected runtime identity and capability interface.
	service *Service
	// reference retains the exact admitted context reference.
	reference extensiondomain.ContextRef
	// models selects the model rather than provider catalog result.
	models bool
	// release returns the runtime operation reservation.
	release func()
}

var _ extensionsdk.HostOperation = (*catalogueOperation)(nil)

// Run executes the typed read and preserves every added error cause.
func (o *catalogueOperation) Run(ctx context.Context) (*extensionpb.HostCompleted, error) {
	slog.DebugContext(ctx, "read extension catalog", "operation_id", o.id, "extension_id", o.service.extensionID,
		"runtime_instance_id", o.service.runtimeID, "session_id", o.reference.SessionID)
	result := new(extensionpb.HostCompleted)
	if o.models {
		catalog, err := o.service.contexts.ReadModels(ctx, o.service.extensionID, o.service.runtimeID, o.reference)
		if err != nil {
			return nil, mapContextFailure(err)
		}
		result.SetGetModels(mapModelCatalog(catalog))
	} else {
		providers, err := o.service.contexts.ReadProviders(ctx, o.service.extensionID, o.service.runtimeID, o.reference)
		if err != nil {
			return nil, mapContextFailure(err)
		}
		result.SetGetProviders(mapProviderCatalog(providers))
	}
	return result, nil
}

// Release returns the reservation to the runtime accounting owner.
func (o *catalogueOperation) Release() { o.release() }

// contextFailureCode extracts the closed owner category without replacing the error text.
func contextFailureCode(err error) string {
	if failure, found := errors.AsType[ContextFailure](err); found {
		return failure.ContextCode()
	}
	return internalFailureCode
}

// mapContextFailure preserves cancellation as cancellation and classified failures as complete causes.
func mapContextFailure(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("read extension catalog: %w", err)
	}
	return extensionsdk.Fail(contextFailureCode(err), fmt.Errorf("read extension catalog: %w", err))
}
