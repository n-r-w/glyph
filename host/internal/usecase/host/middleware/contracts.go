// Package middleware owns ordered ordinary model-facing extension operations.
package middleware

import (
	"context"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// Kind identifies the operation family of one accepted middleware handler.
type Kind uint8

const (
	// Headers transforms public request headers.
	Headers Kind = iota + 1
	// Body transforms safe serialized request JSON.
	Body
	// Response observes public provider response metadata.
	Response
	// SystemInstructions transforms the system instructions of one model request.
	SystemInstructions
	// ModelContext transforms the request-local history of one model request.
	ModelContext
)

// String returns the operation name used in failure text.
func (kind Kind) String() string {
	switch kind {
	case Headers:
		return "provider-headers"
	case Body:
		return "provider-body"
	case Response:
		return "provider-response"
	case SystemInstructions:
		return "system-instructions"
	case ModelContext:
		return "model-context"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(kind))
	}
}

// WireOperation identifies the public payload of one provider-wire invocation.
type WireOperation uint8

const (
	// HeadersOperation transforms public request headers.
	HeadersOperation WireOperation = iota + 1
	// BodyOperation transforms safe serialized request JSON.
	BodyOperation
	// ResponseOperation observes public provider response metadata.
	ResponseOperation
)

// Handler identifies one accepted ordinary middleware declaration.
type Handler struct {
	// ExtensionID identifies the accepted extension process.
	ExtensionID string
	// HandlerID identifies the extension-local declaration.
	HandlerID string
	// Kind selects the operation owned by the declaration.
	Kind Kind
}

// Invocation contains detached original/current values for one wire operation.
type Invocation struct {
	// Context binds this invocation to a trusted generation and session.
	Context extension.Context
	// Operation selects the public payload and permitted action.
	Operation WireOperation
	// OriginalHeaders contains the immutable public request headers.
	OriginalHeaders modelexecution.WireHeaders
	// CurrentHeaders contains the headers left by preceding handlers.
	CurrentHeaders modelexecution.WireHeaders
	// OriginalBody contains the immutable safe serialized request.
	OriginalBody []byte
	// CurrentBody contains safe JSON left by preceding handlers.
	CurrentBody []byte
	// Response contains immutable public response metadata.
	Response modelexecution.WireResponse
}

// ActionKind identifies the permitted transformer or observer result.
type ActionKind uint8

const (
	// Preserve retains the current value without serialization changes.
	Preserve ActionKind = iota + 1
	// Replace supplies a complete replacement value.
	Replace
	// Acknowledge accepts immutable response metadata.
	Acknowledge
)

// Action carries a decoded result whose semantics are validated by the owner.
type Action struct {
	// Kind selects preservation, replacement or observation acknowledgement.
	Kind ActionKind
	// Headers contains a complete public header replacement.
	Headers modelexecution.WireHeaders
	// Body contains a complete safe serialized replacement.
	Body []byte
}

// Transformation contains detached immutable original and composed current values for one transformer handler.
type Transformation[T any] struct {
	// Context binds this invocation to a trusted generation and session.
	Context extension.Context
	// Original contains the immutable value of the operation.
	Original T
	// Current contains the value left by preceding handlers.
	Current T
}

// Transformed carries a decoded transformer result whose semantics are validated by the owner.
type Transformed[T any] struct {
	// Kind selects preservation or replacement.
	Kind ActionKind
	// Replacement contains the complete value when Kind is Replace.
	Replacement T
}

//go:generate go tool mockgen -source=contracts.go -destination=contracts_mock.go -package=middleware

// Runtime invokes one exact-generation ordinary middleware operation.
type Runtime interface {
	// HandleWire invokes one handler without acquiring middleware ordering or validation policy.
	HandleWire(context.Context, string, string, Invocation) (Action, error)
	// HandleSystemInstructions invokes one system-instructions transformer.
	HandleSystemInstructions(
		context.Context, string, string, Transformation[string],
	) (Transformed[string], error)
	// HandleModelContext invokes one request-local history transformer and restores retained private replay.
	HandleModelContext(
		context.Context, string, string, Transformation[[]agent.HistoryEntry],
	) (Transformed[[]agent.HistoryEntry], error)
}

// ContextIssuer binds an invocation to the current extension generation and session.
type ContextIssuer interface {
	// IssueContext captures the active runtime generation and session for an extension.
	IssueContext(string) (extension.Context, error)
}
