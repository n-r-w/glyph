//go:build !integration

package modelselection

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestServiceComposesSelectionHandlersInRegistrationOrder verifies immutable original and successive current targets.
func TestServiceComposesSelectionHandlersInRegistrationOrder(t *testing.T) {
	t.Parallel()
	// Arrange two available handlers where the first replaces and the second preserves.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	delivery := NewMockIssueDelivery(controller)
	original := model.Selection{Provider: "source", Model: "one", ReasoningChoice: model.ReasoningChoiceLow}
	replacement := model.Selection{Provider: "target", Model: "two", ReasoningChoice: model.ReasoningChoiceHigh}
	bindingA := extension.Context{
		ID:                "a",
		ExtensionID:       "a",
		RuntimeInstanceID: "ra",
		SessionID:         "s",
		WorkingDirectory:  "/tmp",
	}
	bindingB := extension.Context{
		ID:                "b",
		ExtensionID:       "b",
		RuntimeInstanceID: "rb",
		SessionID:         "s",
		WorkingDirectory:  "/tmp",
	}
	catalog.EXPECT().ResolveModel(original.Provider, original.Model).Return(original, nil)
	runtime.EXPECT().HandlerRuntimeAvailable("a").Return(true)
	runtime.EXPECT().HandlerRuntimeAvailable("b").Return(true)
	contexts.EXPECT().IssueContext("a").Return(bindingA, nil)
	runtime.EXPECT().HandleSelection(gomock.Any(), "a", "replace", HandlerInvocation{
		Context: bindingA, Kind: HandlerKindModel, Original: original, Current: original,
	}).Return(HandlerAction{Kind: HandlerActionReplace, Replacement: replacement, Rejection: ""}, false, nil)
	contexts.EXPECT().IssueContext("b").Return(bindingB, nil)
	runtime.EXPECT().HandleSelection(gomock.Any(), "b", "preserve", HandlerInvocation{
		Context: bindingB, Kind: HandlerKindModel, Original: original, Current: replacement,
	}).Return(HandlerAction{Kind: HandlerActionPreserve, Replacement: model.Selection{}, Rejection: ""}, false, nil)
	catalog.EXPECT().ValidateSelection(gomock.Any(), replacement).Return(nil)
	catalog.EXPECT().CommitSelection(gomock.Any(), replacement).Return(original, replacement, nil)
	publisher.EXPECT().PublishSelection(replacement).Return(func(context.Context) error { return nil }, nil)
	service := New(catalog, publisher)
	service.BindHandlers(runtime, contexts, delivery)
	service.CommitSelectionHandlers([]startup.AcceptedRegistration{
		{
			ID:       "a",
			Path:     "/a",
			Tools:    nil,
			Handlers: []startup.AcceptedHandler{{ID: "replace", Kind: startup.RawHandlerKindModelSelection}},
		},
		{
			ID:       "b",
			Path:     "/b",
			Tools:    nil,
			Handlers: []startup.AcceptedHandler{{ID: "preserve", Kind: startup.RawHandlerKindModelSelection}},
		},
	})
	run, release, err := prepareModelForTest(service, original.Provider, original.Model)
	require.NoError(t, err)
	defer release()

	// Act by executing the admitted request.
	selection, committed, err := run(t.Context())

	// Assert only the composed final target is validated and committed.
	require.NoError(t, err)
	assert.True(t, committed)
	assert.Equal(t, replacement, selection)
}

// TestServiceContinuesAfterOrdinaryAndInvalidHandlerResults verifies nonfatal issue delivery and state preservation.
func TestServiceContinuesAfterOrdinaryAndInvalidHandlerResults(t *testing.T) {
	t.Parallel()
	// Arrange two faulty handlers followed by one valid replacement.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	delivery := NewMockIssueDelivery(controller)
	original := model.Selection{Provider: "source", Model: "one", ReasoningChoice: model.ReasoningChoiceLow}
	final := model.Selection{Provider: "target", Model: "two", ReasoningChoice: model.ReasoningChoiceHigh}
	binding := extension.Context{
		ID:                "ctx",
		ExtensionID:       "ext",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/tmp",
	}
	handlerErr := errors.New("ordinary handler failed")
	catalog.EXPECT().ResolveReasoning(original.ReasoningChoice).Return(original, nil)
	runtime.EXPECT().HandlerRuntimeAvailable("ext").Return(true)
	gomock.InOrder(
		contexts.EXPECT().IssueContext("ext").Return(binding, nil),
		runtime.EXPECT().
			HandleSelection(gomock.Any(), "ext", "ordinary", gomock.Any()).
			Return(HandlerAction{}, false, handlerErr),
		delivery.EXPECT().
			DeliverSelectionIssue(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, issue Issue) error {
				assert.Equal(t, IssueCodeHandlerError, issue.Code)
				assert.ErrorIs(t, issue.Err, handlerErr)
				return nil
			}),
		contexts.EXPECT().IssueContext("ext").Return(binding, nil),
		runtime.EXPECT().HandleSelection(gomock.Any(), "ext", "invalid", gomock.Any()).Return(
			HandlerAction{Kind: HandlerActionReplace, Replacement: model.Selection{}, Rejection: ""}, false, nil,
		),
		delivery.EXPECT().
			DeliverSelectionIssue(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, issue Issue) error {
				assert.Equal(t, IssueCodeInvalidHandlerAction, issue.Code)
				return nil
			}),
		contexts.EXPECT().IssueContext("ext").Return(binding, nil),
		runtime.EXPECT().HandleSelection(gomock.Any(), "ext", "valid", gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ string, invocation HandlerInvocation) (HandlerAction, bool, error) {
				assert.Equal(t, original, invocation.Current)
				return HandlerAction{Kind: HandlerActionReplace, Replacement: final, Rejection: ""}, false, nil
			},
		),
	)
	catalog.EXPECT().ValidateSelection(gomock.Any(), final).Return(nil)
	catalog.EXPECT().CommitSelection(gomock.Any(), final).Return(original, final, nil)
	publisher.EXPECT().PublishSelection(final).Return(func(context.Context) error { return nil }, nil)
	service := New(catalog, publisher)
	service.BindHandlers(runtime, contexts, delivery)
	service.CommitSelectionHandlers([]startup.AcceptedRegistration{{
		ID: "ext", Path: "/ext", Tools: nil,
		Handlers: []startup.AcceptedHandler{
			{ID: "ordinary", Kind: startup.RawHandlerKindReasoningSelection},
			{ID: "invalid", Kind: startup.RawHandlerKindReasoningSelection},
			{ID: "valid", Kind: startup.RawHandlerKindReasoningSelection},
		},
	}})
	prepared, err := service.prepareReasoning(original.ReasoningChoice)
	require.NoError(t, err)
	defer prepared.Release()

	// Act by executing the full handler chain.
	result := prepared.Run(t.Context())

	// Assert commit succeeds and both ordered issues remain explicit and ordered.
	assert.Equal(t, final, result.selection)
	assert.True(t, result.committed)
	require.Len(t, result.issues, 2)
	assert.Equal(t, "ordinary", result.issues[0].HandlerID)
	assert.Equal(t, IssueCodeHandlerError, result.issues[0].Code)
	assert.ErrorIs(t, result.issues[0].Err, handlerErr)
	assert.Equal(t, "invalid", result.issues[1].HandlerID)
	assert.Equal(t, IssueCodeInvalidHandlerAction, result.issues[1].Code)
	assert.ErrorIs(t, result.source(), handlerErr)
}

// TestServiceRejectsExplicitHandlerDecisionBeforeCommit verifies rejection text and category are retained.
func TestServiceRejectsExplicitHandlerDecisionBeforeCommit(t *testing.T) {
	t.Parallel()
	// Arrange one available handler that explicitly rejects the current target.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	delivery := NewMockIssueDelivery(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	binding := extension.Context{
		ID:                "ctx",
		ExtensionID:       "ext",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/tmp",
	}
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	runtime.EXPECT().HandlerRuntimeAvailable("ext").Return(true)
	contexts.EXPECT().IssueContext("ext").Return(binding, nil)
	runtime.EXPECT().HandleSelection(gomock.Any(), "ext", "reject", gomock.Any()).Return(
		HandlerAction{
			Kind:        HandlerActionReject,
			Replacement: model.Selection{},
			Rejection:   "policy denied target",
		}, false, nil,
	)
	service := New(catalog, publisher)
	service.BindHandlers(runtime, contexts, delivery)
	service.CommitSelectionHandlers(
		[]startup.AcceptedRegistration{
			{
				ID:       "ext",
				Path:     "/ext",
				Tools:    nil,
				Handlers: []startup.AcceptedHandler{{ID: "reject", Kind: startup.RawHandlerKindModelSelection}},
			},
		},
	)
	run, release, err := prepareModelForTest(service, target.Provider, target.Model)
	require.NoError(t, err)
	defer release()

	// Act by executing the admitted request.
	_, committed, err := run(t.Context())

	// Assert rejection stops before final validation or commit with complete text.
	assert.False(t, committed)
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeExtensionRejected, selectionErr.Code)
	assert.ErrorContains(t, err, "policy denied target")
}

// TestServiceFailsInternalWhenHandlerContextCannotBeIssued verifies Host invocation setup is not an ordinary handler error.
func TestServiceFailsInternalWhenHandlerContextCannotBeIssued(t *testing.T) {
	t.Parallel()
	// Arrange one selected available runtime whose invocation context cannot be issued.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	delivery := NewMockIssueDelivery(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	contextErr := errors.New("active session binding failed")
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	runtime.EXPECT().HandlerRuntimeAvailable("ext").Return(true).Times(2)
	contexts.EXPECT().IssueContext("ext").Return(extension.Context{}, contextErr)
	service := New(catalog, publisher)
	service.BindHandlers(runtime, contexts, delivery)
	service.CommitSelectionHandlers(
		[]startup.AcceptedRegistration{
			{
				ID:       "ext",
				Path:     "/ext",
				Tools:    nil,
				Handlers: []startup.AcceptedHandler{{ID: "handler", Kind: startup.RawHandlerKindModelSelection}},
			},
		},
	)
	run, release, err := prepareModelForTest(service, target.Provider, target.Model)
	require.NoError(t, err)
	defer release()

	// Act by executing the admitted request.
	_, committed, err := run(t.Context())

	// Assert Host setup failure stops before commit and preserves its complete cause.
	assert.False(t, committed)
	assert.ErrorIs(t, err, contextErr)
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeInternal, selectionErr.Code)
}

// TestServiceFailsWhenSelectedRuntimeBecomesUnavailable verifies selected runtime loss prevents commit.
func TestServiceFailsWhenSelectedRuntimeBecomesUnavailable(t *testing.T) {
	t.Parallel()
	// Arrange one handler that is available at chain selection and unavailable during invocation.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	delivery := NewMockIssueDelivery(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	binding := extension.Context{
		ID:                "ctx",
		ExtensionID:       "ext",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/tmp",
	}
	unavailableErr := errors.New("runtime exited during handler")
	catalog.EXPECT().ResolveReasoning(target.ReasoningChoice).Return(target, nil)
	runtime.EXPECT().HandlerRuntimeAvailable("ext").Return(true)
	contexts.EXPECT().IssueContext("ext").Return(binding, nil)
	runtime.EXPECT().
		HandleSelection(gomock.Any(), "ext", "handler", gomock.Any()).
		Return(HandlerAction{}, true, unavailableErr)
	service := New(catalog, publisher)
	service.BindHandlers(runtime, contexts, delivery)
	service.CommitSelectionHandlers(
		[]startup.AcceptedRegistration{
			{
				ID:       "ext",
				Path:     "/ext",
				Tools:    nil,
				Handlers: []startup.AcceptedHandler{{ID: "handler", Kind: startup.RawHandlerKindReasoningSelection}},
			},
		},
	)
	run, release, err := prepareReasoningForTest(service, target.ReasoningChoice)
	require.NoError(t, err)
	defer release()

	// Act by executing the admitted request.
	_, committed, err := run(t.Context())

	// Assert runtime loss is terminal before validation and preserves its complete cause.
	assert.False(t, committed)
	assert.ErrorIs(t, err, unavailableErr)
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeExtensionUnavailable, selectionErr.Code)
}

// TestServiceClassifiesRuntimeLossOverConcurrentCancellation verifies runtime loss keeps its closed category.
func TestServiceClassifiesRuntimeLossOverConcurrentCancellation(t *testing.T) {
	t.Parallel()
	// Arrange one selected handler whose runtime fails while the caller is canceled.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	delivery := NewMockIssueDelivery(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	binding := extension.Context{
		ID:                "ctx",
		ExtensionID:       "ext",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/tmp",
	}
	runtimeErr := errors.New("runtime transport closed")
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	runtime.EXPECT().HandlerRuntimeAvailable("ext").Return(true)
	contexts.EXPECT().IssueContext("ext").Return(binding, nil)
	ctx, cancel := context.WithCancel(t.Context())
	runtime.EXPECT().HandleSelection(gomock.Any(), "ext", "handler", gomock.Any()).DoAndReturn(
		func(context.Context, string, string, HandlerInvocation) (HandlerAction, bool, error) {
			cancel()
			return HandlerAction{}, true, runtimeErr
		},
	)
	service := New(catalog, publisher)
	service.BindHandlers(runtime, contexts, delivery)
	service.CommitSelectionHandlers(
		[]startup.AcceptedRegistration{
			{
				ID:       "ext",
				Path:     "/ext",
				Tools:    nil,
				Handlers: []startup.AcceptedHandler{{ID: "handler", Kind: startup.RawHandlerKindModelSelection}},
			},
		},
	)
	run, release, err := prepareModelForTest(service, target.Provider, target.Model)
	require.NoError(t, err)
	defer release()

	// Act while runtime loss and cancellation become observable together.
	_, committed, err := run(ctx)

	// Assert runtime loss wins classification and every cause remains inspectable.
	assert.False(t, committed)
	assert.ErrorIs(t, err, runtimeErr)
	assert.ErrorIs(t, err, context.Canceled)
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeExtensionUnavailable, selectionErr.Code)
}

// TestServiceFailsBeforeCommitWhenIssueDeliveryFails verifies both ordinary and delivery causes are retained.
func TestServiceFailsBeforeCommitWhenIssueDeliveryFails(t *testing.T) {
	t.Parallel()
	// Arrange one ordinary handler failure whose issue cannot be delivered.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	publisher := NewMockPublisher(controller)
	runtime := NewMockRuntime(controller)
	contexts := NewMockContextIssuer(controller)
	delivery := NewMockIssueDelivery(controller)
	target := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	binding := extension.Context{
		ID:                "ctx",
		ExtensionID:       "ext",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/tmp",
	}
	handlerErr := errors.New("handler failed")
	deliveryErr := errors.New("issue writer failed")
	catalog.EXPECT().ResolveModel(target.Provider, target.Model).Return(target, nil)
	runtime.EXPECT().HandlerRuntimeAvailable("ext").Return(true)
	contexts.EXPECT().IssueContext("ext").Return(binding, nil)
	runtime.EXPECT().
		HandleSelection(gomock.Any(), "ext", "handler", gomock.Any()).
		Return(HandlerAction{}, false, handlerErr)
	delivery.EXPECT().DeliverSelectionIssue(gomock.Any(), gomock.Any()).Return(deliveryErr)
	service := New(catalog, publisher)
	service.BindHandlers(runtime, contexts, delivery)
	service.CommitSelectionHandlers(
		[]startup.AcceptedRegistration{
			{
				ID:       "ext",
				Path:     "/ext",
				Tools:    nil,
				Handlers: []startup.AcceptedHandler{{ID: "handler", Kind: startup.RawHandlerKindModelSelection}},
			},
		},
	)
	run, release, err := prepareModelForTest(service, target.Provider, target.Model)
	require.NoError(t, err)
	defer release()

	// Act by executing the admitted request.
	_, committed, err := run(t.Context())

	// Assert no catalogue validation or commit occurs and both causes remain inspectable.
	assert.False(t, committed)
	assert.ErrorIs(t, err, handlerErr)
	assert.ErrorIs(t, err, deliveryErr)
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeInternal, selectionErr.Code)
}
