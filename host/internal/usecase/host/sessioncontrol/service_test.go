//go:build !integration

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

// TestNavigatePreservesEveryNavigatorResult verifies operation result and cause propagation.
func TestNavigatePreservesEveryNavigatorResult(t *testing.T) {
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

			// Arrange the active-session or navigation result.
			controller := gomock.NewController(t)
			navigator := NewMockNavigator(controller)

			active := NewMockActiveSessions(controller)
			publisher := testProgressPublisher(t)
			navigator.EXPECT().NavigateTree(gomock.Any(), testNavigationRequest(), gomock.Any()).DoAndReturn(
				func(
					_ context.Context,
					_ sessionnavigation.Request,
					_ func(sessionnavigation.Progress) error,
				) (sessionnavigation.Result, error) {
					return sessionnavigation.Result{
						Canceled: false, DestinationID: mo.Some("destination"), ActiveLeafID: mo.Some("destination"),
						CreatedSummary: mo.None[session.Entry](), NextInput: mo.None[string](), Issues: nil,
					}, test.navigationErr
				},
			)
			service := New(active, navigator)

			// Act by invoking the requested operation.

			_, err := service.Navigate(t.Context(), testNavigationRequest(), publisher)

			// Assert the operation preserves its result and cause.
			if test.navigationErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.navigationErr)
			}
		})
	}
}

// TestNavigateReturnsCommittedMetadata verifies operation result and cause propagation.
func TestNavigateReturnsCommittedMetadata(t *testing.T) {
	t.Parallel()

	// Arrange a successful navigation and committed active-session snapshots.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	navigator := NewMockNavigator(controller)
	publisher := testProgressPublisher(t)
	navigator.EXPECT().
		NavigateTree(gomock.Any(), testNavigationRequest(), gomock.Any()).
		Return(sessionnavigation.Result{
			Canceled: false, DestinationID: mo.Some("destination"), ActiveLeafID: mo.Some("leaf"),
			CreatedSummary: mo.None[session.Entry](), NextInput: mo.Some("exact input"), Issues: nil,
		}, nil)
	service := New(active, navigator)

	// Act by invoking the requested operation.

	result, err := service.Navigate(t.Context(), testNavigationRequest(), publisher)

	// Assert committed metadata and exact next input are returned together.
	require.NoError(t, err)
	assert.Equal(t, mo.Some("destination"), result.DestinationID)
	assert.Equal(t, mo.Some("leaf"), result.ActiveLeafID)
	assert.Equal(t, mo.Some("exact input"), result.NextInput)
}

// testProgressPublisher creates one accepting operation progress publisher.
func testProgressPublisher(t *testing.T) func(sessionnavigation.Progress) error {
	t.Helper()
	return func(sessionnavigation.Progress) error { return nil }
}

// testNavigationRequest creates the no-summary request used by session-control tests.
func testNavigationRequest() sessionnavigation.Request {
	return sessionnavigation.Request{
		TargetEntryID: "target", SummaryMode: sessionnavigation.SummaryModeNoSummary,
		CustomFocus: mo.None[string](),
	}
}

// Act by invoking the requested operation.
func TestResumeReturnsActiveReplacement(t *testing.T) {
	t.Parallel()

	// Arrange the active-session or navigation result.
	controller := gomock.NewController(t)
	active := NewMockActiveSessions(controller)
	navigator := NewMockNavigator(controller)

	service := New(active, navigator)
	active.EXPECT().ResumeActive(gomock.Any(), session.ID("stored")).DoAndReturn(
		func(_ any, _ session.ID) (session.Replacement, error) {
			return session.Replacement{Info: session.Info{
				ID: "stored", Name: mo.None[string](), WorkingDirectory: "",
				StoragePath: mo.None[string](), CreatedAt: time.Time{}, UpdatedAt: time.Time{},
			}, Entries: nil}, nil
		},
	)

	// Act by invoking the requested operation.

	replacement, err := service.Resume(t.Context(), "stored")

	// Assert the operation preserves its result and cause.
	require.NoError(t, err)
	require.Equal(t, session.ID("stored"), replacement.Info.ID)
}
