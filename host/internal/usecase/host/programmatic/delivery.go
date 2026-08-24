package programmatic

import (
	"context"
	"errors"
	"fmt"
	"sync"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

type activeRun struct {
	correlationID string
	runID         string
	cancel        context.CancelFunc
	done          chan struct{}
	err           error
}

// Delivery correlates Host lifecycle delivery with one accepted user request.
type Delivery struct {
	sender Sender

	sendMutex  sync.Mutex
	mutex      sync.Mutex
	active     *activeRun
	operations map[*activeRun]struct{}
}

// NewDelivery creates a synchronous Programmatic Control delivery router.
func NewDelivery(sender Sender) *Delivery {
	return &Delivery{
		sender: sender, sendMutex: sync.Mutex{}, mutex: sync.Mutex{}, active: nil,
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
	if d.active != nil {
		return false
	}
	d.active = active
	d.operations[active] = struct{}{}
	return true
}

func (d *Delivery) release(active *activeRun) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.active == active {
		d.active = nil
	}
	if _, exists := d.operations[active]; exists {
		delete(d.operations, active)
		close(active.done)
	}
}

func (d *Delivery) finish(active *activeRun, err error) {
	d.mutex.Lock()
	active.err = err
	delete(d.operations, active)
	active.cancel()
	close(active.done)
	d.mutex.Unlock()
}

func (d *Delivery) cancelAndWaitAll() error {
	d.mutex.Lock()
	operations := make([]*activeRun, 0, len(d.operations))
	for operation := range d.operations {
		operations = append(operations, operation)
	}
	active := d.active
	if active != nil {
		active.cancel()
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
	cancel := active.cancel
	if d.active != active {
		cancel = nil
	}
	d.mutex.Unlock()
	if cancel != nil {
		cancel()
	}
	<-active.done
	return active.err
}

func (d *Delivery) sendResponse(ctx context.Context, response controller.Response) error {
	d.sendMutex.Lock()
	defer d.sendMutex.Unlock()
	return d.sender.SendResponse(ctx, response)
}

func (d *Delivery) sendEvent(ctx context.Context, event controller.AgentEvent) error {
	d.sendMutex.Lock()
	defer d.sendMutex.Unlock()
	return d.sender.SendEvent(ctx, event)
}

// DeliverAgent forwards one correlated Agent Core event.
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
	if err := d.sendEvent(ctx, mapped); err != nil {
		return fmt.Errorf("deliver Programmatic Control agent event: %w", err)
	}
	return nil
}

// DeliverSettled clears active state and forwards one correlated Host settlement.
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

	settled := emptyAgentEvent()
	settled.CorrelationID = correlationID
	settled.Type = controller.AgentEventAgentSettled
	settled.RunID = runID
	if err := d.sendEvent(ctx, settled); err != nil {
		return fmt.Errorf("deliver Programmatic Control settlement: %w", err)
	}
	return nil
}

func mapAgentEvent(event run.Event) controller.AgentEvent {
	mapped := emptyAgentEvent()
	mapped.Type = mapAgentEventType(event.Type)
	mapped.RunID = event.RunID
	switch event.Type {
	case run.EventAgentStart, run.EventTurnStart, run.EventMessageStart:
	case run.EventContentStart, run.EventTextDelta, run.EventContentEnd:
		mapped.ModelContent = controller.ModelContent{
			Kind: mapModelContentKind(event.Content.Kind), Position: event.Position, Text: "",
		}
		if event.Type == run.EventTextDelta {
			mapped.ModelContent.Text = event.Content.Text
		}
	case run.EventToolCallStart, run.EventToolCallDelta:
		mapped.ToolCallPreview = mapToolCallPreview(event.Preview)
	case run.EventToolCallEnd:
		mapped.FinalToolCall = controller.FinalToolCall{
			CallID: event.ToolCall.ID, Name: event.ToolCall.Name, Position: event.Position,
			Arguments: cloneArguments(event.ToolCall.Arguments),
		}
	case run.EventMessageEnd:
		mapped.ModelResponse = mapModelResponse(event.Message)
	case run.EventToolExecutionStart:
		mapped.ToolExecution = controller.ToolExecution{CallID: event.ToolCall.ID, ToolName: event.ToolCall.Name}
	case run.EventToolExecutionUpdate:
		mapped.ToolProgress = controller.ToolProgress{
			Channel: mapProgressChannel(event.Progress.Channel), Content: event.Progress.Content,
		}
	case run.EventToolExecutionEnd, run.EventToolResult:
		mapped.ToolResult = mapToolResult(event.ToolResult)
	case run.EventTurnEnd:
		toolResults := make([]controller.ToolResult, len(event.Turn.ToolResults))
		for index, result := range event.Turn.ToolResults {
			toolResults[index] = mapToolResult(result)
		}
		mapped.Turn = controller.TurnSummary{
			Response: mapModelResponse(event.Turn.Response), ToolResults: toolResults,
		}
	case run.EventAgentEnd:
		mapped.Agent = controller.AgentSummary{
			Outcome: mapRunOutcome(event.Agent.Outcome), ErrorMessage: event.Agent.ErrorMessage,
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

func emptyAgentEvent() controller.AgentEvent {
	return controller.AgentEvent{
		CorrelationID: "", Type: controller.AgentEventUnspecified, RunID: "",
		ModelContent: controller.ModelContent{Kind: controller.ModelContentUnspecified, Position: 0, Text: ""},
		ToolCallPreview: controller.ToolCallPreview{
			CallID: "", Name: "", Position: 0, Provisional: false, Fields: nil,
		},
		FinalToolCall: controller.FinalToolCall{CallID: "", Name: "", Position: 0, Arguments: nil},
		ToolExecution: controller.ToolExecution{CallID: "", ToolName: ""},
		ToolProgress:  controller.ToolProgress{Channel: controller.ProgressChannelUnspecified, Content: ""},
		ToolResult:    controller.ToolResult{CallID: "", ToolName: "", Contents: nil, IsError: false},
		ModelResponse: emptyModelResponse(),
		Turn:          controller.TurnSummary{Response: emptyModelResponse(), ToolResults: nil},
		Agent:         controller.AgentSummary{Outcome: controller.RunOutcomeUnspecified, ErrorMessage: ""},
	}
}
