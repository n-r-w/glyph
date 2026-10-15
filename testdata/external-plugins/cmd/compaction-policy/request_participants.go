package main

import (
	"fmt"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// validateReferenceParticipantCount checks the request-composition fixture's selected participant set.
func validateReferenceParticipantCount(purpose extensionpb.CompactionPolicyPurpose, count int) error {
	if purpose != extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW &&
		count != referenceRequestParticipantCount {
		return fmt.Errorf("reference policy requires five captured request handlers; received %d", count)
	}
	return nil
}
