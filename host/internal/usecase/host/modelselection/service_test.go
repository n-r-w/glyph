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
	observer := NewMockObserver(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	catalog.EXPECT().ResolveReasoning(target.ReasoningChoice).Return(target, nil)
	catalog.EXPECT().ValidateSelection(gomock.Any(), target).Return(nil)
	catalog.EXPECT().CommitSelection(gomock.Any(), target).Return(target, target, nil)
	service := New(catalog, publisher)
	service.BindObserver(observer)
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

// TestServiceObservesChangedSelectionAfterDelivery verifies detached ordered post-commit observation.
func TestServiceObservesChangedSelectionAfterDelivery(t *testing.T) {
	t.Parallel()

	// Arrange a commit that changes reasoning and model, then fails and cancels during delivery acknowledgement.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	observer := NewMockObserver(controller)
	preceding := model.Selection{Provider: "old", Model: "old-model", ReasoningChoice: model.ReasoningChoiceLow}
	committed := model.Selection{Provider: "new", Model: "new-model", ReasoningChoice: model.ReasoningChoiceHigh}
	deliveryErr := errors.New("selection delivery failed after commit")
	reasoningErr := errors.New("reasoning observer failed")
	modelErr := errors.New("model observer failed")
	ctx, cancel := context.WithCancel(t.Context())
	order := make([]string, 0, 4)
	catalog.EXPECT().ResolveModel(committed.Provider, committed.Model).Return(committed, nil)
	catalog.EXPECT().ValidateSelection(gomock.Any(), committed).Return(nil)
	catalog.EXPECT().CommitSelection(gomock.Any(), committed).Return(preceding, committed, nil)
	publisher.EXPECT().
		PublishSelection(committed).
		DoAndReturn(func(model.Selection) (func(context.Context) error, error) {
			order = append(order, "published")
			return func(context.Context) error {
				order = append(order, "acknowledgement")
				cancel()
				return deliveryErr
			}, nil
		})
	change := SelectionChange{Preceding: preceding, Committed: committed}
	observer.EXPECT().ObserveSelection(gomock.Any(), ObservationKindReasoning, change).DoAndReturn(
		func(observerCtx context.Context, _ ObservationKind, _ SelectionChange) []Issue {
			assert.NoError(t, observerCtx.Err())
			order = append(order, "reasoning")
			return []Issue{
				{
					ExtensionID: "reasoning-extension",
					HandlerID:   "reasoning-observer",
					Code:        IssueCodeObserverError,
					Err:         reasoningErr,
				},
			}
		},
	)
	observer.EXPECT().ObserveSelection(gomock.Any(), ObservationKindModel, change).DoAndReturn(
		func(observerCtx context.Context, _ ObservationKind, _ SelectionChange) []Issue {
			assert.NoError(t, observerCtx.Err())
			order = append(order, "model")
			return []Issue{
				{
					ExtensionID: "model-extension",
					HandlerID:   "model-observer",
					Code:        IssueCodeObserverError,
					Err:         modelErr,
				},
			}
		},
	)
	service := New(catalog, publisher)
	service.BindObserver(observer)
	prepared, err := service.prepareModel(committed.Provider, committed.Model)
	require.NoError(t, err)
	defer prepared.Release()

	// Act after all pre-commit work succeeds.
	result := prepared.Run(ctx)

	// Assert client publication precedes reasoning and model observation, and all diagnostics remain ordered.
	assert.True(t, result.committed)
	assert.Equal(t, committed, result.selection)
	assert.Equal(t, []string{"published", "acknowledgement", "reasoning", "model"}, order)
	require.Len(t, result.issues, 2)
	assert.ErrorIs(t, result.issues[0].Err, reasoningErr)
	assert.ErrorIs(t, result.issues[1].Err, modelErr)
	assert.ErrorIs(t, result.deliveryErr, deliveryErr)
}

// TestServicePreservesCredentialCancellation verifies canceled credential I/O stays pure before commit.
func TestServicePreservesCredentialCancellation(t *testing.T) {
	t.Parallel()

	// Arrange: cancel the operation during final credential validation and retain the catalog wrapper.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	ctx, cancel := context.WithCancel(t.Context())
	credentialErr := credentialCancellationError{cause: context.Canceled}
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	catalog.EXPECT().ValidateSelection(gomock.Any(), target).DoAndReturn(
		func(context.Context, model.Selection) error {
			cancel()
			return credentialErr
		},
	)
	service := New(catalog, publisher)
	run, release, err := prepareModelForTest(service, target.Provider, target.Model)
	require.NoError(t, err)
	defer release()

	// Act: execute through canceled credential validation.
	_, committed, err := run(ctx)

	// Assert: no commit occurs and both cancellation wrappers remain pure cancellation causes.
	assert.False(t, committed)
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, err, credentialErr)
}

// credentialCancellationError preserves the typed catalog category around cancellation.
type credentialCancellationError struct {
	// cause is the credential I/O cancellation.
	cause error
}

// Error returns complete credential cancellation text.
func (e credentialCancellationError) Error() string { return e.cause.Error() }

// Unwrap exposes the credential I/O cancellation.
func (e credentialCancellationError) Unwrap() error { return e.cause }

// CatalogSelectionCode returns the credential failure category.
func (e credentialCancellationError) CatalogSelectionCode() string { return credentialUnavailableCode }

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
