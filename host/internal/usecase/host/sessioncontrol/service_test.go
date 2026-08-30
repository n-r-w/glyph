package sessioncontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// TestCreateReturnsBusyAndPreservesActiveSession verifies gate rejection prevents active-session replacement.
func TestCreateReturnsBusyAndPreservesActiveSession(t *testing.T) {
	t.Parallel()

	// Arrange session control with a gate that rejects acquisition and no active-session expectation.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	navigator := NewMockNavigator(controller)
	gate := NewMockOperationGate(controller)
	gate.EXPECT().TryAcquire().Return(nil, false)
	service := New(active, navigator, gate)

	// Act by requesting creation while the gate is owned.
	_, err := service.Create(t.Context())

	// Assert the busy error is returned without invoking active replacement.
	require.ErrorIs(t, err, session.ErrBusy)
}

// TestNavigateReturnsBusyWithoutReadingNavigator verifies gate rejection prevents all navigation work.
func TestNavigateReturnsBusyWithoutReadingNavigator(t *testing.T) {
	t.Parallel()

	// Arrange a gate that rejects acquisition and a navigator with no expectations.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	navigator := NewMockNavigator(controller)
	gate := NewMockOperationGate(controller)
	gate.EXPECT().TryAcquire().Return(nil, false)
	service := New(active, navigator, gate)

	// Act by requesting navigation while another operation owns the gate.
	_, err := service.Navigate(t.Context(), testNavigationRequest())

	// Assert busy is returned before the navigator can read or mutate session state.
	require.ErrorIs(t, err, session.ErrBusy)
}

// TestNavigateReleasesGateOnEveryNavigatorResult verifies successful and failed navigation both release ownership.
func TestNavigateReleasesGateOnEveryNavigatorResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		navigationErr error
	}{
		{name: "success", navigationErr: nil},
		{name: "failure", navigationErr: errors.New("navigation failed")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange an acquired gate and a navigator that observes ownership during its call.
			controller := gomock.NewController(t)
			navigator := NewMockNavigator(controller)
			gate := NewMockOperationGate(controller)
			released := false
			gate.EXPECT().TryAcquire().Return(func() { released = true }, true)
			active := NewMockActiveSessions(controller)
			navigator.EXPECT().NavigateTree(gomock.Any(), testNavigationRequest()).DoAndReturn(
				func(_ context.Context, _ sessionnavigation.Request) (sessionnavigation.Result, error) {
					require.False(t, released)
					return sessionnavigation.Result{
						Canceled: false, Tree: session.Tree{}, ActiveLeafID: mo.Some("destination"),
						ActiveBranch: nil, NextInput: mo.None[string](), Issues: nil,
					}, test.navigationErr
				},
			)
			service := New(active, navigator, gate)

			// Act through the facade.
			_, err := service.Navigate(t.Context(), testNavigationRequest())

			// Assert the terminal result is preserved and cleanup always releases the gate.
			if test.navigationErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.navigationErr)
			}
			require.True(t, released)
		})
	}
}

// TestNavigateReturnsCommittedSnapshots verifies navigation returns tree and active-branch state after commit.
func TestNavigateReturnsCommittedSnapshots(t *testing.T) {
	t.Parallel()

	// Arrange a successful navigation and committed active-session snapshots.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	navigator := NewMockNavigator(controller)
	gate := NewMockOperationGate(controller)
	tree, err := session.NewTree(nil, mo.None[string](), nil)
	require.NoError(t, err)
	branch := []session.Entry{}
	gate.EXPECT().TryAcquire().Return(func() {}, true)
	navigator.EXPECT().NavigateTree(gomock.Any(), testNavigationRequest()).Return(sessionnavigation.Result{
		Canceled: false, Tree: tree, ActiveLeafID: mo.None[string](), ActiveBranch: branch,
		NextInput: mo.Some("exact input"), Issues: nil,
	}, nil)
	service := New(active, navigator, gate)

	// Act through the client-facing facade.
	result, err := service.Navigate(t.Context(), testNavigationRequest())

	// Assert committed snapshots and exact next input are returned together.
	require.NoError(t, err)
	assert.Equal(t, tree, result.Tree)
	assert.Equal(t, branch, result.ActiveBranch)
	assert.Equal(t, mo.Some("exact input"), result.NextInput)
}

// testNavigationRequest creates the no-summary request used by session-control tests.
func testNavigationRequest() sessionnavigation.Request {
	return sessionnavigation.Request{
		TargetEntryID: "target", SummaryMode: sessionnavigation.SummaryModeNoSummary,
		CustomFocus: mo.None[string](),
	}
}

// TestResumeHoldsGateThroughActiveReplacement verifies resume releases the gate only after active replacement returns.
func TestResumeHoldsGateThroughActiveReplacement(t *testing.T) {
	t.Parallel()

	// Arrange a release observer and an active-session replacement that checks gate ownership.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	navigator := NewMockNavigator(controller)
	gate := NewMockOperationGate(controller)
	released := false
	gate.EXPECT().TryAcquire().Return(func() { released = true }, true)
	service := New(active, navigator, gate)
	active.EXPECT().ResumeActive(gomock.Any(), session.ID("stored")).DoAndReturn(
		func(_ any, _ session.ID) (session.Replacement, error) {
			require.False(t, released)
			return session.Replacement{Info: session.Info{
				ID: "stored", Name: mo.None[string](), WorkingDirectory: "",
				StoragePath: mo.None[string](), CreatedAt: time.Time{}, UpdatedAt: time.Time{},
			}, Entries: nil}, nil
		},
	)

	// Act by resuming the stored session through session control.
	replacement, err := service.Resume(t.Context(), "stored")

	// Assert replacement runs before release and the gate is released after the result returns.
	require.NoError(t, err)
	require.Equal(t, session.ID("stored"), replacement.Info.ID)
	require.True(t, released)
}
