package plugin

// PayloadKind selects one validated input group without introducing a service interface for data.
type PayloadKind uint8

const (
	// PayloadUnspecified identifies an invalid or missing payload tag.
	PayloadUnspecified PayloadKind = iota
	// PayloadAgent carries model or tool lifecycle data.
	PayloadAgent
	// PayloadText carries a diagnostic or authorization URL.
	PayloadText
	// PayloadAvailability carries Host admission state.
	PayloadAvailability
	// PayloadSelection carries a confirmed model selection.
	PayloadSelection
	// PayloadSettled marks completed agent submission.
	PayloadSettled
	// PayloadSession carries a session query or replacement result.
	PayloadSession
	// PayloadTree carries tree progress or a terminal tree result.
	PayloadTree
)

// Payload contains one typed input group selected by Kind, not a display-state aggregate.
type Payload struct {
	// Kind identifies the only active input group.
	Kind PayloadKind
	// Agent contains lifecycle facts for PayloadAgent.
	Agent AgentUpdate
	// Text contains message data for PayloadText.
	Text TextUpdate
	// Availability contains Host admission state for PayloadAvailability.
	Availability Availability
	// Selection contains committed model selection for PayloadSelection.
	Selection ModelSelection
	// Session contains query or replacement data for PayloadSession.
	Session SessionUpdate
	// Tree contains tree-operation facts for PayloadTree.
	Tree TreeUpdate
}

// NewPayload initializes a tagged value with no unrelated input data.
func NewPayload(kind PayloadKind) Payload {
	return Payload{
		Kind: kind, Agent: AgentUpdate{}, Text: TextUpdate{}, Availability: AvailabilityUnspecified,
		Selection: ModelSelection{}, Session: SessionUpdate{}, Tree: TreeUpdate{},
	}
}

// AgentPayload constructs the validated lifecycle input group.
func AgentPayload(update AgentUpdate) Payload {
	payload := NewPayload(PayloadAgent)
	payload.Agent = update
	return payload
}

// TextPayload constructs the validated diagnostic or authorization input group.
func TextPayload(update TextUpdate) Payload {
	payload := NewPayload(PayloadText)
	payload.Text = update
	return payload
}

// AvailabilityPayload constructs a validated Host admission-state input.
func AvailabilityPayload(availability Availability) Payload {
	payload := NewPayload(PayloadAvailability)
	payload.Availability = availability
	return payload
}

// SelectionPayload constructs a validated committed model-selection input.
func SelectionPayload(selection ModelSelection) Payload {
	payload := NewPayload(PayloadSelection)
	payload.Selection = selection
	return payload
}

// SessionPayload constructs a validated session-result input group.
func SessionPayload(update SessionUpdate) Payload {
	payload := NewPayload(PayloadSession)
	payload.Session = update
	return payload
}

// TreePayload constructs a validated tree-operation input group.
func TreePayload(update TreeUpdate) Payload {
	payload := NewPayload(PayloadTree)
	payload.Tree = update
	return payload
}
