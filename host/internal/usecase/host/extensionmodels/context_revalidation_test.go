//go:build !integration

package extensionmodels_test

import (
	"context"
	"sync"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	sessiondomain "github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionmodels"
)

// TestCataloguesRevalidateBlockedReads verifies session and runtime replacement reject stale catalog results.
func TestCataloguesRevalidateBlockedReads(t *testing.T) {
	t.Parallel()

	for _, replaced := range []string{"session", "runtime"} {
		t.Run(replaced, func(t *testing.T) {
			t.Parallel()

			// Arrange a blocked catalog read against a real context validator.
			controller := gomock.NewController(t)
			runtime := extensioncontext.NewMockRuntimeState(controller)
			session := extensioncontext.NewMockSessionState(controller)
			catalog := extensionmodels.NewMockCatalog(controller)
			requester := extensionmodels.NewMockModelRequester(controller)
			var mutex sync.Mutex
			identity := sessiondomain.Identity{ID: "A", WorkingDirectory: "/project", Incarnation: 1}
			instance := "runtime"
			runtime.EXPECT().ContextRuntime("extension").DoAndReturn(func(string) (string, bool) {
				mutex.Lock()
				defer mutex.Unlock()
				return instance, true
			}).AnyTimes()
			session.EXPECT().ContextSession().DoAndReturn(func() sessiondomain.Identity {
				mutex.Lock()
				defer mutex.Unlock()
				return identity
			}).AnyTimes()
			contexts := extensioncontext.New(runtime, session)
			service := extensionmodels.New(catalog, requester, contexts)
			binding, err := contexts.IssueContext("extension")
			require.NoError(t, err)
			reference := extensiondomain.ContextRef{ID: binding.ID, RuntimeInstanceID: "runtime", SessionID: "A"}
			entered := make(chan struct{})
			release := make(chan struct{})
			catalog.EXPECT().Models().DoAndReturn(func() []model.Descriptor {
				close(entered)
				<-release
				return nil
			})
			catalog.EXPECT().ActiveSelection().Return(model.Selection{})
			result := make(chan error, 1)

			// Act by replacing the session or runtime after admission but before completion.
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

			// Assert the stale result is rejected and a fresh provider read preserves order.
			assertStaleContext(t, <-result)
			fresh, err := contexts.IssueContext("extension")
			require.NoError(t, err)
			freshRef := extensiondomain.ContextRef{
				ID:                fresh.ID,
				RuntimeInstanceID: fresh.RuntimeInstanceID,
				SessionID:         "A",
			}
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

// catalogueDescriptor supplies one complete provider-neutral test descriptor.
func catalogueDescriptor(provider model.ProviderID, id model.ID) model.Descriptor {
	return model.Descriptor{
		Provider: provider, Model: id, Input: []model.InputModality{model.InputModalityText},
		ContextWindow: 1000, MaxTokens: 100,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: false,
			Choices:   []model.ReasoningChoice{model.ReasoningChoiceOff},
			Default:   model.ReasoningChoiceOff,
		},
		ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
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
