package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// conversationPolicyPIDEnvironment is the environment variable name for the policy process ID artifact path.
const conversationPolicyPIDEnvironment = "GLYPH_REFERENCE_CONVERSATION_PID"

// conversationModelEnvironment is the environment variable name for the nested model request flag.
const conversationModelEnvironment = "GLYPH_REFERENCE_CONVERSATION_MODEL"

// conversationAcknowledgeFailureEnvironment names the flag for acknowledgement after a failed nested request.
const conversationAcknowledgeFailureEnvironment = "GLYPH_REFERENCE_CONVERSATION_ACKNOWLEDGE_FAILURE"

// conversationFailureEnvironment is the environment variable name for the observation owner's failure flag.
const conversationFailureEnvironment = "GLYPH_REFERENCE_CONVERSATION_FAILURE"

// conversationObservationEnvironment is the environment variable name for the public observation artifact path.
const conversationObservationEnvironment = "GLYPH_REFERENCE_CONVERSATION_OBSERVATION"

// conversationObservationOperation acknowledges the completed conversation attempt.
type conversationObservationOperation struct {
	// sequence identifies the received conversation invocation.
	sequence int64
	// input contains the provider-neutral observation.
	input *extensionpb.CompletedConversationObservation
}

var _ extensionsdk.HandleOperation = (*conversationObservationOperation)(nil)

// Run records the observation and acknowledges its completion.
func (o *conversationObservationOperation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	if path := os.Getenv(conversationObservationEnvironment); path != "" {
		if err := writePolicyArtifact(path, o.input); err != nil {
			return nil, err
		}
		const invocationPathFormat = "%s.call-%d"
		if err := writePolicyArtifact(fmt.Sprintf(invocationPathFormat, path, o.sequence), o.input); err != nil {
			return nil, err
		}
	}
	if os.Getenv(conversationModelEnvironment) == fixtureEnabledValue {
		binding, err := extensionsdk.ContextFrom(ctx)
		if err != nil {
			return newPolicyFailure(err), nil
		}
		if modelErr := waitPolicyConfiguredModel(
			ctx,
			binding,
			o.input.GetModel(),
			o.input.GetReasoningChoice(),
		); modelErr != nil && os.Getenv(conversationAcknowledgeFailureEnvironment) != fixtureEnabledValue {
			return nil, modelErr
		}
	}
	if os.Getenv(conversationFailureEnvironment) == fixtureEnabledValue {
		return newPolicyFailure(
			errors.New("reference conversation observer failure: original conversation observer cause"),
		), nil
	}
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	response := new(extensionpb.HandleResponse)
	response.SetCompletedConversation(new(extensionpb.CompactionObserverAction))
	return response, nil
}

// Release ends observation ownership.
func (o *conversationObservationOperation) Release() {}
