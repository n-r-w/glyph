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
	Instructions    string
	Model           model.Descriptor
	ReasoningChoice model.ReasoningChoice
	History         []agent.HistoryEntry
	Tools           []tool.Descriptor
}

// RuntimeSelection is one immutable provider request snapshot.
type RuntimeSelection struct {
	Model           model.Descriptor
	ReasoningChoice model.ReasoningChoice
	Provider        ModelProvider
}

// ModelRuntime supplies the active selection immediately before a provider request.
type ModelRuntime interface {
	Current() RuntimeSelection
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
	Kind     StreamEventKind
	Position mo.Option[int]
	Content  mo.Option[model.Content]
	Delta    mo.Option[string]
	Preview  mo.Option[model.ToolCallPreview]
	ToolCall mo.Option[model.ToolCall]
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

// validateStreamEventShape validates the active fields selected by one stream event kind.
func validateStreamEventShape(event StreamEvent) error {
	required, allowed, missingMessage, err := streamEventFieldContract(event.Kind)
	if err != nil {
		return err
	}
	present := presentStreamEventFields(event)
	if present&required != required {
		return errors.New(missingMessage)
	}
	if present&^allowed != 0 {
		return fmt.Errorf("model stream event kind %d has invalid payload fields", event.Kind)
	}
	return nil
}

// streamEventFieldContract returns required and allowed Option fields for one event kind.
func streamEventFieldContract(
	kind StreamEventKind,
) (required, allowed streamEventFields, missingMessage string, err error) {
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

// presentStreamEventFields records Option presence without collapsing valid zero values.
func presentStreamEventFields(event StreamEvent) streamEventFields {
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

// applyStreamEvent applies one semantic stream transition to partial response state.
func applyStreamEvent(partial *model.Response, event StreamEvent) error {
	if err := validateStreamEventShape(event); err != nil {
		return err
	}
	if outcome, present := partial.Outcome.Get(); present && outcome != 0 {
		return errors.New("model stream already terminated")
	}
	if event.Kind == StreamEventDone || event.Kind == StreamEventError {
		return applyTerminalStreamEvent(partial, event.Response)
	}
	position, hasPosition := event.Position.Get()
	if !hasPosition || position < 0 {
		return fmt.Errorf("model stream position %d is invalid", position)
	}
	switch event.Kind {
	case StreamEventContentStart:
		return applyContentStart(partial, position, event.Content)
	case StreamEventTextDelta, StreamEventContentEnd:
		return applyContentUpdate(partial, position, event)
	case StreamEventDone, StreamEventError:
		return errors.New("terminal model stream event reached content handling")
	case StreamEventToolCallStart, StreamEventToolCallDelta, StreamEventToolCallEnd:
		return errors.New("tool-call stream event reached content handling")
	}
	return nil
}

// applyTerminalStreamEvent replaces partial state only after all streamed content is closed.
func applyTerminalStreamEvent(partial *model.Response, responseOption mo.Option[model.Response]) error {
	response, hasResponse := responseOption.Get()
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
		if isStreamedContent(content.Kind) && !content.Final {
			return fmt.Errorf("model content %d is still active", position)
		}
	}
	if err := ValidateTerminalContent(response); err != nil {
		return err
	}
	*partial = response
	return nil
}

// applyContentStart allocates and starts one typed streamed content position.
func applyContentStart(
	partial *model.Response,
	position int,
	contentOption mo.Option[model.Content],
) error {
	eventContent, hasContent := contentOption.Get()
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
	if !isStreamedContent(eventContent.Kind) {
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
func applyContentUpdate(partial *model.Response, position int, event StreamEvent) error {
	eventContent, hasContent := event.Content.Get()
	if !hasContent {
		return fmt.Errorf("model content %d has no stream payload", position)
	}
	if position >= len(partial.Content) {
		return fmt.Errorf("model content %d is not active", position)
	}
	content := &partial.Content[position]
	if !isStreamedContent(content.Kind) || content.Final ||
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

//nolint:gocyclo // The explicit branches validate the closed tool-call event lifecycle.
func applyToolCallStreamEvent(previews map[string]model.ToolCallPreview, event StreamEvent) error {
	if err := validateStreamEventShape(event); err != nil {
		return err
	}
	switch event.Kind {
	case StreamEventToolCallStart:
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
		preview.Fields = clonePreviewFields(preview.Fields)
		previews[preview.CallID] = preview
	case StreamEventToolCallDelta:
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
		preview.Fields = clonePreviewFields(preview.Fields)
		previews[preview.CallID] = preview
	case StreamEventToolCallEnd:
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
	case StreamEventContentStart, StreamEventTextDelta, StreamEventContentEnd,
		StreamEventDone, StreamEventError:
		return fmt.Errorf("event kind %d is not a tool-call stream event", event.Kind)
	}
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

// ValidateTerminalContent validates every content discriminator in a terminal model response.
func ValidateTerminalContent(response model.Response) error {
	for position := range response.Content {
		content := &response.Content[position]
		if err := validateTerminalContentShape(*content); err != nil {
			return fmt.Errorf("terminal model content %d: %w", position, err)
		}
		if isStreamedContent(content.Kind) && !content.Final {
			return fmt.Errorf("terminal model content %d is not final", position)
		}
	}
	return nil
}

// validateTerminalContentShape validates active and inactive payload fields for one content item.
func validateTerminalContentShape(content model.Content) error {
	hasText := content.Text.IsSome()
	hasProviderContext := content.ProviderContext.IsSome()
	hasToolCall := content.ToolCall.IsSome()
	switch content.Kind {
	case model.ContentText, model.ContentRefusal:
		if !hasText || hasProviderContext || hasToolCall {
			return fmt.Errorf("invalid payload fields for kind %d", content.Kind)
		}
	case model.ContentReasoning:
		if (!hasText && !hasProviderContext) || hasToolCall {
			return fmt.Errorf("invalid payload fields for kind %d", content.Kind)
		}
	case model.ContentToolCall:
		if hasText || hasProviderContext || !hasToolCall {
			return fmt.Errorf("invalid payload fields for kind %d", content.Kind)
		}
	default:
		return fmt.Errorf("unknown kind %d", content.Kind)
	}
	return nil
}

// isStreamedContent reports whether one content kind supports ordered text deltas.
func isStreamedContent(kind model.ContentKind) bool {
	return kind == model.ContentText || kind == model.ContentRefusal || kind == model.ContentReasoning
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
