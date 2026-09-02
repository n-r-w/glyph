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
	// delivery owns this operation and its event stream.
	delivery *Delivery
	// operationID identifies the active operation.
	operationID string
	// runID identifies the prepared Agent Core run.
	runID string
	// coordinator starts and cancels the prepared run.
	coordinator Coordinator
	// userText contains the submitted user request.
	userText string
	// runContext controls Agent Core execution.
	runContext context.Context
	// cancel requests run cancellation.
	cancel context.CancelFunc
	// events publishes agent progress events.
	events chan controller.AgentEvent
	// streamDone closes when event delivery has stopped.
	streamDone chan struct{}
	// done closes when the operation has finished.
	done chan struct{}
	// state identifies the operation lifecycle state.
	state operationState
	// streamStopped reports whether streamDone was closed.
	streamStopped bool
	// err contains the terminal operation failure.
	err error
}

// Start begins the prepared run at most once.
func (a *activeRun) Start() {
	a.delivery.start(a)
}

// Events returns the operation's agent progress stream.
func (a *activeRun) Events() <-chan controller.AgentEvent {
	return a.events
}

// Delivery routes lifecycle and progress events for one user request operation.
type Delivery struct {
	// mutex protects delivery lifecycle state.
	mutex sync.Mutex
	// closed reports whether delivery has shut down.
	closed bool
	// active contains the currently running operation.
	active *activeRun
	// operations contains accepted operations awaiting completion.
	operations map[*activeRun]struct{}
}

// NewDelivery creates a Programmatic Control agent-run delivery router.
func NewDelivery() *Delivery {
	return &Delivery{
		mutex:      sync.Mutex{},
		closed:     false,
		active:     nil,
		operations: make(map[*activeRun]struct{}),
	}
}

// activeSnapshot returns the current run while holding delivery synchronization.
func (d *Delivery) activeSnapshot() *activeRun {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.active
}

// reserve registers an accepted operation as the active run.
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

// start transitions an accepted operation to running and starts progress delivery.
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

// finish records terminal state and releases active run resources.
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

// finishAcceptedLocked closes an operation that never entered running state.
func (d *Delivery) finishAcceptedLocked(active *activeRun) {
	if d.active == active {
		d.active = nil
	}
	if active.state == operationAccepted {
		active.coordinator.CancelPrepared(active.runID)
	}
	active.state = operationFinished
	delete(d.operations, active)
	active.cancel()
	d.stopStreamLocked(active)
	close(active.events)
	close(active.done)
}

// stopStreamLocked cancels progress delivery exactly once.
func (d *Delivery) stopStreamLocked(active *activeRun) {
	if active.streamStopped {
		return
	}
	active.streamStopped = true
	close(active.streamDone)
}

// cancelAndWait requests run cancellation and waits for terminal cleanup.
func (d *Delivery) cancelAndWait(active *activeRun) error {
	d.mutex.Lock()
	if active.state == operationAccepted {
		d.finishAcceptedLocked(active)
		d.mutex.Unlock()
		return nil
	}
	if d.active == active {
		d.stopStreamLocked(active)
		active.cancel()
	}
	d.mutex.Unlock()

	<-active.done
	return active.err
}

// emit maps and publishes one operation progress event.
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

// DeliverAgent forwards one Agent Core event with controller backpressure.
func (d *Delivery) DeliverAgent(ctx context.Context, event run.Event) error {
	d.mutex.Lock()
	active := d.active
	if active == nil || active.runID != event.RunID {
		d.mutex.Unlock()
		return fmt.Errorf("deliver Programmatic Control agent event: inactive Host run %q", event.RunID)
	}
	operationID := active.operationID
	d.mutex.Unlock()

	mapped, err := mapAgentEvent(event)
	if err != nil {
		return fmt.Errorf("deliver Programmatic Control agent event: %w", err)
	}
	mapped.OperationID = operationID
	if emitErr := d.emit(ctx, active, mapped); emitErr != nil {
		return fmt.Errorf("deliver Programmatic Control agent event: %w", emitErr)
	}
	return nil
}

// DeliverSettled clears active state and delivers the coordinator-owned settlement.
func (d *Delivery) DeliverSettled(_ context.Context, runID string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	active := d.active
	if active == nil || active.runID != runID {
		return fmt.Errorf("deliver Programmatic Control settlement: inactive Host run %q", runID)
	}
	// Settlement is represented by the operation terminal event, not public progress.
	d.active = nil
	return nil
}

// mapAgentEvent maps one Agent Core event to Programmatic operation progress.
func mapAgentEvent(event run.Event) (controller.AgentEvent, error) {
	mapped := controller.AgentEvent{
		OperationID:     "",
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
		response, err := mapModelResponseProjection(message)
		if err != nil {
			return err
		}
		mapped.ModelResponse = mo.Some(response)
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
			CallID: call.ID, Name: call.Name, Position: position, Arguments: call.Clone().Arguments,
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
		response, err := mapModelResponseProjection(turn.Response)
		if err != nil {
			return err
		}
		mapped.Turn = mo.Some(controller.TurnSummary{Response: response, ToolResults: toolResults})
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

// mapAgentEventType maps nonterminal Agent Core event types.
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

// mapTerminalAgentEventType maps terminal Agent Core event types.
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
