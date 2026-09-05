//go:build !integration

package extensioncontext

import (
	"context"
	"sync"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestBindingsNeverReactivate verifies runtime replacement, same-ID resume, and A-to-B-to-A invalidation.
func TestBindingsNeverReactivate(t *testing.T) {
	t.Parallel()

	// Arrange: isolate runtime and session identity behind generated state-owner mocks.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	session := NewMockSessionState(controller)
	instance := "runtime-1"
	identity := SessionIdentity{ID: "A", WorkingDirectory: "/project", Incarnation: 1}
	runtime.EXPECT().
		ContextRuntime("extension").
		DoAndReturn(func(string) (string, bool) { return instance, true }).
		AnyTimes()
	session.EXPECT().ContextSession().DoAndReturn(func() SessionIdentity { return identity }).AnyTimes()
	service := New(runtime, session)

	// Act: issue and reuse a binding before each permanent replacement.
	first, err := service.IssueContext("extension")
	require.NoError(t, err)
	repeated, err := service.IssueContext("extension")
	require.NoError(t, err)
	require.Equal(t, first, repeated)
	old := extension.ContextRef{ID: first.ID, RuntimeInstanceID: first.RuntimeInstanceID, SessionID: first.SessionID}
	require.NoError(t, service.ValidateContext("extension", instance, old))
	for _, replacement := range []SessionIdentity{
		{ID: "A", WorkingDirectory: "/project", Incarnation: 2},
		{ID: "B", WorkingDirectory: "/project", Incarnation: 3},
		{ID: "A", WorkingDirectory: "/project", Incarnation: 4},
	} {
		identity = replacement
		current, issueErr := service.IssueContext("extension")
		require.NoError(t, issueErr)
		assert.NotEqual(t, first.ID, current.ID)
		assertStaleContext(t, service.ValidateContext("extension", instance, old))
		currentRef := extension.ContextRef{ID: current.ID, RuntimeInstanceID: instance, SessionID: current.SessionID}
		require.NoError(t, service.ValidateContext("extension", instance, currentRef))
	}
	instance = "runtime-2"
	current, err := service.IssueContext("extension")
	require.NoError(t, err)

	// Assert: public identity is exact and neither session nor runtime replacement restores an old binding.
	assert.Equal(t, "extension", current.ExtensionID)
	assert.Equal(t, instance, current.RuntimeInstanceID)
	assert.Equal(t, "A", current.SessionID)
	assert.Equal(t, "/project", current.WorkingDirectory)
	assert.NotEmpty(t, current.ID)
	assertStaleContext(t, service.ValidateContext("extension", "runtime-1", old))
	mismatched := extension.ContextRef{ID: current.ID, RuntimeInstanceID: instance, SessionID: "B"}
	assertStaleContext(t, service.ValidateContext("extension", instance, mismatched))
}

// TestCataloguesRevalidateBlockedReads verifies stale completion and ordered provider-neutral projection.
func TestCataloguesRevalidateBlockedReads(t *testing.T) {
	t.Parallel()
	for _, replaced := range []string{"session", "runtime"} {
		t.Run(replaced, func(t *testing.T) {
			t.Parallel()

			// Arrange: block catalog access while the active-session incarnation changes.
			controller := gomock.NewController(t)
			runtime := NewMockRuntimeState(controller)
			session := NewMockSessionState(controller)
			catalog := NewMockCatalog(controller)
			var mutex sync.Mutex
			identity := SessionIdentity{ID: "A", WorkingDirectory: "/project", Incarnation: 1}
			instance := "runtime"
			runtime.EXPECT().ContextRuntime("extension").DoAndReturn(func(string) (string, bool) {
				mutex.Lock()
				defer mutex.Unlock()
				return instance, true
			}).AnyTimes()
			session.EXPECT().ContextSession().DoAndReturn(func() SessionIdentity {
				mutex.Lock()
				defer mutex.Unlock()
				return identity
			}).AnyTimes()
			service := New(runtime, session)
			service.BindCatalog(catalog)
			binding, err := service.IssueContext("extension")
			require.NoError(t, err)
			reference := extension.ContextRef{ID: binding.ID, RuntimeInstanceID: "runtime", SessionID: "A"}
			entered := make(chan struct{})
			release := make(chan struct{})
			catalog.EXPECT().Models().DoAndReturn(func() []model.Descriptor {
				close(entered)
				<-release
				return nil
			})
			catalog.EXPECT().ActiveSelection().Return(model.Selection{})
			result := make(chan error, 1)

			// Act: replace the session after admission but before the catalog read finishes.
			go func() {
				_, readErr := service.ReadModels(t.Context(), "extension", "runtime", reference)
				result <- readErr
			}()
			<-entered
			mutex.Lock()
			if replaced == "session" {
				identity.Incarnation++
			} else {
				instance = "replacement-runtime"
			}
			mutex.Unlock()
			close(release)

			// Assert: the old result cannot complete successfully after replacement.
			assertStaleContext(t, <-result)
			fresh, err := service.IssueContext("extension")
			require.NoError(t, err)
			freshRef := extension.ContextRef{ID: fresh.ID, RuntimeInstanceID: fresh.RuntimeInstanceID, SessionID: "A"}
			catalog.EXPECT().Models().Return([]model.Descriptor{
				catalogueDescriptor("provider-a", "second"),
				catalogueDescriptor("provider-a", "first"),
				catalogueDescriptor("provider-b", "other"),
			})
			providers, err := service.ReadProviders(t.Context(), "extension", fresh.RuntimeInstanceID, freshRef)
			require.NoError(t, err)
			assert.Equal(t, []extensioncontroller.Provider{
				{ID: "provider-a", ModelIDs: []model.ID{"second", "first"}},
				{ID: "provider-b", ModelIDs: []model.ID{"other"}},
			}, providers)
			canceled, cancel := context.WithCancel(t.Context())
			cancel()
			_, err = service.ReadProviders(canceled, "extension", fresh.RuntimeInstanceID, freshRef)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

// TestFreshContextOwnersNeverReuseBindingIDs verifies recreated Host ownership cannot reactivate an encoded preceding
// reference.
func TestFreshContextOwnersNeverReuseBindingIDs(t *testing.T) {
	t.Parallel()

	// Arrange: supply the same durable identity to two independently constructed context owners.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	session := NewMockSessionState(controller)
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).AnyTimes()
	session.EXPECT().
		ContextSession().
		Return(SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}).
		AnyTimes()

	// Act: issue the first binding from each fresh owner.
	first, err := New(runtime, session).IssueContext("extension")
	require.NoError(t, err)
	second, err := New(runtime, session).IssueContext("extension")
	require.NoError(t, err)

	// Assert: binding identity is not a counter that restarts with Host ownership.
	assert.NotEqual(t, first.ID, second.ID)
}

// catalogueDescriptor supplies a complete provider-neutral test descriptor.
func catalogueDescriptor(provider model.ProviderID, id model.ID) model.Descriptor {
	return model.Descriptor{
		Provider:      provider,
		Model:         id,
		Input:         []model.InputModality{model.InputModalityText},
		ContextWindow: 1000,
		MaxTokens:     100,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: false,
			Choices:   []model.ReasoningChoice{model.ReasoningChoiceOff},
			Default:   model.ReasoningChoiceOff,
		},
		ToolCapabilities: model.ToolCapabilities{},
		Pricing:          mo.None[model.Pricing](),
	}
}

// assertStaleContext checks the closed category and nonempty diagnostic cause.
func assertStaleContext(t *testing.T, err error) {
	t.Helper()
	var failure extensioncontroller.ContextFailure
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, "STALE_CONTEXT", failure.ContextCode())
	assert.NotEmpty(t, err.Error())
}
