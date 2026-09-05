package extensionv1

import (
	"context"
	"errors"

	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// invocationContextKey keeps SDK invocation state separate from caller values.
type invocationContextKey struct{}

// ExtensionContext provides asynchronous Host operations for one issued session binding.
type ExtensionContext struct {
	// identity contains the immutable identity received from Host.
	identity *extensionpb.ExtensionContext
	// initiator owns outbound requests on the invoking stream.
	initiator *contextInitiator
}

// ModelsOperation owns one asynchronous model-catalog read.
type ModelsOperation struct {
	// operation owns the request lifecycle and local wait state.
	operation *contextOperation
}

// ProvidersOperation owns one asynchronous provider-catalog read.
type ProvidersOperation struct {
	// operation owns the request lifecycle and local wait state.
	operation *contextOperation
}

// ContextFrom returns the session-bound context supplied to a tool or handler invocation.
func ContextFrom(ctx context.Context) (*ExtensionContext, error) {
	binding, present := ctx.Value(invocationContextKey{}).(*ExtensionContext)
	if !present || binding == nil {
		return nil, errors.New("extension invocation has no session-bound context")
	}
	return binding, nil
}

// Identity returns a defensive copy of the issued binding.
func (c *ExtensionContext) Identity() *extensionpb.ExtensionContext {
	return proto.CloneOf(c.identity)
}

// StartGetModels starts a model-catalog read without waiting for Host acceptance.
func (c *ExtensionContext) StartGetModels(ctx context.Context) (*ModelsOperation, error) {
	request := new(extensionpb.ExtensionRequest)
	request.SetGetModels(extensionpb.GetModelsRequest_builder{Context: c.reference()}.Build())
	started, err := c.initiator.start(ctx, request)
	if err != nil {
		return nil, err
	}
	return &ModelsOperation{operation: started}, nil
}

// StartGetProviders starts a provider-catalog read without waiting for Host acceptance.
func (c *ExtensionContext) StartGetProviders(ctx context.Context) (*ProvidersOperation, error) {
	request := new(extensionpb.ExtensionRequest)
	request.SetGetProviders(extensionpb.GetProvidersRequest_builder{Context: c.reference()}.Build())
	started, err := c.initiator.start(ctx, request)
	if err != nil {
		return nil, err
	}
	return &ProvidersOperation{operation: started}, nil
}

// Wait waits locally for the typed model-catalog result without canceling remote work.
func (o *ModelsOperation) Wait(ctx context.Context) (*extensionpb.GetModelsResult, error) {
	result, err := o.operation.wait(ctx)
	if err != nil {
		return nil, err
	}
	return result.GetGetModels(), nil
}

// Wait waits locally for the typed provider-catalog result without canceling remote work.
func (o *ProvidersOperation) Wait(ctx context.Context) (*extensionpb.GetProvidersResult, error) {
	result, err := o.operation.wait(ctx)
	if err != nil {
		return nil, err
	}
	return result.GetGetProviders(), nil
}

// reference constructs the exact reference issued to this runtime.
func (c *ExtensionContext) reference() *extensionpb.ExtensionContextRef {
	return extensionpb.ExtensionContextRef_builder{
		ContextId: new(c.identity.GetContextId()), RuntimeInstanceId: new(c.identity.GetRuntimeInstanceId()),
		SessionId: new(c.identity.GetSessionId()),
	}.Build()
}

// invocationContext validates Host identity before exposing context operations to plugin code.
func invocationContext(identity *extensionpb.ExtensionContext, initiator *contextInitiator) (*ExtensionContext, error) {
	if identity == nil || identity.GetContextId() == "" || identity.GetExtensionId() == "" ||
		identity.GetRuntimeInstanceId() == "" ||
		identity.GetSessionId() == "" ||
		identity.GetCwd() == "" {
		return nil, Reject(
			rejectionCodeInvalidArgument,
			errors.New("tool or handler invocation requires complete extension context identity"),
		)
	}
	return &ExtensionContext{identity: proto.CloneOf(identity), initiator: initiator}, nil
}
