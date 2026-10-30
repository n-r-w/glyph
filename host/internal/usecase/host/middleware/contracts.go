// Package middleware owns ordered ordinary model-facing extension operations.
package middleware

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// Kind identifies one public wire operation.
type Kind uint8

const (
	// Headers transforms public request headers.
	Headers Kind = iota + 1
	// Body transforms safe serialized request JSON.
	Body
	// Response observes public provider response metadata.
	Response
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
	// Kind selects the public payload and permitted action.
	Kind Kind
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

//go:generate go tool mockgen -source=contracts.go -destination=contracts_mock.go -package=middleware

// Runtime invokes one exact-generation ordinary middleware operation.
type Runtime interface {
	// HandleWire invokes one handler without acquiring middleware ordering or validation policy.
	HandleWire(context.Context, string, string, Invocation) (Action, error)
}

// ContextIssuer binds an invocation to the current extension generation and session.
type ContextIssuer interface {
	// IssueContext captures the active runtime generation and session for an extension.
	IssueContext(string) (extension.Context, error)
}
