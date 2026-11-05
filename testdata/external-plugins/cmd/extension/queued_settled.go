package main

import (
	"context"
	"errors"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// queuedSettledMode arms one attachment after the current run settles.
	queuedSettledMode = "queue-on-settled"
	// queuedSettledObserverID identifies the public settled observer.
	queuedSettledObserverID = "queue-next-user-attachment"
)

// queuedSettledOperation sends one attachment while Core is idle.
type queuedSettledOperation struct {
	// binding supplies the observer's public context.
	binding *extensionsdk.ExtensionContext
	// service owns the one-shot observer request.
	service *service
}

// Compile-time assertion verifies the observer implements the public operation contract.
var _ extensionsdk.HandleOperation = (*queuedSettledOperation)(nil)

// PrepareHandle admits the settled observer or another fixture handler.
func (s *service) PrepareHandle(
	ctx context.Context,
	request *extensionv1.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	if request.GetHandlerId() != queuedSettledObserverID {
		return s.prepareHandle(ctx, request)
	}
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	return &queuedSettledOperation{binding: binding, service: s}, nil
}

// Run waits for acceptance from the public append operation after settlement.
func (o *queuedSettledOperation) Run(ctx context.Context) (*extensionv1.HandleResponse, error) {
	o.service.contextMutex.Lock()
	armed := o.service.queueOnSettled
	o.service.queueOnSettled = false
	o.service.contextMutex.Unlock()
	if armed {
		operation, err := o.binding.StartAppendExtensionMessage(ctx, extensionv1.AppendExtensionMessageRequest_builder{
			Context: nil, EntryType: new(queuedMessageType), Text: new(queuedNextTurnText),
			Visibility: new(extensionv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE),
			Delivery:   new(extensionv1.ExtensionMessageDelivery_EXTENSION_MESSAGE_DELIVERY_NEXT_TURN),
		}.Build())
		if err != nil {
			return nil, err
		}
		result, err := operation.Wait(ctx)
		if err != nil {
			return nil, err
		}
		if !result.HasQueued() || result.HasEntry() || len(result.GetIssues()) != 0 {
			return nil, errors.New("settled append must return queued acceptance without a committed entry")
		}
	}
	result := new(extensionv1.HandleResponse)
	result.SetLifecycle(new(extensionv1.LifecycleAction))
	return result, nil
}

// Release frees the observer operation, which owns no reservation.
func (*queuedSettledOperation) Release() {}
