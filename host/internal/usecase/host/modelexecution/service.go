package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"github.com/samber/mo"

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
	// conversationContext observes completed delivered agent responses.
	conversationContext ConversationContext
	// retryPolicy contains validated persistent policy values.
	retryPolicy RetryPolicy
	// retryEnabled contains process-local runtime enablement.
	retryEnabled atomic.Bool
	// retryHandlers invokes the registration snapshot captured for each execution.
	retryHandlers RetryHandlers
	// retryOutput delivers agent-request retry progress through the active Host mode.
	retryOutput RetryOutput
}

var (
	_ agentrun.ModelRuntime          = (*Service)(nil)
	_ agentrun.ModelProvider         = (*Service)(nil)
	_ extensionmodels.ModelRequester = (*Service)(nil)
	_ sessiontree.ModelRequester     = (*Service)(nil)
	_ hostprogrammatic.RetryControl  = (*Service)(nil)
	_ hostui.RetryControl            = (*Service)(nil)
)

// New creates the logical model-execution owner from one validated retry policy.
func New(
	catalog CatalogResolver,
	conversationContext ConversationContext,
	retryPolicy RetryPolicy,
	retryHandlers RetryHandlers,
	retryOutput RetryOutput,
) *Service {
	policy := retryPolicy
	policy.Delays = append([]time.Duration(nil), retryPolicy.Delays...)
	service := &Service{
		catalog: catalog, conversationContext: conversationContext,
		retryPolicy: policy, retryEnabled: atomic.Bool{}, retryHandlers: retryHandlers, retryOutput: retryOutput,
	}
	service.retryEnabled.Store(policy.Enabled)
	return service
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
	responseStarted := false
	response, err := s.execute(
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
	)
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
	s.conversationContext.ObserveCompletedConversation(providerRequest, response)
	return nil
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
	// ownedHistory prevents the raw provider from mutating caller history.
	ownedHistory, err := cloneHistory(history)
	if err != nil {
		return model.Response{}, err
	}
	request := ProviderRequest{
		Instructions: instructions, Model: binding.Model, ReasoningChoice: binding.ReasoningChoice,
		History: ownedHistory, Tools: nil,
	}
	response, err := s.execute(
		ctx, binding.Provider, request, nil, func(value RetryProgress) error {
			if progress == nil {
				return nil
			}
			return progress(value.CompletedAttempts, value.AttemptLimit, value.Delay, value.Error)
		},
		errModelTerminalMissingResponse, modelRequestMissingTerminalMessage,
	)
	if err != nil {
		return model.Response{}, fmt.Errorf("execute model request: %w", err)
	}
	return response, nil
}

// clone detaches mutable request slices for one raw attempt.
func (request ProviderRequest) clone() (ProviderRequest, error) {
	history, err := cloneHistory(request.History)
	if err != nil {
		return ProviderRequest{}, fmt.Errorf("clone provider request history: %w", err)
	}
	tools := make([]tool.Descriptor, len(request.Tools))
	for index := range request.Tools {
		tools[index] = request.Tools[index]
		tools[index].InputSchemaJSON = slices.Clone(request.Tools[index].InputSchemaJSON)
	}
	return ProviderRequest{
		Instructions: request.Instructions, Model: request.Model.Clone(), ReasoningChoice: request.ReasoningChoice,
		History: history, Tools: tools,
	}, nil
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
