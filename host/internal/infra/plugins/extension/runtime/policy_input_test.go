//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestPolicyInputPurposeRejectsUnknownIntent checks the closed policy-purpose contract at the transport boundary.
func TestPolicyInputPurposeRejectsUnknownIntent(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name    string
		purpose extensioncontext.PolicyPurpose
		wire    extensionpb.CompactionPolicyPurpose
		invalid bool
	}{
		{name: "manual", purpose: extensioncontext.PolicyPurposeManual, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_MANUAL, invalid: false},
		{name: "automatic", purpose: extensioncontext.PolicyPurposeAutomatic, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC, invalid: false},
		{name: "unspecified", purpose: 0, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_UNSPECIFIED, invalid: true},
		{name: "unknown", purpose: 99, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_UNSPECIFIED, invalid: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange captured input with the caller's declared preparation intent.
			invocation := extensioncontext.PolicyInvocation{
				Purpose:     scenario.purpose,
				OperationID: "authority",
				LifecycleID: "lifetime",
				Snapshot: extensioncontext.CompactionSnapshot{
					Identity:     session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
					ActiveLeafID: mo.None[string](),
					Entries:      nil,
					History:      nil,
					Previous:     mo.None[session.CompactionEntry](),
				},
				Input: modelexecution.ProviderRequest{
					Instructions:    "instructions",
					Model:           model.Descriptor{},
					ReasoningChoice: model.ReasoningChoiceOff,
					History:         nil,
					Tools:           nil,
				},
				Requests:     nil,
				Generators:   nil,
				Results:      nil,
				Instructions: mo.None[string](),
				RecordState:  nil,
			}
			// Act through the input mapper.
			input, err := mapPolicyInvocation(invocation, "generation")
			// Assert unknown intent cannot be sent as a successful policy invocation.
			if scenario.invalid {
				require.ErrorContains(t, err, "compaction policy purpose is unspecified or unknown")
				require.Nil(t, input)
			} else {
				require.NoError(t, err)
				require.Equal(t, scenario.wire, input.GetPurpose())
			}
		})
	}
}
