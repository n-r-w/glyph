package run

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=run

// ModelRequest contains projected history and the available tool catalog.
type ModelRequest struct {
	// Instructions contains the system prompt.
	Instructions string
	// Model contains the immutable configured model snapshot.
	Model model.Descriptor
	// ReasoningChoice identifies the selected reasoning behavior.
	ReasoningChoice model.ReasoningChoice
	// History contains projected provider-neutral conversation history.
	History []agent.HistoryEntry
	// Tools contains the available model-callable tool catalog.
	Tools []tool.Descriptor
}

// RequestSnapshot is one immutable model request snapshot.
type RequestSnapshot struct {
	// Model contains the immutable configured model snapshot.
	Model model.Descriptor
	// ReasoningChoice identifies the selected reasoning behavior.
	ReasoningChoice model.ReasoningChoice
	// Provider executes the selected model request.
	Provider ModelProvider
}

// ErrPersistenceUnavailable classifies a history mutation that cannot become durable.
var ErrPersistenceUnavailable = errors.New("session persistence failed")

// HistoryStore owns canonical provider-neutral history for the active session.
type HistoryStore interface {
	// Snapshot returns an immutable copy that includes complete process-local history.
	Snapshot() []agent.HistoryEntry
	// Append transfers one owned history entry before dependent work starts.
	Append(context.Context, agent.HistoryEntry) error
}

// ModelRuntime supplies an immutable snapshot immediately before each model request.
type ModelRuntime interface {
	// Snapshot returns the active model descriptor, reasoning choice, and provider.
	Snapshot() RequestSnapshot
}

// StreamEventKind identifies one provider-neutral model stream transition.
type StreamEventKind uint8

const (
	// StreamEventContentStart starts one text content block.
	StreamEventContentStart StreamEventKind = iota + 1
	// StreamEventTextDelta appends text to an active content block.
	StreamEventTextDelta
	// StreamEventContentEnd finalizes one text content block.
	StreamEventContentEnd
	// StreamEventToolCallStart starts one provisional function call.
	StreamEventToolCallStart
	// StreamEventToolCallDelta replaces one provisional function-call preview.
	StreamEventToolCallDelta
	// StreamEventToolCallEnd carries exact final function-call arguments.
	StreamEventToolCallEnd
	// StreamEventDone carries one successful terminal response.
	StreamEventDone
	// StreamEventError carries one failed or aborted terminal response.
	StreamEventError
)

// StreamEvent is one ordered provider-neutral model stream transition.
type StreamEvent struct {
	// Kind identifies the stream transition and active payload.
	Kind StreamEventKind
	// Position identifies the response content block order.
	Position mo.Option[int]
	// Content contains one typed model content block.
	Content mo.Option[model.Content]
	// Delta contains an exact model text fragment.
	Delta mo.Option[string]
	// Preview contains provisional tool call state.
	Preview mo.Option[model.ToolCallPreview]
	// ToolCall contains one finalized tool request.
	ToolCall mo.Option[model.ToolCall]
	// Response contains the authoritative terminal response.
	Response mo.Option[model.Response]
}

// StreamHandler consumes model stream transitions in provider order.
type StreamHandler func(event StreamEvent) error

// streamEventFields is a presence mask for optional StreamEvent payload fields.
type streamEventFields uint8

const (
	streamEventFieldPosition streamEventFields = 1 << iota
	streamEventFieldContent
	streamEventFieldDelta
	streamEventFieldPreview
	streamEventFieldToolCall
	streamEventFieldResponse
)

// validateShape validates the active fields selected by the stream event kind.
func (event StreamEvent) validateShape() error {
	required, allowed, missingMessage, err := event.Kind.fieldContract()
	if err != nil {
		return err
	}
	present := event.presentFields()
	if present&required != required {
		return errors.New(missingMessage)
	}
	if present&^allowed != 0 {
		return fmt.Errorf("model stream event kind %d has invalid payload fields", event.Kind)
	}
	return nil
}

// fieldContract returns required and allowed Option fields for one event kind.
func (kind StreamEventKind) fieldContract() (
	required, allowed streamEventFields,
	missingMessage string,
	err error,
) {
	switch kind {
	case StreamEventContentStart, StreamEventContentEnd:
		fields := streamEventFieldPosition | streamEventFieldContent
		return fields, fields, "model content stream event requires position and content", nil
	case StreamEventTextDelta:
		fields := streamEventFieldPosition | streamEventFieldContent | streamEventFieldDelta
		return fields, fields, "model text delta requires position, content, and text delta", nil
	case StreamEventToolCallStart:
		fields := streamEventFieldPosition | streamEventFieldPreview
		return fields, fields, "tool-call start requires preview and position", nil
	case StreamEventToolCallDelta:
		fields := streamEventFieldPosition | streamEventFieldPreview
		return fields, fields, "tool-call delta requires preview and position", nil
	case StreamEventToolCallEnd:
		fields := streamEventFieldPosition | streamEventFieldToolCall
		return fields, fields, "tool-call end requires tool call and position", nil
	case StreamEventDone, StreamEventError:
		return streamEventFieldResponse, streamEventFieldResponse,
			"terminal model stream event requires an outcome", nil
	default:
		return 0, 0, "", fmt.Errorf("unsupported model stream event kind %d", kind)
	}
}

// presentFields records Option presence without collapsing valid zero values.
func (event StreamEvent) presentFields() streamEventFields {
	var fields streamEventFields
	if event.Position.IsSome() {
		fields |= streamEventFieldPosition
	}
	if event.Content.IsSome() {
		fields |= streamEventFieldContent
	}
	if event.Delta.IsSome() {
		fields |= streamEventFieldDelta
	}
	if event.Preview.IsSome() {
		fields |= streamEventFieldPreview
	}
	if event.ToolCall.IsSome() {
		fields |= streamEventFieldToolCall
	}
	if event.Response.IsSome() {
		fields |= streamEventFieldResponse
	}
	return fields
}

// applyTo applies one semantic stream transition to partial response state.
func (event StreamEvent) applyTo(partial *model.Response) error {
	if err := event.validateShape(); err != nil {
		return err
	}
	if outcome, present := partial.Outcome.Get(); present && outcome != 0 {
		return errors.New("model stream already terminated")
	}
	if event.Kind == StreamEventDone || event.Kind == StreamEventError {
		return event.applyTerminalTo(partial)
	}
	position, hasPosition := event.Position.Get()
	if !hasPosition || position < 0 {
		return fmt.Errorf("model stream position %d is invalid", position)
	}
	switch event.Kind {
	case StreamEventContentStart:
		return event.applyContentStart(partial, position)
	case StreamEventTextDelta, StreamEventContentEnd:
		return event.applyContentUpdate(partial, position)
	case StreamEventDone, StreamEventError:
		return errors.New("terminal model stream event reached content handling")
	case StreamEventToolCallStart, StreamEventToolCallDelta, StreamEventToolCallEnd:
		return errors.New("tool-call stream event reached content handling")
	}
	return nil
}

// applyTerminalTo replaces partial state only after all streamed content is closed.
func (event StreamEvent) applyTerminalTo(partial *model.Response) error {
	response, hasResponse := event.Response.Get()
	if !hasResponse {
		return errors.New("terminal model stream event requires an outcome")
	}
	outcome, hasOutcome := response.Outcome.Get()
	if !hasOutcome || outcome == 0 {
		return errors.New("terminal model stream event requires an outcome")
	}
	// Reject provider enum drift at the terminal boundary before shared state changes.
	switch outcome {
	case model.OutcomeStop, model.OutcomeToolUse, model.OutcomeLength, model.OutcomeAborted, model.OutcomeFailed:
	default:
		return fmt.Errorf("unsupported terminal model outcome %d", outcome)
	}
	for position := range partial.Content {
		content := &partial.Content[position]
		if content.Kind.Streamed() && !content.Final {
			return fmt.Errorf("model content %d is still active", position)
		}
	}
	if err := response.ValidateTerminalContent(); err != nil {
		return err
	}
	*partial = response
	return nil
}

// applyContentStart allocates and starts one typed streamed content position.
func (event StreamEvent) applyContentStart(partial *model.Response, position int) error {
	eventContent, hasContent := event.Content.Get()
	if !hasContent {
		return fmt.Errorf("model content %d has no start payload", position)
	}
	for len(partial.Content) <= position {
		partial.Content = append(partial.Content, model.Content{})
	}
	content := &partial.Content[position]
	if content.Kind != 0 {
		return fmt.Errorf("model content %d already started", position)
	}
	if !eventContent.Kind.Streamed() {
		return fmt.Errorf("model content %d has unsupported stream kind %d", position, eventContent.Kind)
	}
	*content = model.Content{
		Kind:            eventContent.Kind,
		Text:            mo.Some(""),
		Final:           false,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.None[model.ToolCall](),
	}
	return nil
}

// applyContentUpdate appends one present delta or closes one active content position.
func (event StreamEvent) applyContentUpdate(partial *model.Response, position int) error {
	eventContent, hasContent := event.Content.Get()
	if !hasContent {
		return fmt.Errorf("model content %d has no stream payload", position)
	}
	if position >= len(partial.Content) {
		return fmt.Errorf("model content %d is not active", position)
	}
	content := &partial.Content[position]
	if !content.Kind.Streamed() || content.Final ||
		(eventContent.Kind != 0 && eventContent.Kind != content.Kind) {
		return fmt.Errorf("model content %d is not active", position)
	}
	if event.Kind == StreamEventContentEnd {
		content.Final = true
		return nil
	}
	delta, hasDelta := event.Delta.Get()
	if !hasDelta {
		return fmt.Errorf("model content %d has no text delta", position)
	}
	text, hasText := content.Text.Get()
	if !hasText {
		return fmt.Errorf("model content %d has no accumulated text", position)
	}
	content.Text = mo.Some(text + delta)
	return nil
}

// applyToolCallTo applies one tool-call stream transition.
func (event StreamEvent) applyToolCallTo(previews map[string]model.ToolCallPreview) error {
	if err := event.validateShape(); err != nil {
		return err
	}
	switch event.Kind {
	case StreamEventToolCallStart:
		return event.applyToolCallStart(previews)
	case StreamEventToolCallDelta:
		return event.applyToolCallDelta(previews)
	case StreamEventToolCallEnd:
		return event.applyToolCallEnd(previews)
	case StreamEventContentStart, StreamEventTextDelta, StreamEventContentEnd,
		StreamEventDone, StreamEventError:
		return fmt.Errorf("event kind %d is not a tool-call stream event", event.Kind)
	}
	return nil
}

// applyToolCallStart validates and stores one provisional tool call.
func (event StreamEvent) applyToolCallStart(previews map[string]model.ToolCallPreview) error {
	preview, hasPreview := event.Preview.Get()
	position, hasPosition := event.Position.Get()
	if !hasPreview || !hasPosition || position != preview.Position || preview.CallID == "" || preview.Name == "" ||
		preview.Position < 0 || !preview.Provisional {
		return errors.New("tool-call start requires provisional identity")
	}
	if _, exists := previews[preview.CallID]; exists {
		return fmt.Errorf("tool call %q already started", preview.CallID)
	}
	if err := validateToolCallPreviewFields(preview.Fields); err != nil {
		return err
	}
	previews[preview.CallID] = preview.Clone()
	return nil
}

// applyToolCallDelta validates and replaces one active provisional tool call.
func (event StreamEvent) applyToolCallDelta(previews map[string]model.ToolCallPreview) error {
	preview, hasPreview := event.Preview.Get()
	if !hasPreview {
		return errors.New("tool-call delta requires preview")
	}
	position, hasPosition := event.Position.Get()
	active, exists := previews[preview.CallID]
	if !hasPosition || position != preview.Position || !exists || preview.Name != active.Name ||
		preview.Position != active.Position || !preview.Provisional {
		return fmt.Errorf("tool call %q is not active", preview.CallID)
	}
	if err := validateToolCallPreviewFields(preview.Fields); err != nil {
		return err
	}
	previews[preview.CallID] = preview.Clone()
	return nil
}

// applyToolCallEnd validates and removes one finalized tool call.
func (event StreamEvent) applyToolCallEnd(previews map[string]model.ToolCallPreview) error {
	toolCall, hasToolCall := event.ToolCall.Get()
	if !hasToolCall {
		return errors.New("tool-call end requires tool call")
	}
	position, hasPosition := event.Position.Get()
	active, exists := previews[toolCall.ID]
	if !hasPosition || !exists || toolCall.Name != active.Name || position != active.Position {
		return fmt.Errorf("tool call %q is not active", toolCall.ID)
	}
	delete(previews, toolCall.ID)
	return nil
}

// validateToolCallPreviewFields checks that each discriminator selects exactly one present payload.
func validateToolCallPreviewFields(fields []model.ToolCallPreviewField) error {
	for index, field := range fields {
		switch field.Kind {
		case model.ToolCallPreviewFieldComplete:
			if field.Value.IsNone() || field.Prefix.IsSome() {
				return fmt.Errorf("tool-call preview field %d has invalid complete content", index)
			}
		case model.ToolCallPreviewFieldPrefix:
			if field.Prefix.IsNone() || field.Value.IsSome() {
				return fmt.Errorf("tool-call preview field %d has invalid prefix content", index)
			}
		default:
			return fmt.Errorf("tool-call preview field %d has unknown kind %d", index, field.Kind)
		}
	}
	return nil
}

// ModelProvider streams one provider-neutral response.
type ModelProvider interface {
	Stream(ctx context.Context, request ModelRequest, handle StreamHandler) error
}

// ToolRuntime exposes the Host tool catalog and execution gateway.
type ToolRuntime interface {
	Tools() []tool.Descriptor
	Execute(
		ctx context.Context,
		call model.ToolCall,
		handleProgress tool.ProgressHandler,
	) (agent.ToolResult, error)
}

// EventSink receives Agent Core lifecycle events synchronously.
type EventSink interface {
	Deliver(ctx context.Context, event Event) error
}
