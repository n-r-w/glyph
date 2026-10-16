package extension

import (
	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// decodePurpose maps compaction purposes into extension policy categories.
func decodePurpose(value pb.CompactionPolicyPurpose) compaction.Purpose {
	switch value {
	case pb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_UNSPECIFIED:
		return compaction.PurposeUnspecified
	case pb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_MANUAL:
		return compaction.PurposeManual
	case pb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC:
		return compaction.PurposeAutomatic
	case pb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW:
		return compaction.PurposeOverflow
	default:
		return compaction.PurposeInvalid
	}
}

// decodeTrigger maps compaction triggers into extension categories.
func decodeTrigger(value pb.CompactionTrigger) compaction.Trigger {
	switch value {
	case pb.CompactionTrigger_COMPACTION_TRIGGER_UNSPECIFIED:
		return compaction.TriggerUnspecified
	case pb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL:
		return compaction.TriggerManual
	case pb.CompactionTrigger_COMPACTION_TRIGGER_THRESHOLD:
		return compaction.TriggerThreshold
	case pb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW:
		return compaction.TriggerOverflow
	default:
		return compaction.TriggerInvalid
	}
}

// encodeTrigger maps a compaction trigger to its public variant.
func encodeTrigger(value compaction.Trigger) pb.CompactionTrigger {
	switch value {
	case compaction.TriggerManual:
		return pb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL
	case compaction.TriggerThreshold:
		return pb.CompactionTrigger_COMPACTION_TRIGGER_THRESHOLD
	case compaction.TriggerOverflow:
		return pb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW
	case compaction.TriggerUnspecified, compaction.TriggerInvalid:
	}
	return pb.CompactionTrigger_COMPACTION_TRIGGER_UNSPECIFIED
}

// decodeRequestDisposition maps a participant's request decision.
func decodeRequestDisposition(value pb.CompactionRequestDisposition) compaction.RequestDisposition {
	switch value {
	case pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_UNSPECIFIED:
		return compaction.RequestUnspecified
	case pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE:
		return compaction.RequestPreserve
	case pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE:
		return compaction.RequestReplace
	default:
		return compaction.RequestInvalid
	}
}

// decodeResultDisposition maps a participant's result decision.
func decodeResultDisposition(value pb.CompactionResultDisposition) compaction.ResultDisposition {
	switch value {
	case pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED:
		return compaction.ResultUnspecified
	case pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE:
		return compaction.ResultPreserve
	case pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE:
		return compaction.ResultReplace
	case pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR:
		return compaction.ResultClear
	default:
		return compaction.ResultInvalid
	}
}

// decodeModality maps a model input kind into extension categories.
func decodeModality(value pb.InputModality) compaction.Modality {
	switch value {
	case pb.InputModality_INPUT_MODALITY_UNSPECIFIED:
		return compaction.ModalityUnspecified
	case pb.InputModality_INPUT_MODALITY_TEXT:
		return compaction.ModalityText
	case pb.InputModality_INPUT_MODALITY_IMAGE:
		return compaction.ModalityImage
	default:
		return compaction.ModalityInvalid
	}
}

// encodeModality maps a model input kind to its public variant.
func encodeModality(value compaction.Modality) pb.InputModality {
	switch value {
	case compaction.ModalityText:
		return pb.InputModality_INPUT_MODALITY_TEXT
	case compaction.ModalityImage:
		return pb.InputModality_INPUT_MODALITY_IMAGE
	case compaction.ModalityUnspecified, compaction.ModalityInvalid:
	}
	return pb.InputModality_INPUT_MODALITY_UNSPECIFIED
}

// decodeVisibility maps client visibility into extension categories.
func decodeVisibility(value pb.ClientVisibility) compaction.Visibility {
	switch value {
	case pb.ClientVisibility_CLIENT_VISIBILITY_UNSPECIFIED:
		return compaction.VisibilityUnspecified
	case pb.ClientVisibility_CLIENT_VISIBILITY_VISIBLE:
		return compaction.VisibilityVisible
	case pb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN:
		return compaction.VisibilityHidden
	default:
		return compaction.VisibilityInvalid
	}
}

// encodeVisibility maps client visibility to its public variant.
func encodeVisibility(value compaction.Visibility) pb.ClientVisibility {
	switch value {
	case compaction.VisibilityVisible:
		return pb.ClientVisibility_CLIENT_VISIBILITY_VISIBLE
	case compaction.VisibilityHidden:
		return pb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN
	case compaction.VisibilityUnspecified, compaction.VisibilityInvalid:
	}
	return pb.ClientVisibility_CLIENT_VISIBILITY_UNSPECIFIED
}
