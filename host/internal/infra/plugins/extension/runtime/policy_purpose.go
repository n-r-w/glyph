package runtime

import (
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// mapPolicyPurpose encodes preparation intent and the rejected attempt's optional source text.
func mapPolicyPurpose(
	purpose extensioncontext.PolicyPurpose,
	overflow mo.Option[string],
) (extensionpb.CompactionPolicyPurpose, *string, error) {
	var wire extensionpb.CompactionPolicyPurpose
	switch purpose {
	case extensioncontext.PolicyPurposeManual:
		wire = extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_MANUAL
	case extensioncontext.PolicyPurposeAutomatic:
		wire = extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC
	case extensioncontext.PolicyPurposeOverflow:
		wire = extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW
	default:
		return wire, nil, errors.New("compaction policy purpose is unspecified or unknown")
	}
	if (purpose == extensioncontext.PolicyPurposeOverflow) != overflow.IsSome() {
		return wire, nil, errors.New("compaction policy overflow purpose and source error presence disagree")
	}
	var text *string
	if value, present := overflow.Get(); present {
		text = new(value)
	}
	return wire, text, nil
}
