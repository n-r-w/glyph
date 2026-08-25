// Package programmatic maps Host operations and events to the Programmatic Control API.
package programmatic

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/samber/lo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

type operationState uint8

const (
	operationAccepted operationState = iota
	operationRunning
	operationFinished
)

type activeRun struct {
	delivery      *Delivery
	correlationID string
	runID         string
	coordinator   Coordinator
	userText      string
	runContext    context.Context
	cancel        context.CancelFunc
	events        chan controller.AgentEvent
	streamDone    chan struct{}
	done          chan struct{}
	state         operationState
	streamStopped bool
	err           error
}

var _ controller.Operation = (*activeRun)(nil)

// Start begins the prepared run at most once.
func (a *activeRun) Start() {
	a.delivery.start(a)
}

// Events returns the operation's synchronous event stream.
func (a *activeRun) Events() <-chan controller.AgentEvent {
	return a.events
}

// Delivery correlates Host lifecycle delivery with one accepted user request.
type Delivery struct {
	mutex      sync.Mutex
	closed     bool
	active     *activeRun
	operations map[*activeRun]struct{}
}

// NewDelivery creates a synchronous Programmatic Control delivery router.
func NewDelivery() *Delivery {
	return &Delivery{
		mutex:      sync.Mutex{},
		closed:     false,
		active:     nil,
		operations: make(map[*activeRun]struct{}),
	}
}

func (d *Delivery) activeSnapshot() *activeRun {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.active
}

func (d *Delivery) reserve(active *activeRun) bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.closed || d.active != nil {
		return false
	}
	d.active = active
	d.operations[active] = struct{}{}
	return true
}

func (d *Delivery) start(active *activeRun) {
	d.mutex.Lock()
	if active.state != operationAccepted {
		d.mutex.Unlock()
		return
	}
	active.state = operationRunning
	d.mutex.Unlock()

	go func() {
		outcome, runErr := active.coordinator.RunPrepared(
			active.runContext, active.runID, active.userText,
		)
		d.finish(active, filterRunError(outcome, runErr))
	}()
}

func (d *Delivery) finish(active *activeRun, err error) {
	d.mutex.Lock()
	if active.state == operationFinished {
		d.mutex.Unlock()
		return
	}
	active.state = operationFinished
	active.err = err
	delete(d.operations, active)
	active.cancel()
	d.stopStreamLocked(active)
	close(active.events)
	close(active.done)
	d.mutex.Unlock()
}

func (d *Delivery) finishAcceptedLocked(active *activeRun) {
	if d.active == active {
		d.active = nil
	}
	active.state = operationFinished
	delete(d.operations, active)
	active.cancel()
	d.stopStreamLocked(active)
	close(active.events)
	close(active.done)
}

func (d *Delivery) stopStreamLocked(active *activeRun) {
	if active.streamStopped {
		return
	}
	active.streamStopped = true
	close(active.streamDone)
}

func (d *Delivery) cancelAndWaitAll() error {
	d.mutex.Lock()
	d.closed = true
	operations := make([]*activeRun, 0, len(d.operations))
	for operation := range d.operations {
		operations = append(operations, operation)
		d.stopStreamLocked(operation)
		if operation.state == operationAccepted {
			d.finishAcceptedLocked(operation)
			continue
		}
		operation.cancel()
	}
	d.mutex.Unlock()

	errs := make([]error, 0, len(operations))
	for _, operation := range operations {
		<-operation.done
		errs = append(errs, operation.err)
	}
	return errors.Join(errs...)
}

func (d *Delivery) cancelAndWait(active *activeRun) error {
	d.mutex.Lock()
	if active.state == operationAccepted {
		d.finishAcceptedLocked(active)
		d.mutex.Unlock()
		return nil
	}
	if d.active == active {
		active.cancel()
	}
	d.mutex.Unlock()

	<-active.done
	return active.err
}

func (d *Delivery) emit(
	ctx context.Context,
	active *activeRun,
	event controller.AgentEvent,
) error {
	select {
	case active.events <- event:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-active.streamDone:
		return context.Canceled
	}
}

// DeliverAgent forwards one correlated Agent Core event with controller backpressure.
func (d *Delivery) DeliverAgent(ctx context.Context, event run.Event) error {
	d.mutex.Lock()
	active := d.active
	if active == nil || active.runID != event.RunID {
		d.mutex.Unlock()
		return fmt.Errorf("deliver Programmatic Control agent event: inactive Host run %q", event.RunID)
	}
	correlationID := active.correlationID
	d.mutex.Unlock()

	mapped := mapAgentEvent(event)
	mapped.CorrelationID = correlationID
	if err := d.emit(ctx, active, mapped); err != nil {
		return fmt.Errorf("deliver Programmatic Control agent event: %w", err)
	}
	return nil
}

// DeliverSettled clears active state and delivers the coordinator-owned settlement.
func (d *Delivery) DeliverSettled(ctx context.Context, runID string) error {
	d.mutex.Lock()
	active := d.active
	if active == nil || active.runID != runID {
		d.mutex.Unlock()
		return fmt.Errorf("deliver Programmatic Control settlement: inactive Host run %q", runID)
	}
	d.active = nil
	correlationID := active.correlationID
	d.mutex.Unlock()

	settled := controller.AgentEvent{
		CorrelationID:   correlationID,
		Type:            controller.AgentEventAgentSettled,
		RunID:           runID,
		ModelContent:    controller.ModelContent{},
		ToolCallPreview: controller.ToolCallPreview{},
		FinalToolCall:   controller.FinalToolCall{},
		ToolExecution:   controller.ToolExecution{},
		ToolProgress:    controller.ToolProgress{},
		ToolResult:      controller.ToolResult{},
		ModelResponse:   controller.ModelResponse{},
		Turn:            controller.TurnSummary{},
		Agent:           controller.AgentSummary{},
	}
	if err := d.emit(ctx, active, settled); err != nil {
		return fmt.Errorf("deliver Programmatic Control settlement: %w", err)
	}
	return nil
}

func mapAgentEvent(event run.Event) controller.AgentEvent {
	mapped := controller.AgentEvent{
		CorrelationID:   "",
		Type:            mapAgentEventType(event.Type),
		RunID:           event.RunID,
		ModelContent:    controller.ModelContent{},
		ToolCallPreview: controller.ToolCallPreview{},
		FinalToolCall:   controller.FinalToolCall{},
		ToolExecution:   controller.ToolExecution{},
		ToolProgress:    controller.ToolProgress{},
		ToolResult:      controller.ToolResult{},
		ModelResponse:   controller.ModelResponse{},
		Turn:            controller.TurnSummary{},
		Agent:           controller.AgentSummary{},
	}
	switch event.Type {
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart:
	case run.EventContentStart, run.EventTextDelta, run.EventContentEnd:
		mapped.ModelContent = controller.ModelContent{
			Kind:     mapModelContentKind(event.Content.OrEmpty().Kind),
			Position: event.Position.OrEmpty(),
			Text:     "",
		}
		if event.Type == run.EventTextDelta {
			mapped.ModelContent.Text = event.Content.OrEmpty().Text.OrEmpty()
		}
	case run.EventToolCallStart, run.EventToolCallDelta:
		mapped.ToolCallPreview = mapToolCallPreview(event.Preview.OrEmpty())
	case run.EventToolCallEnd:
		mapped.FinalToolCall = controller.FinalToolCall{
			CallID:    event.ToolCall.OrEmpty().ID,
			Name:      event.ToolCall.OrEmpty().Name,
			Position:  event.Position.OrEmpty(),
			Arguments: cloneArguments(event.ToolCall.OrEmpty().Arguments),
		}
	case run.EventMessageEnd:
		mapped.ModelResponse = mapModelResponse(event.Message.OrEmpty())
	case run.EventToolExecutionStart:
		mapped.ToolExecution = controller.ToolExecution{
			CallID:   event.ToolCall.OrEmpty().ID,
			ToolName: event.ToolCall.OrEmpty().Name,
		}
	case run.EventToolExecutionUpdate:
		mapped.ToolProgress = controller.ToolProgress{
			Channel: mapProgressChannel(event.Progress.OrEmpty().Channel),
			Content: event.Progress.OrEmpty().Content,
		}
	case run.EventToolExecutionEnd, run.EventToolResult:
		mapped.ToolResult = mapToolResult(event.ToolResult.OrEmpty())
	case run.EventTurnEnd:
		toolResults := lo.Map(event.Turn.OrEmpty().ToolResults, func(result agent.ToolResult, _ int) controller.ToolResult {
			return mapToolResult(result)
		})
		mapped.Turn = controller.TurnSummary{
			Response:    mapModelResponse(event.Turn.OrEmpty().Response),
			ToolResults: toolResults,
		}
	case run.EventAgentEnd:
		mapped.Agent = controller.AgentSummary{
			Outcome:      mapRunOutcome(event.Agent.OrEmpty().Outcome),
			ErrorMessage: event.Agent.OrEmpty().ErrorMessage.OrEmpty(),
		}
	}
	return mapped
}

func mapAgentEventType(eventType run.EventType) controller.AgentEventType {
	switch eventType {
	case run.EventAgentStart:
		return controller.AgentEventAgentStart
	case run.EventTurnStart:
		return controller.AgentEventTurnStart
	case run.EventMessageStart:
		return controller.AgentEventMessageStart
	case run.EventContentStart:
		return controller.AgentEventModelContentStart
	case run.EventTextDelta:
		return controller.AgentEventModelTextDelta
	case run.EventContentEnd:
		return controller.AgentEventModelContentEnd
	case run.EventToolCallStart:
		return controller.AgentEventToolCallStart
	case run.EventToolCallDelta:
		return controller.AgentEventToolCallDelta
	case run.EventToolCallEnd,
		run.EventMessageEnd,
		run.EventToolExecutionStart,
		run.EventToolExecutionUpdate,
		run.EventToolExecutionEnd,
		run.EventToolResult,
		run.EventTurnEnd,
		run.EventAgentEnd:
		return mapTerminalAgentEventType(eventType)
	default:
		return controller.AgentEventUnspecified
	}
}

func mapTerminalAgentEventType(eventType run.EventType) controller.AgentEventType {
	switch eventType {
	case run.EventToolCallEnd:
		return controller.AgentEventToolCallEnd
	case run.EventMessageEnd:
		return controller.AgentEventMessageEnd
	case run.EventToolExecutionStart:
		return controller.AgentEventToolExecutionStart
	case run.EventToolExecutionUpdate:
		return controller.AgentEventToolExecutionUpdate
	case run.EventToolExecutionEnd:
		return controller.AgentEventToolExecutionEnd
	case run.EventToolResult:
		return controller.AgentEventToolResult
	case run.EventTurnEnd:
		return controller.AgentEventTurnEnd
	case run.EventAgentEnd:
		return controller.AgentEventAgentEnd
	case run.EventAgentStart,
		run.EventTurnStart,
		run.EventMessageStart,
		run.EventContentStart,
		run.EventTextDelta,
		run.EventContentEnd,
		run.EventToolCallStart,
		run.EventToolCallDelta:
		return controller.AgentEventUnspecified
	}
	return controller.AgentEventUnspecified
}
