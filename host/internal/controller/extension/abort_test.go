//go:build !integration

package extension

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// abortRequest supplies one public request with no caller-controlled source identity.
func abortRequest() *extensionpb.ExtensionRequest {
	request := new(extensionpb.ExtensionRequest)
	request.SetAbortRun(extensionpb.AbortRunRequest_builder{Context: extensionpb.ExtensionContextRef_builder{
		ContextId: new("binding"), RuntimeInstanceId: new("runtime"), SessionId: new("session"),
	}.Build()}.Build())
	return request
}

// TestAbortAdmissionBindings checks stale binding rejection before cancellation and exact cleanup ownership.
func TestAbortAdmissionBindings(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"session", "runtime", "parent", "idle", "active", "replaced"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			// Arrange one bound controller with no operation gate and a complete failure at the selected boundary.
			mocks := gomock.NewController(t)
			contexts := NewMockContextOperations(mocks)
			runtime := NewMockRuntimeOperations(mocks)
			messages := NewMockMessageEnqueuer(mocks)
			service := New(nil, contexts, runtime, "bound-extension", "runtime")
			service.BindMessages(messages)
			cause := errors.New("stale binding original cause\ncomplete suffix")
			var sessionErr, runtimeErr, parentErr error
			if stage == "session" {
				sessionErr = cause
			}
			if stage == "runtime" {
				runtimeErr = cause
			}
			if stage == "parent" {
				parentErr = cause
			}
			contexts.EXPECT().ValidateContext("bound-extension", "runtime", extensiondomain.ContextRef{
				ID: "binding", RuntimeInstanceID: "runtime", SessionID: "session",
			}).Return(sessionErr)
			released := 0
			child := NewMockInvocationChild(mocks)
			if sessionErr == nil {
				runtime.EXPECT().BeginContextOperation(gomock.Any(), "bound-extension", "runtime").
					Return(func() { released++ }, runtimeErr)
			}
			if sessionErr == nil && runtimeErr == nil {
				if parentErr != nil {
					runtime.EXPECT().BeginInvocationChild("bound-extension", "runtime", "").Return(nil, parentErr)
				} else {
					runtime.EXPECT().BeginInvocationChild("bound-extension", "runtime", "").Return(child, nil)
					child.EXPECT().Release()
					child.EXPECT().Fail(gomock.Any())
				}
			}
			switch stage {
			case "idle":
				messages.EXPECT().CheckAbort().Return("", agent.ErrRunNotActive)
			case "active", "replaced":
				messages.EXPECT().CheckAbort().Return("captured-run", nil)
				child.EXPECT().Link(gomock.Any()).
					DoAndReturn(func(ctx context.Context) (context.Context, func()) { return ctx, func() {} })
				var abortErr error
				if stage == "replaced" {
					abortErr = agent.ErrRunNotActive
				}
				messages.EXPECT().Abort(gomock.Any(), "captured-run", "bound-extension").Return(abortErr)
			}
			// Act by admitting, then executing only an accepted public operation.
			prepared, err := service.Prepare(t.Context(), "abort", abortRequest())
			if err == nil {
				result, runErr := prepared.Run(t.Context(), nil)
				prepared.Release()
				err = runErr
				if stage == "active" {
					require.NotNil(t, result.GetAbortRun())
				}
			}
			// Assert stale or ended work fails explicitly without calling cancellation for another run.
			switch stage {
			case "active":
				require.NoError(t, err)
			case "idle", "replaced":
				require.ErrorIs(t, err, agent.ErrRunNotActive)
			default:
				require.ErrorIs(t, err, cause)
				require.ErrorContains(t, err, cause.Error())
			}
			if sessionErr == nil && runtimeErr == nil {
				require.Equal(t, 1, released)
			} else {
				require.Zero(t, released)
			}
		})
	}
}
