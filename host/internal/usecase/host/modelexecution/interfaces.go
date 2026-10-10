package modelexecution

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// ManualCompactionResult contains the durable outcome of an admitted manual operation.
type ManualCompactionResult struct {
	// Committed contains the persisted replacement receipt.
	Committed mo.Option[session.Entry]
	// Canceled reports policy cancellation before persistence.
	Canceled bool
}

// ManualCompactionPreparation supplies extension-owned manual replacement.
type ManualCompactionPreparation interface {
	// CompactManualPolicy invokes the policy with captured model input and manual guidance.
	CompactManualPolicy(context.Context, ProviderRequest, mo.Option[string]) (ManualCompactionResult, error)
	// ValidateContext validates the session-bound initiator.
	ValidateContext(string, string, extension.ContextRef) error
}

// ManualCompactionTools supplies the active conversation tool definitions.
type ManualCompactionTools interface {
	// Tools captures the active tool catalog.
	Tools() []tool.Descriptor
}
