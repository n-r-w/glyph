package run

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// ModelRequest contains projected history and the available tool catalog.
type ModelRequest struct {
	Instructions string
	History      []agent.HistoryEntry
	Tools        []tool.Descriptor
}

// ModelUpdate is one streamed text fragment at one content-item position.
type ModelUpdate struct {
	Position int
	Delta    string
}

// ModelUpdateHandler consumes model text in stream order.
type ModelUpdateHandler func(update ModelUpdate) error

// ModelProvider streams and finalizes one provider-neutral response.
type ModelProvider interface {
	Generate(ctx context.Context, request ModelRequest, handleUpdate ModelUpdateHandler) (agent.ModelResponse, error)
}

// ToolRuntime exposes the Host tool catalog and execution gateway.
type ToolRuntime interface {
	Tools() []tool.Descriptor
	Execute(
		ctx context.Context,
		call agent.ToolCall,
		handleProgress tool.ProgressHandler,
	) (agent.ToolResult, error)
}

// EventSink receives Agent Core lifecycle events synchronously.
type EventSink interface {
	Deliver(ctx context.Context, event Event) error
}

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=run
