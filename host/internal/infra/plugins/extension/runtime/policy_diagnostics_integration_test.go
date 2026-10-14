//go:build integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestPolicyCancellationDecisionRetainsDiagnostic checks cancellation metadata beside an independent error.
func TestPolicyCancellationDecisionRetainsDiagnostic(t *testing.T) {
	t.Parallel()
	// Arrange a real peer that completes cancellation with an independent diagnostic.
	peer := startHelperRuntime(t, policyCanceledDiagnosticMode)
	_, err := peer.Register(t.Context())
	require.NoError(t, err)
	invocation := extensioncontext.PolicyInvocation{
		Purpose:     extensioncontext.PolicyPurposeManual,
		OperationID: "policy-diagnostic", LifecycleID: "policy-diagnostic-lifetime",
		Snapshot: extensioncontext.CompactionSnapshot{}, Input: modelexecution.ProviderRequest{},
		Requests: nil, Generators: nil, Results: nil, Instructions: mo.None[string](), RecordState: nil,
	}
	// Act through the production policy decoder over gRPC.
	decision, err := peer.InvokeCompactionPolicy(t.Context(), "policy", runtimeTestContext(), invocation)
	// Assert that the decoder retains both the acquired decision and full failure text.
	require.NoError(t, t.Context().Err())
	require.ErrorContains(t, err, policyIndependentDiagnostic)
	require.Equal(t, extensioncontext.PolicyCanceled, decision)
}
