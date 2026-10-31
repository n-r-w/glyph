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
	// wire supplies Host-owned per-attempt middleware without provider dependencies.
	wire WireMiddleware
	// contextMiddleware supplies Host-owned instruction and history middleware for each logical execution.
	contextMiddleware ContextMiddleware
	// responseFinalizer supplies the final value of each configured terminal model response.
	responseFinalizer ResponseFinalizer
	// catalog resolves logical model selections to raw provider bindings.
	catalog CatalogResolver
	// policies captures preparation and observation for the admitted owner.
	policies ConversationPolicies
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
	_ agentrun.EffectiveContextPort            = (*Service)(nil)
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
	retryPolicy RetryPolicy,
	retryHandlers RetryHandlers,
	retryOutput RetryOutput,
	policies ConversationPolicies,
) *Service {
	policy := retryPolicy
	policy.Delays = append([]time.Duration(nil), retryPolicy.Delays...)
	service := &Service{
		catalog: catalog, policies: policies,
		retryPolicy: policy, retryEnabled: atomic.Bool{}, retryHandlers: retryHandlers, retryOutput: retryOutput,
		manualPreparation: nil, manualTools: nil, manualInstructions: "", wire: nil, contextMiddleware: nil,
		responseFinalizer: nil,
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

// PrepareContext returns the effective request of the active conversation and the capability that executes it.
func (s *Service) PrepareContext(
	ctx context.Context,
	request agentrun.ContextRequest,
) (agentrun.EffectiveContext, error) {
	captured, err := s.captureConversation(ctx)
	if err != nil {
		return agentrun.EffectiveContext{}, err
	}
	// binding keeps the model and provider selection atomic.
	binding := s.catalog.ActiveBinding()
	// ownedHistory prevents preparation and middleware from mutating Agent Core history.
	ownedHistory, err := cloneHistory(request.History)
	if err != nil {
		return agentrun.EffectiveContext{}, err
	}
	base, err := s.prepareProviderContext(ctx, ProviderRequest{
		Instructions: request.Instructions, Model: binding.Model, ReasoningChoice: binding.ReasoningChoice,
		History: ownedHistory, Tools: request.Tools, Wire: mo.None[WireCallbacks](),
	}, captured)
	if err != nil {
		return agentrun.EffectiveContext{}, err
	}
	composer := s.newContextComposer()
	prepared, err := composeRequest(ctx, composer, base, nil)
	if err != nil {
		return agentrun.EffectiveContext{}, err
	}
	effective := prepared.effective.cloneBinding()
	return agentrun.EffectiveContext{
		Request: agentrun.ModelRequest{
			Instructions: effective.Instructions, Model: effective.Model, ReasoningChoice: effective.ReasoningChoice,
			History: agent.CloneHistory(effective.History), Tools: effective.Tools,
		},
		Provider: &preparedProviderBinding{
			service: s, provider: binding.Provider, captured: captured, composer: composer, base: prepared.base,
		},
	}, nil
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
	if s.responseFinalizer == nil {
		return model.Response{}, errors.New("configured model request requires a bound response finalizer")
	}
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
	request, err := composeRequest(ctx, s.newContextComposer(), ProviderRequest{
		Instructions: instructions, Model: binding.Model, ReasoningChoice: binding.ReasoningChoice,
		History: ownedHistory, Tools: nil, Wire: mo.None[WireCallbacks](),
	}, nil)
	if err != nil {
		return model.Response{}, err
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
	return s.finalizeConfiguredResponse(ctx, completion.response)
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
		History: request.History, Tools: tools, Wire: request.Wire,
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
	ownedHistory, err := agent.ValidatedCloneHistory(history)
	if err != nil {
		return nil, fmt.Errorf("validate model request history: %w", err)
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
