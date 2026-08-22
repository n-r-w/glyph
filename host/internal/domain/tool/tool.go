// Package tool defines provider-neutral tools and their execution values.
package tool

import "fmt"

// Descriptor describes one model-callable tool.
type Descriptor struct {
	Name                string
	Description         string
	InputSchemaJSON     []byte
	ConstrainedSampling ConstrainedSampling
}

// ConstrainedSamplingKind identifies one provider-neutral constrained generation request.
type ConstrainedSamplingKind uint8

const (
	// ConstrainedSamplingJSONSchema requests JSON Schema constrained generation.
	ConstrainedSamplingJSONSchema ConstrainedSamplingKind = iota + 1
	// ConstrainedSamplingGrammar requests grammar constrained generation.
	ConstrainedSamplingGrammar
)

// JSONSchemaStrictness controls whether a provider may fall back from strict generation.
type JSONSchemaStrictness uint8

const (
	// JSONSchemaStrictPrefer permits provider fallback when strict generation is unavailable.
	JSONSchemaStrictPrefer JSONSchemaStrictness = iota + 1
	// JSONSchemaStrictRequire rejects providers that cannot guarantee strict generation.
	JSONSchemaStrictRequire
)

// GrammarVariants contains equivalent grammar definitions for supported formats.
type GrammarVariants struct {
	Lark  string
	Regex string
}

// ConstrainedSampling describes one optional provider-side input constraint.
type ConstrainedSampling struct {
	Kind                 ConstrainedSamplingKind
	JSONSchemaStrictness JSONSchemaStrictness
	Grammar              GrammarVariants
	GrammarInputProperty string
}

// ProgressChannel identifies the meaning of one progress fragment.
type ProgressChannel uint8

const (
	// ProgressChannelStatus carries human-readable lifecycle status.
	ProgressChannelStatus ProgressChannel = iota + 1
	// ProgressChannelStdout carries command standard output.
	ProgressChannelStdout
	// ProgressChannelStderr carries command standard error.
	ProgressChannelStderr
)

// Progress is one ordered tool-execution progress fragment.
type Progress struct {
	Channel ProgressChannel
	Content string
}

// Result is one terminal tool-execution outcome.
type Result struct {
	Content string
	IsError bool
}

// ProgressHandler consumes progress in execution order.
type ProgressHandler func(progress Progress) error

// RuntimeUnavailableCondition classifies why a registered extension became unavailable.
type RuntimeUnavailableCondition uint8

const (
	// RuntimeUnavailableProcessExited means the extension process ended after successful startup.
	RuntimeUnavailableProcessExited RuntimeUnavailableCondition = iota + 1
)

// RuntimeFailure identifies one post-start extension availability failure.
type RuntimeFailure struct {
	PluginID  string
	Condition RuntimeUnavailableCondition
}

// Message returns the safe user-visible runtime failure text.
func (failure RuntimeFailure) Message() (string, error) {
	if failure.Condition != RuntimeUnavailableProcessExited {
		return "", fmt.Errorf("unknown runtime unavailability condition %d", failure.Condition)
	}
	return fmt.Sprintf("extension %s unavailable: extension process exited", failure.PluginID), nil
}
