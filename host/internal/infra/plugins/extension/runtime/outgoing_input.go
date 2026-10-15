package runtime

import (
	"context"
	"errors"
	"fmt"
	"math"

	controllerextension "github.com/n-r-w/glyph/host/internal/controller/extension"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree"
)

// ValidateOutgoingInput invokes the captured policy's final input check.
func (r *Runtime) ValidateOutgoingInput(
	ctx context.Context,
	handlerID string,
	binding extension.Context,
	input extensioncontext.OutgoingInput,
) (terminalCause error) {
	category := modelexecution.FailureExtensionFailed
	defer func() {
		terminalCause = extensioncontext.NewPolicyOperationError(ctx, category, terminalCause)
	}()
	if input.Snapshot.Identity.Incarnation > math.MaxInt64 {
		category = modelexecution.FailureInternal
		return errors.New("session incarnation exceeds the outgoing input range")
	}
	history, err := controllerextension.MapCompactionHistory(input.Input.History)
	if err != nil {
		category = modelexecution.FailureInternal
		return fmt.Errorf("map outgoing conversation history: %w", err)
	}
	tools, err := mapPolicyTools(input.Input.Tools)
	if err != nil {
		category = modelexecution.FailureInternal
		return fmt.Errorf("map outgoing conversation tools: %w", err)
	}
	var leaf *string
	if value, present := input.Snapshot.ActiveLeafID.Get(); present {
		leaf = new(value)
	}
	outgoing := extensionpb.CompactionOutgoingInput_builder{
		SessionId: new(
			input.Snapshot.Identity.ID,
		),
		SessionIncarnation: new(int64(input.Snapshot.Identity.Incarnation)),
		ActiveLeafId:       leaf,
		Model: mapCompactionModel(
			input.Input.Model,
		),
		ReasoningChoice: new(string(input.Input.ReasoningChoice)),
		Instructions:    new(input.Input.Instructions),
		Tools:           tools,
		History:         history,
	}.Build()
	request := new(extensionpb.HandleRequest)
	request.SetHandlerId(handlerID)
	request.SetContext(mapContext(binding))
	request.SetParentCompactionId(input.LifecycleID)
	request.SetCompactionOutgoingInput(outgoing)
	completion, err := r.runCompactionHandler(ctx, handlerID, request)
	response := completion.Response
	if response == nil {
		return err
	}
	if response.GetError() != nil {
		invocationCanceled := ctx.Err() != nil &&
			errtree.IsCurrentCancellation(completion.InvocationCause, func(node error) bool {
				return errors.Is(node, ctx.Err()) || errors.Is(node, context.Cause(ctx))
			})
		if completion.InvocationCause == nil || invocationCanceled {
			category = modelexecution.FailureCompactionFailed
		}
		return err
	}
	if response.GetCompactionOutgoingInput() == nil {
		return errors.Join(
			err,
			r.protocolViolation(errors.New("outgoing conversation input acknowledgement is missing")),
		)
	}
	return err
}
