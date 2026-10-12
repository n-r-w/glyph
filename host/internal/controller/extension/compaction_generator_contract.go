package extension

// CompactionGeneratorCommand contains one policy-selected generator invocation.
type CompactionGeneratorCommand struct {
	// OperationID identifies the policy authority.
	OperationID string
	// Participant identifies the captured generator.
	Participant CompactionParticipant
	// Original contains the immutable initial request.
	Original CompactionRequestValue
	// Current contains the final composed request.
	Current CompactionRequestValue
}
