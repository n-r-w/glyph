package main

import (
	"context"

	"github.com/samber/mo"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// turnEndAbortObserverID identifies the lifecycle handler armed by a public tool call.
const turnEndAbortObserverID = "abort-on-turn-end"

// turnEndAbortObserver acknowledges a turn-end event and aborts only when a tool armed its signal endpoint.
type turnEndAbortObserver struct {
	// url contains the one-shot signal endpoint captured during admission.
	url mo.Option[string]
}

// The compile-time assertion checks the observer against the public SDK contract.
var _ extensionsdk.HandleOperation = (*turnEndAbortObserver)(nil)

// Run observes remote abort acceptance through the live lifecycle invocation before acknowledging the event.
func (o *turnEndAbortObserver) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	if url, armed := o.url.Get(); armed {
		if err := acknowledgeAbort(ctx, url); err != nil {
			return nil, err
		}
	}
	result := new(extensionpb.HandleResponse)
	result.SetLifecycle(new(extensionpb.LifecycleAction))
	return result, nil
}

// Release ends lifecycle admission after its accepted child abort operation has settled.
func (o *turnEndAbortObserver) Release() {}
