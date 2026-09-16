//go:build !integration

package modelselection

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestServiceRejectsOverlappingSelection verifies that all callers share one admission reservation.
func TestServiceRejectsOverlappingSelection(t *testing.T) {
	t.Parallel()
	// Arrange one admitted model selection that retains its prepared cleanup.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	catalog.EXPECT().ResolveModel(model.ProviderID("provider"), model.ID("model")).Return(target, nil)
	service := New(catalog, publisher)
	_, release, err := prepareModelForTest(service, "provider", "model")
	require.NoError(t, err)
	defer release()

	// Act by preparing an overlapping reasoning selection.
	_, _, err = prepareReasoningForTest(service, model.ReasoningChoiceHigh)

	// Assert BUSY classification without consulting the catalogue.
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeBusy, selectionErr.Code)
}

// TestServicePublishesChangedSelectionBeforeCompletion verifies atomic commit and authoritative publication.
func TestServicePublishesChangedSelectionBeforeCompletion(t *testing.T) {
	t.Parallel()
	// Arrange a valid target and changed atomic commit.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	target := model.Selection{Provider: "next", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	preceding := model.Selection{Provider: "old", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	catalog.EXPECT().ValidateSelection(gomock.Any(), target).Return(nil)
	catalog.EXPECT().CommitSelection(gomock.Any(), target).Return(preceding, target, nil)
	publicationWaited := false
	publisher.EXPECT().PublishSelection(target).Return(func(context.Context) error {
		publicationWaited = true
		return nil
	}, nil)
	service := New(catalog, publisher)
	run, release, err := prepareModelForTest(service, target.Provider, target.Model)
	require.NoError(t, err)
	defer release()

	// Act by executing the admitted selection.
	selection, committed, err := run(t.Context())

	// Assert the committed full selection is returned after publication acknowledgement.
	require.NoError(t, err)
	assert.True(t, committed)
	assert.True(t, publicationWaited)
	assert.Equal(t, target, selection)
}

// TestServiceDoesNotPublishUnchangedSelection verifies no-op selection has no connection event.
func TestServiceDoesNotPublishUnchangedSelection(t *testing.T) {
	t.Parallel()
	// Arrange a commit whose preceding and committed selections match.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	catalog.EXPECT().ResolveReasoning(target.ReasoningChoice).Return(target, nil)
	catalog.EXPECT().ValidateSelection(gomock.Any(), target).Return(nil)
	catalog.EXPECT().CommitSelection(gomock.Any(), target).Return(target, target, nil)
	service := New(catalog, publisher)
	run, release, err := prepareReasoningForTest(service, target.ReasoningChoice)
	require.NoError(t, err)
	defer release()

	// Act by executing the no-op selection.
	selection, committed, err := run(t.Context())

	// Assert success without publisher interaction.
	require.NoError(t, err)
	assert.True(t, committed)
	assert.Equal(t, target, selection)
}

// TestServiceHonorsCancellationBeforeFinalValidation verifies canceled work never reaches commit.
func TestServiceHonorsCancellationBeforeFinalValidation(t *testing.T) {
	t.Parallel()
	// Arrange one admitted request whose execution context is already canceled.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	service := New(catalog, publisher)
	run, release, err := prepareModelForTest(service, target.Provider, target.Model)
	require.NoError(t, err)
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act by executing after cancellation.
	_, committed, err := run(ctx)

	// Assert cancellation is returned directly without validation or commit.
	assert.False(t, committed)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestServiceRetainsCommittedSelectionWhenPublicationFails verifies post-commit diagnostics do not imply rollback.
func TestServiceRetainsCommittedSelectionWhenPublicationFails(t *testing.T) {
	t.Parallel()
	// Arrange a changed commit followed by a publication acknowledgement failure.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	target := model.Selection{Provider: "provider", Model: "next", ReasoningChoice: model.ReasoningChoiceHigh}
	preceding := model.Selection{Provider: "provider", Model: "old", ReasoningChoice: model.ReasoningChoiceLow}
	deliveryErr := errors.New("selection delivery failed")
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	catalog.EXPECT().ValidateSelection(gomock.Any(), target).Return(nil)
	catalog.EXPECT().CommitSelection(gomock.Any(), target).Return(preceding, target, nil)
	publisher.EXPECT().PublishSelection(target).Return(func(context.Context) error { return deliveryErr }, nil)
	service := New(catalog, publisher)
	run, release, err := prepareModelForTest(service, target.Provider, target.Model)
	require.NoError(t, err)
	defer release()

	// Act by executing the committed selection.
	selection, committed, err := run(t.Context())

	// Assert committed state and complete delivery diagnostics are both retained.
	assert.Equal(t, target, selection)
	assert.True(t, committed)
	assert.ErrorIs(t, err, deliveryErr)
}

// prepareModelForTest adapts the private prepared result for focused policy assertions.
func prepareModelForTest(
	service *Service,
	provider model.ProviderID,
	modelID model.ID,
) (func(context.Context) (model.Selection, bool, error), func(), error) {
	prepared, err := service.prepareModel(provider, modelID)
	if err != nil {
		return nil, nil, err
	}
	return func(ctx context.Context) (model.Selection, bool, error) {
		result := prepared.Run(ctx)
		return result.selection, result.committed, result.source()
	}, prepared.Release, nil
}

// prepareReasoningForTest adapts the private prepared result for focused policy assertions.
func prepareReasoningForTest(
	service *Service,
	choice model.ReasoningChoice,
) (func(context.Context) (model.Selection, bool, error), func(), error) {
	prepared, err := service.prepareReasoning(choice)
	if err != nil {
		return nil, nil, err
	}
	return func(ctx context.Context) (model.Selection, bool, error) {
		result := prepared.Run(ctx)
		return result.selection, result.committed, result.source()
	}, prepared.Release, nil
}
