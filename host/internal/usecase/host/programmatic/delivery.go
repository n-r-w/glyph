// Package programmatic maps Host operations and events to the Programmatic Control API.
package programmatic

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/samber/lo"
	"github.com/samber/mo"

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
	case <-active.streamDone:
		return nil
	default:
	}

	select {
	case active.events <- event:
		return nil
	case <-active.streamDone:
		return nil
	case <-ctx.Done():
		// Owner teardown closes streamDone before it cancels the run context.
		select {
		case <-active.streamDone:
			return nil
		default:
			return context.Cause(ctx)
		}
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

	mapped, err := mapAgentEvent(event)
	if err != nil {
		return fmt.Errorf("deliver Programmatic Control agent event: %w", err)
	}
	mapped.CorrelationID = correlationID
	if emitErr := d.emit(ctx, active, mapped); emitErr != nil {
		return fmt.Errorf("deliver Programmatic Control agent event: %w", emitErr)
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
		ModelContent:    mo.None[controller.ModelContent](),
		ToolCallPreview: mo.None[controller.ToolCallPreview](),
		FinalToolCall:   mo.None[controller.FinalToolCall](),
		ToolExecution:   mo.None[controller.ToolExecution](),
		ToolProgress:    mo.None[controller.ToolProgress](),
		ToolResult:      mo.None[controller.ToolResult](),
		ModelResponse:   mo.None[controller.ModelResponse](),
		Turn:            mo.None[controller.TurnSummary](),
		Agent:           mo.None[controller.AgentSummary](),
	}
	if err := d.emit(ctx, active, settled); err != nil {
		return fmt.Errorf("deliver Programmatic Control settlement: %w", err)
	}
	return nil
}

func mapAgentEvent(event run.Event) (controller.AgentEvent, error) {
	mapped := controller.AgentEvent{
		CorrelationID:   "",
		Type:            mapAgentEventType(event.Type),
		RunID:           event.RunID,
		ModelContent:    mo.None[controller.ModelContent](),
		ToolCallPreview: mo.None[controller.ToolCallPreview](),
		FinalToolCall:   mo.None[controller.FinalToolCall](),
		ToolExecution:   mo.None[controller.ToolExecution](),
		ToolProgress:    mo.None[controller.ToolProgress](),
		ToolResult:      mo.None[controller.ToolResult](),
		ModelResponse:   mo.None[controller.ModelResponse](),
		Turn:            mo.None[controller.TurnSummary](),
		Agent:           mo.None[controller.AgentSummary](),
	}
	var err error
	switch event.Type {
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart:
	case run.EventContentStart, run.EventTextDelta, run.EventContentEnd, run.EventMessageEnd:
		err = mapProgrammaticModelEvent(event, &mapped)
	case run.EventToolCallStart, run.EventToolCallDelta, run.EventToolCallEnd,
		run.EventToolExecutionStart, run.EventToolExecutionUpdate, run.EventToolExecutionEnd, run.EventToolResult:
		err = mapProgrammaticToolEvent(event, &mapped)
	case run.EventTurnEnd, run.EventAgentEnd:
		err = mapProgrammaticTerminalEvent(event, &mapped)
	}
	if err != nil {
		return controller.AgentEvent{}, err
	}
	return mapped, nil
}

// mapProgrammaticModelEvent maps selected model payloads to Programmatic Control.
func mapProgrammaticModelEvent(event run.Event, mapped *controller.AgentEvent) error {
	switch event.Type {
	case run.EventContentStart, run.EventTextDelta, run.EventContentEnd:
		content, hasContent := event.Content.Get()
		position, hasPosition := event.Position.Get()
		if !hasContent || !hasPosition {
			return fmt.Errorf("event type %d requires content and position", event.Type)
		}
		text := mo.None[string]()
		if event.Type == run.EventTextDelta {
			value, present := content.Text.Get()
			if !present {
				return errors.New("text delta event requires text")
			}
			text = mo.Some(value)
		}
		mapped.ModelContent = mo.Some(controller.ModelContent{
			Kind: mapModelContentKind(content.Kind), Position: position, Text: text,
		})
	case run.EventMessageEnd:
		message, present := event.Message.Get()
		if !present {
			return errors.New("message end event requires model response")
		}
		mapped.ModelResponse = mo.Some(mapModelResponse(message))
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart,
		run.EventToolCallStart, run.EventToolCallDelta, run.EventToolCallEnd,
		run.EventToolExecutionStart, run.EventToolExecutionUpdate, run.EventToolExecutionEnd,
		run.EventToolResult, run.EventTurnEnd, run.EventAgentEnd:
		return fmt.Errorf("unsupported Programmatic Control model event type %d", event.Type)
	}
	return nil
}

// mapProgrammaticToolEvent maps selected tool payloads to Programmatic Control.
func mapProgrammaticToolEvent(event run.Event, mapped *controller.AgentEvent) error {
	switch event.Type {
	case run.EventToolCallStart, run.EventToolCallDelta:
		preview, present := event.Preview.Get()
		if !present {
			return fmt.Errorf("event type %d requires tool call preview", event.Type)
		}
		mapped.ToolCallPreview = mo.Some(mapToolCallPreview(preview))
	case run.EventToolCallEnd:
		call, hasCall := event.ToolCall.Get()
		position, hasPosition := event.Position.Get()
		if !hasCall || !hasPosition {
			return errors.New("tool call end event requires tool call and position")
		}
		mapped.FinalToolCall = mo.Some(controller.FinalToolCall{
			CallID: call.ID, Name: call.Name, Position: position, Arguments: cloneArguments(call.Arguments),
		})
	case run.EventToolExecutionStart:
		call, present := event.ToolCall.Get()
		if !present {
			return errors.New("tool execution start event requires tool call")
		}
		mapped.ToolExecution = mo.Some(controller.ToolExecution{CallID: call.ID, ToolName: call.Name})
	case run.EventToolExecutionUpdate:
		progress, present := event.Progress.Get()
		if !present {
			return errors.New("tool execution update event requires progress")
		}
		mapped.ToolProgress = mo.Some(controller.ToolProgress{
			Channel: mapProgressChannel(progress.Channel), Content: progress.Content,
		})
	case run.EventToolExecutionEnd, run.EventToolResult:
		result, present := event.ToolResult.Get()
		if !present {
			return fmt.Errorf("event type %d requires tool result", event.Type)
		}
		mapped.ToolResult = mo.Some(mapToolResult(result))
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart,
		run.EventContentStart, run.EventTextDelta, run.EventContentEnd, run.EventMessageEnd,
		run.EventTurnEnd, run.EventAgentEnd:
		return fmt.Errorf("unsupported Programmatic Control tool event type %d", event.Type)
	}
	return nil
}

// mapProgrammaticTerminalEvent maps selected terminal summaries to Programmatic Control.
func mapProgrammaticTerminalEvent(event run.Event, mapped *controller.AgentEvent) error {
	switch event.Type {
	case run.EventTurnEnd:
		turn, present := event.Turn.Get()
		if !present {
			return errors.New("turn end event requires turn summary")
		}
		toolResults := lo.Map(turn.ToolResults, func(result agent.ToolResult, _ int) controller.ToolResult {
			return mapToolResult(result)
		})
		mapped.Turn = mo.Some(controller.TurnSummary{Response: mapModelResponse(turn.Response), ToolResults: toolResults})
	case run.EventAgentEnd:
		summary, present := event.Agent.Get()
		if !present {
			return errors.New("agent end event requires agent summary")
		}
		mapped.Agent = mo.Some(controller.AgentSummary{
			Outcome: mapRunOutcome(summary.Outcome), ErrorMessage: summary.ErrorMessage,
		})
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart,
		run.EventContentStart, run.EventTextDelta, run.EventContentEnd,
		run.EventToolCallStart, run.EventToolCallDelta, run.EventToolCallEnd, run.EventMessageEnd,
		run.EventToolExecutionStart, run.EventToolExecutionUpdate, run.EventToolExecutionEnd, run.EventToolResult:
		return fmt.Errorf("unsupported Programmatic Control terminal event type %d", event.Type)
	}
	return nil
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
