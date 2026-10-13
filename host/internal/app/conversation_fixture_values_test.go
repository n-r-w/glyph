//go:build integration

package app

const (
	// conversationObservationEnvironment names the public observation artifact setting.
	conversationObservationEnvironment = "GLYPH_REFERENCE_CONVERSATION_OBSERVATION"
	// conversationModelEnvironment names the observer's nested model request setting.
	conversationModelEnvironment = "GLYPH_REFERENCE_CONVERSATION_MODEL"
	// conversationFailureEnvironment names the observer failure setting.
	conversationFailureEnvironment = "GLYPH_REFERENCE_CONVERSATION_FAILURE"
	// conversationPolicyPIDEnvironment names the policy process ID artifact setting.
	conversationPolicyPIDEnvironment = "GLYPH_REFERENCE_CONVERSATION_PID"
	// conversationAcknowledgeFailureEnvironment names acknowledgement after nested failure.
	conversationAcknowledgeFailureEnvironment = "GLYPH_REFERENCE_CONVERSATION_ACKNOWLEDGE_FAILURE"
	// conversationToolEnvironment names the tool execution artifact setting.
	conversationToolEnvironment = "GLYPH_REFERENCE_CONVERSATION_TOOL"
	// conversationPolicyDisabledEnvironment names the disabled policy capability setting.
	conversationPolicyDisabledEnvironment = "GLYPH_REFERENCE_POLICY_DISABLED"
	// conversationResponseZero identifies provider output with zero-valued usage.
	conversationResponseZero = "zero"
	// conversationResponseLength identifies provider output at the response limit.
	conversationResponseLength = "length"
	// conversationExtensionFailed is the public observation failure category.
	conversationExtensionFailed = "EXTENSION_FAILED"
	// conversationObservationCallGlob is the per-invocation artifact suffix pattern.
	conversationObservationCallGlob = ".call-*"
	// conversationReferenceToolName identifies the fixture's model-callable tool.
	conversationReferenceToolName = "reference_observation_tool"
)
