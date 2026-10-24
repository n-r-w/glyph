//go:build !integration

package extension

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestInvocationPreparationFailureReleasesAdmission checks source retention before once-only cleanup.
func TestInvocationPreparationFailureReleasesAdmission(t *testing.T) {
	t.Parallel()
	// Arrange a qualifying failure after runtime and invocation admission.
	mocks := gomock.NewController(t)
	contexts := NewMockContextOperations(mocks)
	runtime := NewMockRuntimeOperations(mocks)
	child := NewMockInvocationChild(mocks)
	selection := NewMockModelSelection(mocks)
	failure := NewMockNestedExtensionFailure(mocks)
	failure.EXPECT().Error().Return("independent preparation failure: original cause").AnyTimes()
	failure.EXPECT().IsExtensionFailure().Return(true)
	reference := extensiondomain.ContextRef{ID: "binding", RuntimeInstanceID: "runtime", SessionID: "session"}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
	released := 0
	runtime.EXPECT().BeginContextOperation(gomock.Any(), "extension", "runtime").Return(func() { released++ }, nil)
	runtime.EXPECT().BeginInvocationChild("extension", "runtime", "parent").Return(child, nil)
	selection.EXPECT().PrepareExtensionSelection(gomock.Any()).Return(nil, failure)
	var retained error
	child.EXPECT().Fail(gomock.Not(nil)).Do(func(cause error) {
		assert.Zero(t, released)
		retained = cause
	})
	child.EXPECT().Release().Do(func() { assert.Equal(t, 1, released) })
	service := New(nil, contexts, runtime, "extension", "runtime")
	service.BindSelection(selection)
	request := new(extensionpb.ExtensionRequest)
	request.SetParentInvocationId("parent")
	request.SetSelectModel(extensionpb.SelectModelRequest_builder{
		Context: extensionpb.ExtensionContextRef_builder{
			ContextId: new(
				reference.ID,
			),
			RuntimeInstanceId: new(reference.RuntimeInstanceID),
			SessionId:         new(reference.SessionID),
		}.Build(),
		ProviderId: new("provider"), ModelId: new("model"),
	}.Build())
	// Act while selection preparation rejects the accepted child.
	prepared, err := service.Prepare(t.Context(), "child", request)
	// Assert the complete source is retained before cleanup releases runtime ownership once.
	require.Nil(t, prepared)
	require.ErrorIs(t, err, failure)
	require.ErrorIs(t, retained, failure)
	require.Equal(t, 1, released)
}

// TestInvocationRunAndSkippedRelease checks failure classification and duplicate cleanup ownership.
func TestInvocationRunAndSkippedRelease(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"extension", "ordinary", "canceled", "skipped"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			// Arrange one accepted catalog action with generated runtime and child owners.
			mocks := gomock.NewController(t)
			contexts := NewMockContextOperations(mocks)
			models := NewMockModelOperations(mocks)
			runtime := NewMockRuntimeOperations(mocks)
			child := NewMockInvocationChild(mocks)
			reference := extensiondomain.ContextRef{ID: "binding", RuntimeInstanceID: "runtime", SessionID: "session"}
			contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
			released := 0
			runtime.EXPECT().
				BeginContextOperation(gomock.Any(), "extension", "runtime").
				Return(func() { released++ }, nil)
			runtime.EXPECT().BeginInvocationChild("extension", "runtime", "parent").Return(child, nil)
			var source error
			if mode == "extension" {
				failure := NewMockNestedExtensionFailure(mocks)
				failure.EXPECT().Error().Return("nested observer: complete cause").AnyTimes()
				failure.EXPECT().IsExtensionFailure().Return(true)
				source = failure
			} else if mode == "canceled" {
				source = context.Canceled
			} else {
				source = errors.New("ordinary catalog validation failure")
			}
			var retained []error
			if mode != "skipped" {
				child.EXPECT().Link(gomock.Any()).DoAndReturn(func(ctx context.Context) (context.Context, func()) {
					return ctx, func() {}
				})
				models.EXPECT().
					ReadModels(gomock.Any(), "extension", "runtime", reference).
					Return(ModelCatalog{}, source)
				child.EXPECT().Fail(gomock.Any()).Do(func(cause error) {
					assert.Zero(t, released)
					retained = append(retained, cause)
				})
			}
			child.EXPECT().Release().Do(func() { assert.Equal(t, 1, released) })
			service := New(models, contexts, runtime, "extension", "runtime")
			request := new(extensionpb.ExtensionRequest)
			request.SetParentInvocationId("parent")
			request.SetGetModels(extensionpb.GetModelsRequest_builder{Context: extensionpb.ExtensionContextRef_builder{
				ContextId: new(
					reference.ID,
				),
				RuntimeInstanceId: new(reference.RuntimeInstanceID),
				SessionId:         new(reference.SessionID),
			}.Build()}.Build())
			prepared, err := service.Prepare(t.Context(), "child", request)
			require.NoError(t, err)
			// Act by running or skipping the accepted action, then releasing it twice.
			if mode != "skipped" {
				_, err = prepared.Run(t.Context(), nil)
				require.ErrorIs(t, err, source)
			}
			prepared.Release()
			prepared.Release()
			// Assert qualifying sources alone reach the parent and runtime cleanup executes once.
			require.Equal(t, 1, released)
			if mode == "extension" {
				require.Len(t, retained, 1)
				require.ErrorIs(t, retained[0], source)
			} else if mode != "skipped" {
				require.Equal(t, []error{nil}, retained)
			}
		})
	}
}
