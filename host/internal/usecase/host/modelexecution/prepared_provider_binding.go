package modelexecution

import (
	"context"
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// preparedProviderBinding executes one prepared request with its resolved provider and captured conversation owner.
type preparedProviderBinding struct {
	// service supplies retry policy, wire middleware, and retry output.
	service *Service
	// provider is the raw provider resolved when the context was prepared.
	provider ProviderAttempt
	// captured is the captured conversation owner of the prepared request.
	captured mo.Option[CapturedConversation]
	// composer composes each prepared-context revision with the handlers captured during preparation.
	composer mo.Option[ContextComposer]
	// base contains the prepared request before middleware transformation.
	base ProviderRequest
}

// Verify the prepared provider binding is the execution capability returned to Agent Core.
var _ agentrun.ModelProvider = (*preparedProviderBinding)(nil)

// Stream executes the effective request across its retry-coordinated raw provider attempts.
func (p *preparedProviderBinding) Stream(
	ctx context.Context,
	request agentrun.ModelRequest,
	handle agentrun.StreamHandler,
) error {
	// ownedHistory prevents the raw provider from mutating Agent Core history.
	ownedHistory, err := cloneHistory(request.History)
	if err != nil {
		return err
	}
	prepared := preparedRequest{base: p.base, effective: ProviderRequest{
		Instructions: request.Instructions, Model: request.Model, ReasoningChoice: request.ReasoningChoice,
		History: ownedHistory, Tools: request.Tools, Wire: mo.None[WireCallbacks](),
	}}
	responseStarted := false
	completion, err := p.service.execute(
		ctx, p.provider, prepared,
		func(event StreamEvent) error {
			responseStarted = true
			return handle(logicalStreamEvent(event))
		},
		func(progress RetryProgress) error {
			if responseStarted {
				if resetErr := handle(agentrun.StreamEvent{
					Kind: agentrun.StreamEventResponseReset, Position: mo.None[int](),
					Content: mo.None[model.Content](), Delta: mo.None[string](),
					Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](),
					Response: mo.None[model.Response](),
				}); resetErr != nil {
					return resetErr
				}
				responseStarted = false
			}
			if p.service.retryOutput == nil {
				return errors.New("deliver retry progress: Host retry output is unavailable")
			}
			return p.service.retryOutput.DeliverRetry(ctx, progress)
		},
		errModelStreamTerminalMissingResponse,
		modelStreamMissingTerminalMessage,
		selectCapturedOverflowRecovery(p.captured, p.composer),
	)
	response := completion.response
	if err == nil {
		err = completion.observe(ctx, p.captured)
	}
	if err != nil {
		if response.Outcome.IsSome() {
			deliveryErr := handle(agentrun.StreamEvent{
				Kind: agentrun.StreamEventError, Position: mo.None[int](), Content: mo.None[model.Content](),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.Some(response),
			})
			return errors.Join(err, deliveryErr)
		}
		return err
	}
	terminalKind := agentrun.StreamEventDone
	if outcome, present := response.Outcome.Get(); present &&
		(outcome == model.OutcomeFailed || outcome == model.OutcomeAborted) {
		terminalKind = agentrun.StreamEventError
	}
	return handle(agentrun.StreamEvent{
		Kind: terminalKind, Position: mo.None[int](), Content: mo.None[model.Content](),
		Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](),
		Response: mo.Some(response),
	})
}
