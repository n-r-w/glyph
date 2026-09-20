//go:build !integration

package sessions

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestProtectContextCommitAcquiresSessionBeforeRuntime verifies guard order, callback coverage, and release.
func TestProtectContextCommitAcquiresSessionBeforeRuntime(t *testing.T) {
	t.Parallel()

	// Arrange: install one active incarnation and observe lock and runtime protection during commit.
	service := New(nil, nil, nil, nil, "/project")
	expected := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	service.contextIdentity.Store(&expected)
	runtimeProtected := false
	guard := func() (func(), error) {
		acquired := service.mutex.TryLock()
		if acquired {
			service.mutex.Unlock()
		}
		assert.False(t, acquired, "session protection must be acquired before runtime protection")
		runtimeProtected = true
		return func() { runtimeProtected = false }, nil
	}
	committed := false

	// Act: protect one final callback.
	err := service.ProtectContextCommit(t.Context(), expected, guard, func() error {
		assert.True(t, runtimeProtected)
		acquired := service.mutex.TryLock()
		if acquired {
			service.mutex.Unlock()
		}
		assert.False(t, acquired, "session protection must cover commit callback")
		committed = true
		return nil
	})

	// Assert: callback completed and both protections were released before return.
	require.NoError(t, err)
	assert.True(t, committed)
	assert.False(t, runtimeProtected)
	require.True(t, service.mutex.TryLock())
	service.mutex.Unlock()
}

// TestProtectContextCommitRejectsReactivatedDurableID verifies an old incarnation stays stale after A-to-B-to-A.
func TestProtectContextCommitRejectsReactivatedDurableID(t *testing.T) {
	t.Parallel()

	// Arrange: retain old A identity while current identity is a later A incarnation.
	service := New(nil, nil, nil, nil, "/project")
	oldA := session.Identity{ID: "session-a", WorkingDirectory: "/project", Incarnation: 1}
	newA := session.Identity{ID: "session-a", WorkingDirectory: "/project", Incarnation: 3}
	service.contextIdentity.Store(&newA)
	guardCalled := false
	commitCalled := false

	// Act: try to protect the old A binding after A-to-B-to-A replacement.
	err := service.ProtectContextCommit(t.Context(), oldA, func() (func(), error) {
		guardCalled = true
		return func() {}, nil
	}, func() error {
		commitCalled = true
		return nil
	})

	// Assert: durable ID reuse does not authorize old context and neither later phase runs.
	require.Error(t, err)
	assert.ErrorIs(t, err, session.ErrUnavailable)
	assert.Contains(t, err.Error(), "incarnation was replaced")
	assert.False(t, guardCalled)
	assert.False(t, commitCalled)
}

// TestProtectContextCommitPreservesRuntimeGuardFailure verifies complete runtime staleness reaches the caller.
func TestProtectContextCommitPreservesRuntimeGuardFailure(t *testing.T) {
	t.Parallel()

	// Arrange: keep session current and fail runtime protection with a detailed cause.
	service := New(nil, nil, nil, nil, "/project")
	expected := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	service.contextIdentity.Store(&expected)
	runtimeErr := errors.New("runtime instance was replaced")

	// Act: acquire session protection and reject runtime protection.
	err := service.ProtectContextCommit(t.Context(), expected, func() (func(), error) {
		return nil, runtimeErr
	}, func() error {
		t.Fatal("commit must not run after runtime protection failure")
		return nil
	})

	// Assert: runtime failure is retained and session protection is released.
	require.ErrorIs(t, err, runtimeErr)
	assert.Contains(t, err.Error(), "runtime instance was replaced")
	require.True(t, service.mutex.TryLock())
	service.mutex.Unlock()
}
