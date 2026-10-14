// Package modelexecution owns Host logical model execution and raw provider attempts.
package modelexecution

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=modelexecution

// ConversationPreparation contains policy-accepted history and a flag for durable history replacement.
type ConversationPreparation struct {
	// History contains the detached provider input history.
	History []agent.HistoryEntry
	// Committed indicates a durable history replacement during preparation.
	Committed bool
}

// CapturedConversation supplies preparation and observation for one admitted owner.
type CapturedConversation interface {
	// Prepare obtains policy-accepted history for the frozen conversation binding.
	Prepare(context.Context, ProviderRequest) (ConversationPreparation, error)
	// Observe reports the successful provider attempt before logical completion.
	Observe(context.Context, ProviderRequest, model.Response) error
}

// ConversationPolicies captures the optional policy owner for one logical execution.
type ConversationPolicies interface {
	// CaptureConversation retains owner and participant generations.
	CaptureConversation() (mo.Option[CapturedConversation], error)
}

// OverflowRecovery supplies the remaining legacy overflow integration.
type OverflowRecovery interface {
	// RecoverOverflow compacts one rejected agent request before its single changed replacement attempt.
	RecoverOverflow(context.Context, ProviderRequest) (ProviderRequest, error)
}

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
