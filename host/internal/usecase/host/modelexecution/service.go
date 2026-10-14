package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"github.com/samber/mo"

	controllerextension "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionmodels"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

const (
	// modelStreamMissingTerminalMessage describes a logical stream without provider terminal output.
	modelStreamMissingTerminalMessage = "model stream ended without a terminal response"
	// modelStreamTerminalMissingResponseMessage describes malformed logical-stream terminal output.
	modelStreamTerminalMissingResponseMessage = "model stream terminal event has no response"
	// modelRequestMissingTerminalMessage describes a configured request without provider terminal output.
	modelRequestMissingTerminalMessage = "model request ended without a terminal response"
	// modelTerminalMissingResponseMessage describes malformed configured-request terminal output.
	modelTerminalMissingResponseMessage = "model request terminal event has no response"
)

var (
	// errModelStreamTerminalMissingResponse identifies malformed logical-stream terminal output.
	errModelStreamTerminalMissingResponse = errors.New(modelStreamTerminalMissingResponseMessage)
	// errModelTerminalMissingResponse identifies malformed configured-request terminal output.
	errModelTerminalMissingResponse = errors.New(modelTerminalMissingResponseMessage)
)

// Service owns retry-coordinated logical model execution for Host consumers.
type Service struct {
	// catalog resolves logical model selections to raw provider bindings.
	catalog CatalogResolver
	// conversationContext records usage for remaining legacy overflow accounting.
	conversationContext ConversationContext
	// policies captures preparation and observation for the admitted owner.
	policies ConversationPolicies
	// overflow supplies legacy overflow recovery.
	overflow OverflowRecovery
	// retryPolicy contains validated persistent policy values.
	retryPolicy RetryPolicy
	// retryEnabled contains process-local runtime enablement.
	retryEnabled atomic.Bool
	// retryHandlers invokes the registration snapshot captured for each execution.
	retryHandlers RetryHandlers
	// retryOutput delivers agent-request retry progress through the active Host mode.
	retryOutput RetryOutput
	// manualPreparation invokes the captured manual compaction policy.
	manualPreparation ManualCompactionPreparation
	// manualTools supplies conversation tool definitions for manual operations.
	manualTools ManualCompactionTools
	// manualInstructions contains the conversation instructions for manual input.
	manualInstructions string
}

var (
	_ agentrun.ModelRuntime                    = (*Service)(nil)
	_ agentrun.ModelProvider                   = (*Service)(nil)
	_ extensionmodels.ModelRequester           = (*Service)(nil)
	_ sessiontree.ModelRequester               = (*Service)(nil)
	_ hostprogrammatic.RetryControl            = (*Service)(nil)
	_ hostui.RetryControl                      = (*Service)(nil)
	_ hostui.Compactor                         = (*Service)(nil)
	_ hostprogrammatic.Compactor               = (*Service)(nil)
	_ controllerextension.CompactionOperations = (*Service)(nil)
)

// New creates the logical model-execution owner from one validated retry policy.
func New(
	catalog CatalogResolver,
	conversationContext ConversationContext,
	retryPolicy RetryPolicy,
	retryHandlers RetryHandlers,
	retryOutput RetryOutput,
	policies ConversationPolicies,
) *Service {
	policy := retryPolicy
	policy.Delays = append([]time.Duration(nil), retryPolicy.Delays...)
	service := &Service{
		catalog: catalog, conversationContext: conversationContext, policies: policies, overflow: nil,
		retryPolicy: policy, retryEnabled: atomic.Bool{}, retryHandlers: retryHandlers, retryOutput: retryOutput,
		manualPreparation: nil, manualTools: nil, manualInstructions: "",
	}
	service.retryEnabled.Store(policy.Enabled)
	return service
}

// BindOverflowRecovery connects legacy overflow recovery.
func (s *Service) BindOverflowRecovery(recovery OverflowRecovery) error {
	if s.overflow != nil {
		return errors.New("overflow recovery is already bound")
	}
	if recovery == nil {
		return errors.New("overflow recovery is required")
	}
	s.overflow = recovery
	return nil
}

// overflowRecovery supplies the legacy callback for an admitted owner.
func (s *Service) overflowRecovery(hasOwner bool) overflowRecoveryHandler {
	if !hasOwner || s.overflow == nil {
		return nil
	}
	return s.overflow.RecoverOverflow
}

// SetRetryEnabled changes process-local enablement for later logical executions.
func (s *Service) SetRetryEnabled(enabled bool) { s.retryEnabled.Store(enabled) }

// RetryPolicy returns a detached policy snapshot for Host client consumers.
func (s *Service) RetryPolicy() (
	enabled bool,
	maxRetries int64,
	delays []time.Duration,
	maxProviderDelay time.Duration,
) {
	policy := s.retryPolicySnapshot()
	return policy.Enabled, policy.MaxRetries, slices.Clone(policy.Delays), policy.MaxProviderDelay
}

// retryPolicySnapshot returns one detached internal execution-policy snapshot.
func (s *Service) retryPolicySnapshot() RetryPolicy {
	policy := s.retryPolicy
	policy.Enabled = s.retryEnabled.Load()
	policy.Delays = append([]time.Duration(nil), s.retryPolicy.Delays...)
	return policy
}

// Snapshot returns the active logical model snapshot with this service as its provider.
func (s *Service) Snapshot() agentrun.RequestSnapshot {
	// binding keeps the model and provider selection atomic.
	binding := s.catalog.ActiveBinding()
	return agentrun.RequestSnapshot{
		Model:           binding.Model,
		ReasoningChoice: binding.ReasoningChoice,
		Provider:        s,
	}
}

// Stream executes one logical Agent Core request across its retry-coordinated raw provider attempts.
func (s *Service) Stream(
	ctx context.Context,
	request agentrun.ModelRequest,
	handle agentrun.StreamHandler,
) error {
	captured, err := s.captureConversation(ctx)
	if err != nil {
		return err
	}
	// selection reconstructs the exact catalog key captured by Agent Core.
	selection := model.Selection{
		Provider:        request.Model.Provider,
		Model:           request.Model.Model,
		ReasoningChoice: request.ReasoningChoice,
	}
	// binding selects one raw provider without configured-request credential policy.
	binding, err := s.catalog.ResolveBinding(selection)
	if err != nil {
		return err
	}
	// ownedHistory prevents the raw provider from mutating Agent Core history.
	ownedHistory, err := cloneHistory(request.History)
	if err != nil {
		return err
	}
	providerRequest := ProviderRequest{
		Instructions: request.Instructions, Model: binding.Model, ReasoningChoice: binding.ReasoningChoice,
		History: ownedHistory, Tools: request.Tools,
	}
	providerRequest, err = s.prepareProviderContext(ctx, providerRequest, captured)
	if err != nil {
		return err
	}
	responseStarted := false
	completion, err := s.execute(
		ctx, binding.Provider, providerRequest,
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
			if s.retryOutput == nil {
				return errors.New("deliver retry progress: Host retry output is unavailable")
			}
			return s.retryOutput.DeliverRetry(ctx, progress)
		},
		errModelStreamTerminalMissingResponse,
		modelStreamMissingTerminalMessage,
		s.overflowRecovery(captured.IsSome()),
	)
	response := completion.response
	if err == nil {
		err = completion.observe(ctx, captured)
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
	if handleErr := handle(agentrun.StreamEvent{
		Kind: terminalKind, Position: mo.None[int](), Content: mo.None[model.Content](),
		Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](),
		Response: mo.Some(response),
	}); handleErr != nil {
		return handleErr
	}
	s.conversationContext.RecordCompletedConversationUsage(completion.input, response)
	return nil
}

// prepareProviderContext obtains policy-selected history for provider dispatch.
func (s *Service) prepareProviderContext(
	ctx context.Context,
	request ProviderRequest,
	capture mo.Option[CapturedConversation],
) (ProviderRequest, error) {
	owner, present := capture.Get()
	if !present {
		return request, nil
	}
	frozen := request.cloneBinding()
	prepared, err := owner.Prepare(ctx, frozen)
	if err == nil {
		request.History = prepared.History
		return request, nil
	}
	if !prepared.Committed && isPureOwningCancellation(ctx, err) {
		return ProviderRequest{}, joinOwningContextCause(ctx, err)
	}
	wrapped := fmt.Errorf("prepare conversation context: %w", joinOwningContextCause(ctx, err))
	return ProviderRequest{}, contextPreparationLogicalFailure(wrapped)
}

// RequestConfigured executes one configured request with operation-scoped retry progress.
func (s *Service) RequestConfigured(
	ctx context.Context,
	selection model.Selection,
	instructions string,
	history []agent.HistoryEntry,
	progress func(completedAttempts, attemptLimit int64, delay time.Duration, failure string) error,
) (model.Response, error) {
	// binding preserves exact configured selection validation and credential preflight.
	binding, err := s.catalog.ResolveConfiguredBinding(ctx, selection)
	if err != nil {
		return model.Response{}, err
	}
	if !slices.Contains(binding.Model.Input, model.InputModalityImage) {
		for messageIndex := range history {
			if user, present := history[messageIndex].User.Get(); present {
				for contentIndex, content := range user.Content {
					if content.Kind == model.InputContentImage {
						return model.Response{}, fmt.Errorf(
							"configured model %s does not accept image input at message %d content %d",
							binding.Model.Model, messageIndex, contentIndex,
						)
					}
				}
			}
		}
	}
	// ownedHistory prevents the raw provider from mutating caller history.
	ownedHistory, err := cloneHistory(history)
	if err != nil {
		return model.Response{}, err
	}
	request := ProviderRequest{
		Instructions: instructions, Model: binding.Model, ReasoningChoice: binding.ReasoningChoice,
		History: ownedHistory, Tools: nil,
	}
	completion, err := s.execute(
		ctx, binding.Provider, request, nil, func(value RetryProgress) error {
			if progress == nil {
				return nil
			}
			return progress(value.CompletedAttempts, value.AttemptLimit, value.Delay, value.Error)
		},
		errModelTerminalMissingResponse, modelRequestMissingTerminalMessage,
		nil,
	)
	if err != nil {
		return model.Response{}, fmt.Errorf("execute model request: %w", err)
	}
	return completion.response, nil
}

// clone detaches mutable request slices for one raw attempt.
func (request ProviderRequest) clone() (ProviderRequest, error) {
	history, err := cloneHistory(request.History)
	if err != nil {
		return ProviderRequest{}, fmt.Errorf("clone provider request history: %w", err)
	}
	cloned := request.cloneBinding()
	cloned.History = history
	return cloned, nil
}

// cloneBinding detaches model and tool definitions while retaining the caller's history.
func (request ProviderRequest) cloneBinding() ProviderRequest {
	tools := make([]tool.Descriptor, len(request.Tools))
	for index := range request.Tools {
		tools[index] = request.Tools[index]
		tools[index].InputSchemaJSON = slices.Clone(request.Tools[index].InputSchemaJSON)
	}
	return ProviderRequest{
		Instructions: request.Instructions, Model: request.Model.Clone(), ReasoningChoice: request.ReasoningChoice,
		History: request.History, Tools: tools,
	}
}

// missingTerminalFailure classifies one incomplete raw provider attempt without selecting retry policy.
func missingTerminalFailure(message string) error {
	return &ProviderFailureError{
		Classification: ProviderFailureTransient,
		RetryDelay:     mo.None[time.Duration](),
		Cause:          errors.New(message),
	}
}

// cloneHistory validates and transfers immutable history ownership to one raw attempt.
func cloneHistory(history []agent.HistoryEntry) ([]agent.HistoryEntry, error) {
	// ownedHistory receives validated deep clones in source order.
	ownedHistory := make([]agent.HistoryEntry, len(history))
	for index := range history {
		clone, err := history[index].ValidatedClone()
		if err != nil {
			return nil, fmt.Errorf("validate model request history: %w", err)
		}
		ownedHistory[index] = clone
	}
	return ownedHistory, nil
}

// logicalStreamEvent converts one raw event into the independent Agent Core consumer contract.
func logicalStreamEvent(event StreamEvent) agentrun.StreamEvent {
	// logicalKind remains the Agent Core invalid zero value for unsupported raw kinds.
	logicalKind := agentrun.StreamEventKind(0)
	switch event.Kind {
	case StreamEventContentStart:
		logicalKind = agentrun.StreamEventContentStart
	case StreamEventTextDelta:
		logicalKind = agentrun.StreamEventTextDelta
	case StreamEventContentEnd:
		logicalKind = agentrun.StreamEventContentEnd
	case StreamEventToolCallStart:
		logicalKind = agentrun.StreamEventToolCallStart
	case StreamEventToolCallDelta:
		logicalKind = agentrun.StreamEventToolCallDelta
	case StreamEventToolCallEnd:
		logicalKind = agentrun.StreamEventToolCallEnd
	case StreamEventDone:
		logicalKind = agentrun.StreamEventDone
	case StreamEventError:
		logicalKind = agentrun.StreamEventError
	}
	return agentrun.StreamEvent{
		Kind:     logicalKind,
		Position: event.Position,
		Content:  event.Content,
		Delta:    event.Delta,
		Preview:  event.Preview,
		ToolCall: event.ToolCall,
		Response: event.Response,
	}
}
