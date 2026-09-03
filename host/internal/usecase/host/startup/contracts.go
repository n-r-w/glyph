package startup

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// Directory identifies the effective extension catalog and its failure policy.
type Directory struct {
	// Path is the effective extension catalog directory.
	Path string
	// Explicit reports whether the invocation supplied Path.
	Explicit bool
}

// Issue reports one isolated catalog, registration, or runtime failure.
type Issue struct {
	// PluginIDs identifies affected extension plugins.
	PluginIDs []string
	// Path identifies the failed catalog entry.
	Path string
	// Err contains the complete isolated failure.
	Err error
}

// RawConstrainedSamplingKind identifies one protocol constraint payload.
type RawConstrainedSamplingKind uint8

const (
	// RawConstrainedSamplingMissing reports a constraint with no selected configuration.
	RawConstrainedSamplingMissing RawConstrainedSamplingKind = iota
	// RawConstrainedSamplingJSONSchema reports a JSON Schema constraint.
	RawConstrainedSamplingJSONSchema
	// RawConstrainedSamplingGrammar reports a grammar constraint.
	RawConstrainedSamplingGrammar
	// RawConstrainedSamplingInvalid reports an unknown protocol configuration.
	RawConstrainedSamplingInvalid
)

// RawJSONSchemaStrictness preserves JSON Schema constraint strictness before policy validation.
type RawJSONSchemaStrictness int32

const (
	// RawJSONSchemaStrictnessUnspecified reports an omitted public strictness value.
	RawJSONSchemaStrictnessUnspecified RawJSONSchemaStrictness = 0
	// RawJSONSchemaStrictnessPrefer requests strict JSON Schema output when supported.
	RawJSONSchemaStrictnessPrefer RawJSONSchemaStrictness = 1
	// RawJSONSchemaStrictnessRequire requires strict JSON Schema output.
	RawJSONSchemaStrictnessRequire RawJSONSchemaStrictness = 2
)

// RawGrammar contains the protocol grammar variants before policy validation.
type RawGrammar struct {
	// Present reports whether the selected grammar payload exists.
	Present bool
	// Lark contains the optional Lark grammar exactly as registered.
	Lark mo.Option[string]
	// Regex contains the optional regular expression exactly as registered.
	Regex mo.Option[string]
}

// RawConstrainedSampling contains one protocol constraint before tool policy validation.
type RawConstrainedSampling struct {
	// Kind identifies the selected protocol configuration.
	Kind RawConstrainedSamplingKind
	// JSONSchemaPresent reports whether the selected JSON Schema payload exists.
	JSONSchemaPresent bool
	// JSONSchemaStrictness preserves the public enum value.
	JSONSchemaStrictness RawJSONSchemaStrictness
	// Grammar contains the selected grammar payload.
	Grammar RawGrammar
}

// RawToolDescriptor contains transport-mapped tool registration data.
type RawToolDescriptor struct {
	// Present reports whether the descriptor payload exists.
	Present bool
	// Name is the registered local tool name.
	Name string
	// Description is the model-visible tool description.
	Description string
	// InputSchemaJSON is the registered input schema bytes.
	InputSchemaJSON []byte
	// ConstrainedSampling contains the optional registered constraint.
	ConstrainedSampling mo.Option[RawConstrainedSampling]
}

// RawHandlerKind preserves the public handler kind before policy validation.
type RawHandlerKind int32

const (
	// RawHandlerKindUnspecified reports an omitted public handler kind.
	RawHandlerKindUnspecified RawHandlerKind = 0
	// RawHandlerKindSessionBeforeTreeRequest identifies a navigation request handler.
	RawHandlerKindSessionBeforeTreeRequest RawHandlerKind = 1
	// RawHandlerKindSessionBeforeTreeResult identifies a summary result handler.
	RawHandlerKindSessionBeforeTreeResult RawHandlerKind = 2
	// RawHandlerKindSessionTree identifies a committed navigation observer.
	RawHandlerKindSessionTree RawHandlerKind = 3
)

// RawHandlerDescriptor contains transport-mapped handler registration data.
type RawHandlerDescriptor struct {
	// Present reports whether the descriptor payload exists.
	Present bool
	// ID is the extension-local handler identifier.
	ID string
	// Kind preserves the public handler enum value.
	Kind RawHandlerKind
}

// PendingRegistration identifies one started runtime and its raw registration.
type PendingRegistration struct {
	// ID identifies the extension plugin.
	ID string
	// Path is the extension executable path.
	Path string
	// Tools contains raw tool descriptors in registration order.
	Tools []RawToolDescriptor
	// Handlers contains raw handler descriptors in registration order.
	Handlers []RawHandlerDescriptor
}

// AcceptedHandler identifies one validated handler registration.
type AcceptedHandler struct {
	// ID is the extension-local handler identifier.
	ID string
	// Kind is the validated public handler kind.
	Kind RawHandlerKind
}

// AcceptedRegistration identifies one registration accepted by every capability owner.
type AcceptedRegistration struct {
	// ID identifies the extension plugin.
	ID string
	// Path is the extension executable path.
	Path string
	// Tools contains validated tool descriptors.
	Tools []tool.Descriptor
	// Handlers contains validated handlers in registration order.
	Handlers []AcceptedHandler
}

// PendingLoad contains isolated load issues and every runtime awaiting acceptance.
type PendingLoad struct {
	// Issues contains discovery, startup, and registration transport failures.
	Issues []Issue
	// Registrations contains every successfully registered pending runtime.
	Registrations []PendingRegistration
}

// LoadReport contains isolated failures and every available loaded extension.
type LoadReport struct {
	// Issues contains isolated catalog, registration, and runtime failures.
	Issues []Issue
	// Extensions contains every available loaded extension.
	Extensions []AcceptedRegistration
}

//go:generate go tool mockgen -source=contracts.go -destination=contracts_mock.go -package=startup

// RuntimeLoader owns pending runtime creation, rejection, and activation.
type RuntimeLoader interface {
	LoadPending(ctx context.Context, directory Directory) (PendingLoad, error)
	RejectPending(pluginIDs []string)
	Accept(registrations []AcceptedRegistration)
}

// ToolRegistrar owns tool registration validation and publication.
type ToolRegistrar interface {
	ValidateLocal(registration PendingRegistration) ([]tool.Descriptor, error)
	Conflicts(registrations []AcceptedRegistration) []Issue
	Commit(registrations []AcceptedRegistration)
}

// HandlerRegistrar owns handler registration validation and publication.
type HandlerRegistrar interface {
	ValidateHandlers(registration PendingRegistration) ([]AcceptedHandler, error)
	CommitHandlers(registrations []AcceptedRegistration)
}
