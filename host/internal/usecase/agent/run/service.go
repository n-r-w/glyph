// Package run implements provider-neutral Agent Core run behavior.
package run

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/hooks"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

const (
	failedModelMessage  = "Model request failed."
	abortedModelMessage = "Model request was canceled."
	lengthCallMessage   = "Tool call skipped because the model response reached its length limit."
)

var (
	// ErrRunActive rejects another run before Host settlement.
	ErrRunActive = errors.New("agent run is active")
	// ErrSettlement rejects settlement for a different or incomplete run.
	ErrSettlement = errors.New("agent run cannot be settled")
)

// Service owns in-memory history and one active run state.
type Service struct {
	instructions string
	runtime      ModelRuntime
	hooks        hooks.ContextRunner
	tools        ToolRuntime
	events       EventSink

	mutex   sync.RWMutex
	state   State
	history []agent.HistoryEntry
}

// New creates an Agent Core run service.
func New(
	instructions string,
	runtime ModelRuntime,
	hookRunner hooks.ContextRunner,
	tools ToolRuntime,
	events EventSink,
) *Service {
	return &Service{
		instructions: instructions,
		runtime:      runtime,
		hooks:        hookRunner,
		tools:        tools,
		events:       events,
		mutex:        sync.RWMutex{},
		state: State{
			Status:          StatusIdle,
			RunID:           mo.None[string](),
			PartialResponse: mo.None[model.Response](),
			ToolPreviews:    nil,
		},
		history: nil,
	}
}

// Run executes one user request through the model/tool loop.
func (s *Service) Run(ctx context.Context, request Request) (Result, error) {
	startIndex, beginErr := s.begin(request)
	if beginErr != nil {
		return Result{}, beginErr
	}
	deliveryErr := s.deliver(ctx, newEvent(EventAgentStart, request.RunID))
	if deliveryErr != nil {
		return s.finish(
			ctx, request.RunID, startIndex, agent.RunOutcomeFailed, mo.Some(deliveryErr.Error()), deliveryErr,
		)
	}

	for {
		if contextErr := ctx.Err(); contextErr != nil {
			return s.finish(
				ctx, request.RunID, startIndex, agent.RunOutcomeAborted, mo.Some(abortedModelMessage), contextErr,
			)
		}
		turnResult, next, runErr := s.runTurn(ctx, request.RunID)
		if next {
			continue
		}
		return s.finish(ctx, request.RunID, startIndex, turnResult.Outcome, turnResult.ErrorMessage, runErr)
	}
}

// Settle completes the Host settlement handoff and makes the agent idle.
func (s *Service) Settle(runID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	activeRunID, present := s.state.RunID.Get()
	if s.state.Status != StatusAwaitingSettlement || !present || activeRunID != runID {
		return fmt.Errorf("%w: %q", ErrSettlement, runID)
	}
	s.state = State{
		Status:          StatusIdle,
		RunID:           mo.None[string](),
		PartialResponse: mo.None[model.Response](),
		ToolPreviews:    nil,
	}
	return nil
}

// State returns an immutable current state snapshot.
func (s *Service) State() State {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return State{
		Status:          s.state.Status,
		RunID:           s.state.RunID,
		PartialResponse: s.state.PartialResponse.MapValue(cloneModelResponse),
		ToolPreviews:    cloneToolPreviews(s.state.ToolPreviews),
	}
}

// History returns an immutable ordered history snapshot.
func (s *Service) History() []agent.HistoryEntry {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return cloneHistory(s.history)
}

// ProjectHistory returns provider-visible history with temporary skipped results.
func (s *Service) ProjectHistory() []agent.HistoryEntry {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return projectHistory(s.history)
}

// begin reserves the only run slot and stores the user message.
func (s *Service) begin(request Request) (int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.state.Status != StatusIdle {
		return 0, ErrRunActive
	}
	startIndex := len(s.history)
	s.history = append(s.history, agent.HistoryEntry{
		Kind:       agent.HistoryEntryUser,
		User:       mo.Some(model.TextMessage(request.UserText)),
		Model:      mo.None[model.Response](),
		ToolResult: mo.None[agent.ToolResult](),
	})
	s.state = State{
		Status:          StatusRunning,
		RunID:           mo.Some(request.RunID),
		PartialResponse: mo.Some(model.Response{}),
		ToolPreviews:    make(map[string]model.ToolCallPreview),
	}
	return startIndex, nil
}

// runTurn performs one provider request and applies its terminal outcome.
//
//nolint:gocyclo // The branches preserve explicit stream, delivery, and terminal failure paths.
func (s *Service) runTurn(ctx context.Context, runID string) (Result, bool, error) {
	if err := s.deliver(ctx, newEvent(EventTurnStart, runID)); err != nil {
		return Result{Outcome: agent.RunOutcomeFailed, AddedHistory: nil, ErrorMessage: mo.Some(err.Error())}, false, err
	}
	if err := s.deliver(ctx, newEvent(EventMessageStart, runID)); err != nil {
		return Result{Outcome: agent.RunOutcomeFailed, AddedHistory: nil, ErrorMessage: mo.Some(err.Error())}, false, err
	}

	projectedContext, hookErr := s.hooks.TransformContext(ctx, hooks.Context{History: s.ProjectHistory()})
	if hookErr != nil {
		return s.finalizeProviderError(ctx, runID, internalHookFailureResponse(hooks.StageContext), hookErr)
	}

	var deliveryErr error
	var response model.Response
	tools := s.tools.Tools()
	selection := s.runtime.Current()
	providerErr := selection.Provider.Stream(ctx, ModelRequest{
		Instructions:    s.instructions,
		Model:           selection.Model,
		ReasoningChoice: selection.ReasoningChoice,
		History:         projectedContext.History,
		Tools:           tools,
	}, func(streamEvent StreamEvent) error {
		var terminal model.Response
		if streamEvent.Kind == StreamEventDone || streamEvent.Kind == StreamEventError {
			var terminalErr error
			terminal, terminalErr = mergeTerminalResponse(streamEvent.Response, s.State().PartialResponse)
			if terminalErr != nil {
				return terminalErr
			}
			streamEvent.Response = mo.Some(terminal)
		}
		if err := s.applyStreamEvent(streamEvent); err != nil {
			return err
		}
		if streamEvent.Kind == StreamEventDone || streamEvent.Kind == StreamEventError {
			response = cloneModelResponse(terminal)
			return nil
		}
		event := newEvent(EventContentStart, runID)
		switch streamEvent.Kind {
		case StreamEventContentStart:
			event.Type = EventContentStart
		case StreamEventTextDelta:
			event.Type = EventTextDelta
		case StreamEventContentEnd:
			event.Type = EventContentEnd
		case StreamEventToolCallStart:
			event.Type = EventToolCallStart
			event.Preview = streamEvent.Preview
		case StreamEventToolCallDelta:
			event.Type = EventToolCallDelta
			event.Preview = streamEvent.Preview
		case StreamEventToolCallEnd:
			event.Type = EventToolCallEnd
			event.ToolCall = streamEvent.ToolCall
		case StreamEventDone, StreamEventError:
			return errors.New("terminal model stream event reached lifecycle delivery")
		default:
			return fmt.Errorf("unsupported model stream event kind %d", streamEvent.Kind)
		}
		event.Position = streamEvent.Position
		if streamEvent.Kind == StreamEventContentStart || streamEvent.Kind == StreamEventTextDelta ||
			streamEvent.Kind == StreamEventContentEnd {
			event.Content = streamEvent.Content.MapValue(func(content model.Content) model.Content {
				return model.Content{
					Kind:            content.Kind,
					Text:            streamEvent.Delta,
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
				}
			})
		}
		eventErr := s.deliver(ctx, event)
		if eventErr != nil {
			deliveryErr = eventErr
		}
		return eventErr
	})
	if deliveryErr != nil {
		s.clearPartial()
		return Result{
			Outcome: agent.RunOutcomeFailed, AddedHistory: nil, ErrorMessage: mo.Some(deliveryErr.Error()),
		}, false, deliveryErr
	}
	if providerErr != nil {
		return s.finalizeProviderError(ctx, runID, response, providerErr)
	}
	if outcome, present := response.Outcome.Get(); !present || outcome == 0 {
		return s.finalizeProviderError(ctx, runID, response, errors.New("model stream ended without a terminal event"))
	}

	response = normalizeTerminalResponse(cloneModelResponse(response))
	s.clearPartial()
	s.appendModel(response)
	messageEnd := newEvent(EventMessageEnd, runID)
	messageEnd.Message = mo.Some(response)
	if err := s.deliver(context.WithoutCancel(ctx), messageEnd); err != nil {
		return Result{Outcome: agent.RunOutcomeFailed, AddedHistory: nil, ErrorMessage: mo.Some(err.Error())}, false, err
	}
	return s.applyOutcome(ctx, runID, response)
}

// internalHookFailureResponse creates one safe provider-neutral hook failure.
func internalHookFailureResponse(stage hooks.Stage) model.Response {
	return model.Response{
		Content:       nil,
		Outcome:       mo.Some(model.OutcomeFailed),
		ErrorMessage:  mo.Some(failedModelMessage),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics: []model.Diagnostic{{
			Code:    "internal_hook_failed",
			Message: string(stage),
		}},
	}
}

// normalizeTerminalResponse supplies safe errors for provider-declared terminal failures.
// mergeTerminalResponse validates and completes one terminal response with streamed content.
func mergeTerminalResponse(
	responseOption mo.Option[model.Response],
	partialOption mo.Option[model.Response],
) (model.Response, error) {
	terminal, present := responseOption.Get()
	if !present {
		return model.Response{}, errors.New("terminal model stream event has no response")
	}
	terminal = cloneModelResponse(terminal)
	if partial, hasPartial := partialOption.Get(); len(terminal.Content) == 0 && hasPartial {
		terminal.Content = partial.Content
	}
	return terminal, nil
}

func normalizeTerminalResponse(response model.Response) model.Response {
	if errorMessage, present := response.ErrorMessage.Get(); present && errorMessage != "" {
		return response
	}
	outcome, present := response.Outcome.Get()
	if !present {
		return response
	}
	switch outcome {
	case model.OutcomeAborted:
		response.ErrorMessage = mo.Some(abortedModelMessage)
	case model.OutcomeFailed:
		response.ErrorMessage = mo.Some(failedModelMessage)
	case model.OutcomeStop, model.OutcomeToolUse, model.OutcomeLength:
	}
	return response
}

// finalizeProviderError stores safe partial content and excludes it from later projections.
func (s *Service) finalizeProviderError(
	ctx context.Context,
	runID string,
	response model.Response,
	providerErr error,
) (Result, bool, error) {
	response = cloneModelResponse(response)
	if partial, present := s.State().PartialResponse.Get(); len(response.Content) == 0 && present {
		response.Content = partial.Content
	}
	outcome := model.OutcomeFailed
	if errors.Is(providerErr, context.Canceled) || errors.Is(providerErr, context.DeadlineExceeded) || ctx.Err() != nil {
		outcome = model.OutcomeAborted
	}
	errorMessage, hasErrorMessage := response.ErrorMessage.Get()
	if !hasErrorMessage || errorMessage == "" {
		errorMessage = failedModelMessage
		if outcome == model.OutcomeAborted {
			errorMessage = abortedModelMessage
		}
	}
	response.Outcome = mo.Some(outcome)
	response.ErrorMessage = mo.Some(errorMessage)
	s.clearPartial()
	s.appendModel(response)
	terminalContext := context.WithoutCancel(ctx)
	messageEnd := newEvent(EventMessageEnd, runID)
	messageEnd.Message = mo.Some(response)
	deliveryErr := s.deliver(terminalContext, messageEnd)
	turn := TurnSummary{Response: response, ToolResults: nil}
	turnEnd := newEvent(EventTurnEnd, runID)
	turnEnd.Turn = mo.Some(turn)
	deliveryErr = errors.Join(deliveryErr, s.deliver(terminalContext, turnEnd))
	runOutcome := agent.RunOutcomeFailed
	if outcome == model.OutcomeAborted {
		runOutcome = agent.RunOutcomeAborted
	}
	return Result{
		Outcome: runOutcome, AddedHistory: nil, ErrorMessage: mo.EmptyableToOption(errorMessage),
	}, false, errors.Join(providerErr, deliveryErr)
}

// applyOutcome executes the complete behavior for one finalized model outcome.
func (s *Service) applyOutcome(
	ctx context.Context,
	runID string,
	response model.Response,
) (Result, bool, error) {
	outcome, present := response.Outcome.Get()
	if !present {
		err := errors.New("model response has no outcome")
		return s.endTurn(ctx, runID, response, nil, agent.RunOutcomeFailed, err.Error(), err)
	}
	switch outcome {
	case model.OutcomeStop:
		return s.endTurn(ctx, runID, response, nil, agent.RunOutcomeCompleted, "", nil)
	case model.OutcomeToolUse:
		return s.executeCalls(ctx, runID, response)
	case model.OutcomeLength:
		calls := modelToolCalls(response)
		if len(calls) == 0 {
			return s.endTurn(ctx, runID, response, nil, agent.RunOutcomeCompleted, "", nil)
		}
		results := make([]agent.ToolResult, 0, len(calls))
		for _, call := range calls {
			result := agent.ToolResult{
				CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents(lengthCallMessage), IsError: true,
			}
			s.appendToolResult(result)
			results = append(results, result)
			toolResult := newEvent(EventToolResult, runID)
			toolResult.ToolResult = mo.Some(result)
			if err := s.deliver(context.WithoutCancel(ctx), toolResult); err != nil {
				return Result{Outcome: agent.RunOutcomeFailed, AddedHistory: nil, ErrorMessage: mo.Some(err.Error())}, false, err
			}
		}
		_, _, err := s.endTurn(ctx, runID, response, results, 0, "", nil)
		return Result{}, err == nil, err
	case model.OutcomeAborted:
		errorMessage, hasErrorMessage := response.ErrorMessage.Get()
		if !hasErrorMessage || errorMessage == "" {
			errorMessage = abortedModelMessage
		}
		return s.endTurn(
			ctx, runID, response, nil, agent.RunOutcomeAborted,
			errorMessage, errors.New(errorMessage),
		)
	case model.OutcomeFailed:
		errorMessage, hasErrorMessage := response.ErrorMessage.Get()
		if !hasErrorMessage || errorMessage == "" {
			errorMessage = failedModelMessage
		}
		return s.endTurn(
			ctx, runID, response, nil, agent.RunOutcomeFailed,
			errorMessage, errors.New(errorMessage),
		)
	default:
		err := fmt.Errorf("unknown model response outcome %d", outcome)
		return s.endTurn(ctx, runID, response, nil, agent.RunOutcomeFailed, err.Error(), err)
	}
}

// executeCalls runs finalized tool calls sequentially in model order.
func (s *Service) executeCalls(
	ctx context.Context,
	runID string,
	response model.Response,
) (Result, bool, error) {
	results := make([]agent.ToolResult, 0)
	for _, call := range modelToolCalls(response) {
		if err := ctx.Err(); err != nil {
			return s.endTurn(ctx, runID, response, results, agent.RunOutcomeAborted, abortedModelMessage, err)
		}
		toolStart := newEvent(EventToolExecutionStart, runID)
		toolStart.ToolCall = mo.Some(call)
		if err := s.deliver(ctx, toolStart); err != nil {
			return Result{Outcome: agent.RunOutcomeFailed, AddedHistory: nil, ErrorMessage: mo.Some(err.Error())}, false, err
		}
		var progressDeliveryErr error
		result, executeErr := s.tools.Execute(ctx, call, func(progress tool.Progress) error {
			toolUpdate := newEvent(EventToolExecutionUpdate, runID)
			toolUpdate.ToolCall = mo.Some(call)
			toolUpdate.Progress = mo.Some(progress)
			deliveryErr := s.deliver(ctx, toolUpdate)
			if deliveryErr != nil && progressDeliveryErr == nil {
				progressDeliveryErr = deliveryErr
			}
			return deliveryErr
		})
		if executeErr != nil {
			result = agent.ToolResult{
				CallID: call.ID, ToolName: call.Name, Contents: tool.TextContents(executeErr.Error()), IsError: true,
			}
		}
		result.CallID = call.ID
		result.ToolName = call.Name
		s.appendToolResult(result)
		results = append(results, result)
		toolEnd := newEvent(EventToolExecutionEnd, runID)
		toolEnd.ToolCall = mo.Some(call)
		toolEnd.ToolResult = mo.Some(result)
		if err := s.deliver(context.WithoutCancel(ctx), toolEnd); err != nil {
			return Result{Outcome: agent.RunOutcomeFailed, AddedHistory: nil, ErrorMessage: mo.Some(err.Error())}, false, err
		}
		toolResult := newEvent(EventToolResult, runID)
		toolResult.ToolResult = mo.Some(result)
		if err := s.deliver(context.WithoutCancel(ctx), toolResult); err != nil {
			return Result{Outcome: agent.RunOutcomeFailed, AddedHistory: nil, ErrorMessage: mo.Some(err.Error())}, false, err
		}
		if progressDeliveryErr != nil {
			return s.endTurn(
				ctx,
				runID,
				response,
				results,
				agent.RunOutcomeFailed,
				progressDeliveryErr.Error(),
				errors.Join(progressDeliveryErr, executeErr),
			)
		}
		if executeErr != nil && (errors.Is(executeErr, context.Canceled) ||
			errors.Is(executeErr, context.DeadlineExceeded) ||
			ctx.Err() != nil) {
			return s.endTurn(
				ctx,
				runID,
				response,
				results,
				agent.RunOutcomeAborted,
				abortedModelMessage,
				executeErr,
			)
		}
	}
	_, _, err := s.endTurn(ctx, runID, response, results, 0, "", nil)
	return Result{}, err == nil, err
}

// endTurn emits the self-contained terminal turn and selects continuation or completion.
func (s *Service) endTurn(
	ctx context.Context,
	runID string,
	response model.Response,
	results []agent.ToolResult,
	outcome agent.RunOutcome,
	errorMessage string,
	runErr error,
) (Result, bool, error) {
	turn := TurnSummary{Response: cloneModelResponse(response), ToolResults: slices.Clone(results)}
	turnEnd := newEvent(EventTurnEnd, runID)
	turnEnd.Turn = mo.Some(turn)
	if err := s.deliver(context.WithoutCancel(ctx), turnEnd); err != nil {
		return Result{
			Outcome: agent.RunOutcomeFailed, AddedHistory: nil, ErrorMessage: mo.Some(err.Error()),
		}, false, errors.Join(runErr, err)
	}
	if outcome == 0 {
		return Result{}, true, runErr
	}
	return Result{
		Outcome: outcome, AddedHistory: nil, ErrorMessage: mo.EmptyableToOption(errorMessage),
	}, false, runErr
}

// finish emits agent_end and leaves the run awaiting explicit Host settlement.
func (s *Service) finish(
	ctx context.Context,
	runID string,
	startIndex int,
	outcome agent.RunOutcome,
	errorMessage mo.Option[string],
	runErr error,
) (Result, error) {
	s.mutex.Lock()
	added := cloneHistory(s.history[startIndex:])
	s.state = State{
		Status:          StatusAwaitingSettlement,
		RunID:           mo.Some(runID),
		PartialResponse: mo.None[model.Response](),
		ToolPreviews:    nil,
	}
	s.mutex.Unlock()
	result := Result{Outcome: outcome, AddedHistory: added, ErrorMessage: errorMessage}
	agentEnd := newEvent(EventAgentEnd, runID)
	agentEnd.Agent = mo.Some(AgentSummary{
		Outcome: outcome, AddedHistory: added, ErrorMessage: errorMessage,
	})
	eventErr := s.deliver(context.WithoutCancel(ctx), agentEnd)
	return result, errors.Join(runErr, eventErr)
}

// applyStreamEvent exposes typed partial model content in current run state.
func (s *Service) applyStreamEvent(event StreamEvent) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if event.Kind == StreamEventToolCallStart || event.Kind == StreamEventToolCallDelta ||
		event.Kind == StreamEventToolCallEnd {
		return applyToolCallStreamEvent(s.state.ToolPreviews, event)
	}
	if event.Kind == StreamEventDone || event.Kind == StreamEventError {
		clear(s.state.ToolPreviews)
	}
	partial, present := s.state.PartialResponse.Get()
	if !present {
		partial = model.Response{}
	}
	if err := applyStreamEvent(&partial, event); err != nil {
		return err
	}
	s.state.PartialResponse = mo.Some(partial)
	return nil
}

// clearPartial removes provider streaming scratch data.
func (s *Service) clearPartial() {
	s.mutex.Lock()
	s.state.PartialResponse = mo.None[model.Response]()
	clear(s.state.ToolPreviews)
	s.mutex.Unlock()
}

// appendModel stores one finalized model response.
func (s *Service) appendModel(response model.Response) {
	s.mutex.Lock()
	s.history = append(s.history, agent.HistoryEntry{
		Kind:       agent.HistoryEntryModel,
		User:       mo.None[model.Message](),
		Model:      mo.Some(cloneModelResponse(response)),
		ToolResult: mo.None[agent.ToolResult](),
	})
	s.mutex.Unlock()
}

// appendToolResult stores one completed tool call result.
func (s *Service) appendToolResult(result agent.ToolResult) {
	s.mutex.Lock()
	s.history = append(s.history, agent.HistoryEntry{
		Kind:       agent.HistoryEntryToolResult,
		User:       mo.None[model.Message](),
		Model:      mo.None[model.Response](),
		ToolResult: mo.Some(cloneToolResult(result)),
	})
	s.mutex.Unlock()
}

// deliver performs one synchronous Host event call without queuing or retry.
func (s *Service) deliver(ctx context.Context, event Event) error {
	if err := s.events.Deliver(ctx, event); err != nil {
		return fmt.Errorf("deliver agent event: %w", err)
	}
	return nil
}
