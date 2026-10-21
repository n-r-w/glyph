package compaction

// Purpose identifies why the caller requested policy preparation.
type Purpose string

const (
	// PurposeUnspecified identifies absent preparation intent.
	PurposeUnspecified Purpose = ""
	// PurposeManual identifies explicit manual compaction.
	PurposeManual Purpose = "manual"
	// PurposeAutomatic identifies ordinary conversation preparation.
	PurposeAutomatic Purpose = "automatic"
	// PurposeOverflow identifies context-limit recovery preparation.
	PurposeOverflow Purpose = "overflow"
	// PurposeInvalid identifies an unrecognized preparation intent.
	PurposeInvalid Purpose = "invalid"
)

// RequestIntent supplies the compaction trigger and retry intent.
func (p Purpose) RequestIntent() (Trigger, bool) {
	switch p {
	case PurposeManual:
		return TriggerManual, false
	case PurposeAutomatic:
		return TriggerThreshold, false
	case PurposeOverflow:
		return TriggerOverflow, true
	case PurposeUnspecified, PurposeInvalid:
		return TriggerUnspecified, false
	}
	return TriggerInvalid, false
}

// Trigger identifies the compaction request's intent.
type Trigger string

const (
	// TriggerUnspecified identifies absent compaction intent.
	TriggerUnspecified Trigger = ""
	// TriggerManual identifies explicit compaction.
	TriggerManual Trigger = "manual"
	// TriggerThreshold identifies capacity-triggered compaction.
	TriggerThreshold Trigger = "threshold"
	// TriggerOverflow identifies provider context-limit recovery.
	TriggerOverflow Trigger = "overflow"
	// TriggerInvalid identifies an unrecognized compaction intent.
	TriggerInvalid Trigger = "invalid"
)

// Valid reports whether the trigger identifies a compaction intent.
func (t Trigger) Valid() bool {
	return t == TriggerManual || t == TriggerThreshold || t == TriggerOverflow
}

// RequestDisposition identifies an update to the composed request.
type RequestDisposition string

const (
	// RequestUnspecified identifies an absent request decision.
	RequestUnspecified RequestDisposition = ""
	// RequestPreserve is the decision to retain the accepted request.
	RequestPreserve RequestDisposition = "preserve"
	// RequestReplace is the decision to install a replacement request.
	RequestReplace RequestDisposition = "replace"
	// RequestInvalid identifies an unrecognized request decision.
	RequestInvalid RequestDisposition = "invalid"
)

// ResultDisposition identifies an update to the composed result.
type ResultDisposition string

const (
	// ResultUnspecified identifies an absent result decision.
	ResultUnspecified ResultDisposition = ""
	// ResultPreserve is the decision to retain the accepted result.
	ResultPreserve ResultDisposition = "preserve"
	// ResultReplace is the decision to install a replacement result.
	ResultReplace ResultDisposition = "replace"
	// ResultClear is the decision to remove an optional result.
	ResultClear ResultDisposition = "clear"
	// ResultInvalid identifies an unrecognized result decision.
	ResultInvalid ResultDisposition = "invalid"
)

// Modality identifies a model's accepted input kind.
type Modality string

const (
	// ModalityUnspecified identifies an absent input kind.
	ModalityUnspecified Modality = ""
	// ModalityText identifies textual input.
	ModalityText Modality = "text"
	// ModalityImage identifies image input.
	ModalityImage Modality = "image"
	// ModalityInvalid identifies an unrecognized input kind.
	ModalityInvalid Modality = "invalid"
)
