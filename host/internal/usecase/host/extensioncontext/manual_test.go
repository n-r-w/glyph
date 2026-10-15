//go:build !integration

// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestScenario checks rejection before session capture.
func (s *manualPolicyRejectsAmbiguousOwnersSuite) TestScenario() {
	t := s.T()
	// Arrange two registered owners and a session that must not be captured.
	controller := gomock.NewController(t)
	runtime := NewMockPolicyRuntime(controller)
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{}).AnyTimes()
	runtime.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{
		{ExtensionID: "first", RuntimeID: "generation-a", HandlerID: "policy"},
		{ExtensionID: "second", RuntimeID: "generation-b", HandlerID: "policy"},
	})
	service := New(NewMockRuntimeState(controller), NewMockSessionState(controller))
	service.policyRuntime = runtime
	service.compactionSessions = NewMockCompactionSessions(controller)

	// Act through the manual policy preparation boundary.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())

	// Assert that no load-order owner is selected.
	require.ErrorContains(t, err, "multiple compaction policy owners")
	require.True(t, result.Committed.IsNone())
}

// TestScenario checks explicit absence without session capture.
func (s *manualPolicyRejectsAbsentOwnerSuite) TestScenario() {
	t := s.T()
	// Arrange a runtime with no registered policy and a session that must not be captured.
	controller := gomock.NewController(t)
	runtime := NewMockPolicyRuntime(controller)
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{}).AnyTimes()
	runtime.EXPECT().SnapshotCompactionPolicies().Return(nil)
	service := New(NewMockRuntimeState(controller), NewMockSessionState(controller))
	service.policyRuntime = runtime
	service.compactionSessions = NewMockCompactionSessions(controller)

	// Act through the manual policy preparation boundary.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.Some("guidance"))

	// Assert that absence is explicit and no replacement exists.
	require.ErrorContains(t, err, "no compaction policy owner is registered")
	require.True(t, result.Committed.IsNone())
	require.False(t, result.Canceled)
}
