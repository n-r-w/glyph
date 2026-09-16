//go:build !integration

package modelselection

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestExtensionSelectionProtectsOnlyCommitAndEnqueue verifies bound protection ends before delivery acknowledgement.
func TestExtensionSelectionProtectsOnlyCommitAndEnqueue(t *testing.T) {
	t.Parallel()

	// Arrange: validate credentials before protection, then require commit and enqueue while protected.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	protection := NewMockBindingProtection(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	binding := Binding{
		ExtensionID: "extension", RuntimeID: "runtime",
		Context: extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"},
	}
	protected := false
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	catalog.EXPECT().
		ValidateSelection(gomock.Any(), target).
		DoAndReturn(func(_ context.Context, _ model.Selection) error {
			assert.False(t, protected)
			return nil
		})
	protection.EXPECT().ProtectSelectionCommit(gomock.Any(), binding, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ Binding, commit func() error) error {
			protected = true
			defer func() { protected = false }()
			return commit()
		},
	)
	catalog.EXPECT().CommitSelection(gomock.Any(), target).DoAndReturn(
		func(_ context.Context, _ model.Selection) (model.Selection, model.Selection, error) {
			assert.True(t, protected)
			return model.Selection{}, target, nil
		},
	)
	deliveryErr := errors.New("selection event writer failed")
	publisher.EXPECT().PublishSelection(target).DoAndReturn(
		func(model.Selection) (func(context.Context) error, error) {
			assert.True(t, protected)
			return func(context.Context) error {
				assert.False(t, protected)
				return deliveryErr
			}, nil
		},
	)
	service := New(catalog, publisher)
	service.BindProtection(protection)

	// Act: prepare and execute one bound extension model selection.
	prepared, err := service.PrepareExtensionSelection(extensioncontroller.SelectionCommand{
		Kind:        extensioncontroller.SelectionCommandModel,
		ExtensionID: binding.ExtensionID, RuntimeID: binding.RuntimeID, Context: binding.Context,
		Provider: target.Provider, Model: target.Model, ReasoningChoice: "",
	})
	require.NoError(t, err)
	result := prepared.Run(t.Context())
	prepared.Release()

	// Assert: committed state remains authoritative and delivery failure is an ordered post-commit issue.
	assert.True(t, result.Committed)
	assert.Equal(t, target, result.Selection)
	require.Len(t, result.Issues, 1)
	assert.Equal(t, deliveryFailedIssueCode, result.Issues[0].Code)
	assert.ErrorIs(t, result.Source, deliveryErr)
}

// TestExtensionSelectionPreservesProtectionCancellation verifies guard cancellation remains a pure pre-commit cause.
func TestExtensionSelectionPreservesProtectionCancellation(t *testing.T) {
	t.Parallel()

	// Arrange: complete handler and credential work, then cancel during final binding protection.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	protection := NewMockBindingProtection(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	binding := Binding{
		ExtensionID: "extension", RuntimeID: "runtime",
		Context: extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"},
	}
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	catalog.EXPECT().ValidateSelection(gomock.Any(), target).Return(nil)
	protection.EXPECT().ProtectSelectionCommit(gomock.Any(), binding, gomock.Any()).Return(context.Canceled)
	service := New(catalog, publisher)
	service.BindProtection(protection)

	// Act: run the accepted operation through canceled final protection.
	prepared, err := service.PrepareExtensionSelection(extensioncontroller.SelectionCommand{
		Kind:        extensioncontroller.SelectionCommandModel,
		ExtensionID: binding.ExtensionID, RuntimeID: binding.RuntimeID, Context: binding.Context,
		Provider: target.Provider, Model: target.Model, ReasoningChoice: "",
	})
	require.NoError(t, err)
	result := prepared.Run(t.Context())
	prepared.Release()

	// Assert: cancellation remains in the source and no commit or event occurs.
	assert.False(t, result.Committed)
	assert.ErrorIs(t, result.Source, context.Canceled)
}

// TestExtensionSelectionRejectsStaleBindingBeforeCommit verifies stale protection cannot mutate or publish selection.
func TestExtensionSelectionRejectsStaleBindingBeforeCommit(t *testing.T) {
	t.Parallel()

	// Arrange: complete handler and credential work, then reject the final binding protection.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	protection := NewMockBindingProtection(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	binding := Binding{
		ExtensionID: "extension", RuntimeID: "runtime",
		Context: extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"},
	}
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	catalog.EXPECT().ValidateSelection(gomock.Any(), target).Return(nil)
	staleErr := staleBindingTestError{cause: errors.New("session incarnation changed")}
	protection.EXPECT().ProtectSelectionCommit(gomock.Any(), binding, gomock.Any()).Return(staleErr)
	service := New(catalog, publisher)
	service.BindProtection(protection)

	// Act: run the accepted operation after its binding becomes stale.
	prepared, err := service.PrepareExtensionSelection(extensioncontroller.SelectionCommand{
		Kind:        extensioncontroller.SelectionCommandModel,
		ExtensionID: binding.ExtensionID, RuntimeID: binding.RuntimeID, Context: binding.Context,
		Provider: target.Provider, Model: target.Model, ReasoningChoice: "",
	})
	require.NoError(t, err)
	result := prepared.Run(t.Context())
	prepared.Release()

	// Assert: stale context is terminal, complete, and no commit or event call is admitted.
	assert.False(t, result.Committed)
	var failure extensioncontroller.SelectionFailure
	require.ErrorAs(t, result.Source, &failure)
	assert.Equal(t, ErrorCodeStaleContext, failure.ModelSelectionCode())
	assert.ErrorIs(t, result.Source, staleErr)
	assert.Contains(t, result.Source.Error(), "session incarnation changed")
}

// staleBindingTestError exposes the context category used by the binding owner.
type staleBindingTestError struct {
	// cause contains complete stale-binding detail.
	cause error
}

// Error returns complete stale-binding text.
func (e staleBindingTestError) Error() string { return e.cause.Error() }

// Unwrap exposes the original stale-binding cause.
func (e staleBindingTestError) Unwrap() error { return e.cause }

// ContextCode returns the stable stale-binding category.
func (e staleBindingTestError) ContextCode() string { return "STALE_CONTEXT" }
