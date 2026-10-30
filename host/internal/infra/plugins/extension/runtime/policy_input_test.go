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
		name     string
		purpose  extensioncontext.PolicyPurpose
		wire     extensionpb.CompactionPolicyPurpose
		invalid  bool
		overflow mo.Option[string]
	}{
		{name: "manual", purpose: extensioncontext.PolicyPurposeManual, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_MANUAL, invalid: false, overflow: mo.None[string]()},
		{name: "automatic", purpose: extensioncontext.PolicyPurposeAutomatic, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC, invalid: false, overflow: mo.None[string]()},
		{name: "unspecified", purpose: 0, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_UNSPECIFIED, invalid: true, overflow: mo.None[string]()},
		{name: "unknown", purpose: 99, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_UNSPECIFIED, invalid: true, overflow: mo.None[string]()},
		{name: "overflow", purpose: extensioncontext.PolicyPurposeOverflow, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW, invalid: false, overflow: mo.Some("provider overflow\nsource α")},
		{name: "overflow empty source", purpose: extensioncontext.PolicyPurposeOverflow, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW, invalid: false, overflow: mo.Some("")},
		{name: "overflow without source", purpose: extensioncontext.PolicyPurposeOverflow, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW, invalid: true, overflow: mo.None[string]()},
		{name: "automatic with source", purpose: extensioncontext.PolicyPurposeAutomatic, wire: extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC, invalid: true, overflow: mo.Some("provider overflow")},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange captured input with the caller's declared preparation intent.
			invocation := extensioncontext.PolicyInvocation{
				Purpose:       scenario.purpose,
				OverflowError: scenario.overflow,
				OperationID:   "authority",
				LifecycleID:   "lifetime",
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
					Tools:           nil, Wire: mo.None[modelexecution.WireCallbacks](),
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
				require.Error(t, err)
				if scenario.purpose == 0 || scenario.purpose == 99 {
					require.ErrorContains(t, err, "compaction policy purpose is unspecified or unknown")
				}
				require.Nil(t, input)
			} else {
				require.NoError(t, err)
				require.Equal(t, scenario.wire, input.GetPurpose())
				require.Equal(t, scenario.overflow.IsSome(), input.HasOverflowError())
				require.Equal(t, scenario.overflow.OrEmpty(), input.GetOverflowError())
			}
		})
	}
}
