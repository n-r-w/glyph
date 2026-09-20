//go:build !integration

package extensioncontext

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
)

// TestProtectSelectionCommitCoordinatesIssuedSessionAndRuntime verifies exact binding projection and guard delegation.
func TestProtectSelectionCommitCoordinatesIssuedSessionAndRuntime(t *testing.T) {
	t.Parallel()

	// Arrange: issue one context and require the session owner to acquire the supplied runtime guard.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 7}
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).Times(2)
	sessions.EXPECT().ContextSession().Return(identity).Times(2)
	service := New(runtime, sessions)
	issued, err := service.IssueContext("extension")
	require.NoError(t, err)
	runtimeProtected := false
	released := false
	runtime.EXPECT().BeginContextCommit("extension", "runtime").DoAndReturn(func(string, string) (func(), error) {
		runtimeProtected = true
		return func() {
			runtimeProtected = false
			released = true
		}, nil
	})
	sessions.EXPECT().ProtectContextCommit(gomock.Any(), identity, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ session.Identity, guard ContextCommitGuard, commit func() error) error {
			release, guardErr := guard()
			if guardErr != nil {
				return guardErr
			}
			defer release()
			return commit()
		},
	)
	commitCalled := false
	binding := modelselection.Binding{
		ExtensionID: "extension", RuntimeID: "runtime",
		Context: extensiondomain.ContextRef{
			ID: issued.ID, RuntimeInstanceID: issued.RuntimeInstanceID, SessionID: issued.SessionID,
		},
	}

	// Act: protect one selection commit through the issued context.
	err = service.ProtectSelectionCommit(t.Context(), binding, func() error {
		assert.True(t, runtimeProtected)
		commitCalled = true
		return nil
	})

	// Assert: the issued incarnation was used and runtime protection was released after commit.
	require.NoError(t, err)
	assert.True(t, commitCalled)
	assert.True(t, released)
	assert.False(t, runtimeProtected)
}

// TestProtectSelectionCommitRejectsSupersededContext verifies final validation rejects an old context before session commit.
func TestProtectSelectionCommitRejectsSupersededContext(t *testing.T) {
	t.Parallel()

	// Arrange: issue A, replace its session incarnation, then issue a new context for the same durable session ID.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	oldIdentity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	newIdentity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 3}
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).Times(2)
	sessions.EXPECT().ContextSession().Return(oldIdentity)
	sessions.EXPECT().ContextSession().Return(newIdentity)
	service := New(runtime, sessions)
	oldContext, err := service.IssueContext("extension")
	require.NoError(t, err)
	newContext, err := service.IssueContext("extension")
	require.NoError(t, err)
	require.NotEqual(t, oldContext.ID, newContext.ID)
	oldBinding := modelselection.Binding{
		ExtensionID: "extension", RuntimeID: "runtime",
		Context: extensiondomain.ContextRef{
			ID: oldContext.ID, RuntimeInstanceID: oldContext.RuntimeInstanceID, SessionID: oldContext.SessionID,
		},
	}

	// Act: attempt final protection through the superseded context.
	err = service.ProtectSelectionCommit(t.Context(), oldBinding, func() error {
		t.Fatal("stale context must not commit")
		return nil
	})

	// Assert: the old context remains stale even though durable session and runtime IDs match again.
	require.Error(t, err)
	var failure modelselection.BindingFailure
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, staleContextCode, failure.ContextCode())
	assert.Contains(t, err.Error(), "reference does not match the context issued to this runtime")
}
