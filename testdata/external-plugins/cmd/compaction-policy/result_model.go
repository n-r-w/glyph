package main

import (
	"context"

	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// runNestedResultWork acquires selection and model outcomes inside the participant lifetime.
func (o *referenceResultOperation) runNestedResultWork(
	ctx context.Context,
	binding *extensionsdk.ExtensionContext,
) error {
	request := o.invocation.GetOriginalRequest()
	if err := handleMissingModelSelection(ctx, binding, request.GetModel().GetProviderId()); err != nil {
		return err
	}
	return waitPolicyConfiguredModel(ctx, binding, request.GetModel(), request.GetReasoningChoice())
}
