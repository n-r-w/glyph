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
	"github.com/n-r-w/glyph/host/internal/domain/model"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestSelectionRequestUsesDirectSelectionPort verifies controller validation, direct preparation, and typed completion mapping.
func TestSelectionRequestUsesDirectSelectionPort(t *testing.T) {
	t.Parallel()

	// Arrange: provide a valid issued context and one committed result with ordered diagnostics.
	controller := gomock.NewController(t)
	contexts := NewMockContextOperations(controller)
	models := NewMockModelOperations(controller)
	runtime := NewMockRuntimeOperations(controller)
	selection := NewMockModelSelection(controller)
	preparedSelection := NewMockPreparedSelection(controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	committed := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
	runtime.EXPECT().BeginContextOperation(gomock.Any(), "extension", "runtime").Return(func() {}, nil)
	selection.EXPECT().PrepareExtensionSelection(SelectionCommand{
		Kind: SelectionCommandModel, ExtensionID: "extension", RuntimeID: "runtime", Context: reference,
		Provider: "provider", Model: "model", ReasoningChoice: "",
	}).Return(preparedSelection, nil)
	preparedSelection.EXPECT().Run(gomock.Any()).Return(SelectionResult{
		Selection: committed, Committed: true,
		Issues: []SelectionIssue{
			{ExtensionID: "first", HandlerID: "handler-a", Code: "HANDLER_ERROR", Message: "first issue"},
			{
				ExtensionID: "second", HandlerID: "handler-b", Code: "INVALID_HANDLER_ACTION",
				Message: "second issue",
			},
			{ExtensionID: "third", HandlerID: "observer", Code: "OBSERVER_ERROR", Message: "third issue"},
		},
		Source: nil,
	})
	preparedSelection.EXPECT().Release()
	service := New(models, contexts, runtime, "extension", "runtime")
	service.BindSelection(selection)
	request := new(extensionpb.ExtensionRequest)
	request.SetSelectModel(extensionpb.SelectModelRequest_builder{
		Context: extensionpb.ExtensionContextRef_builder{
			ContextId: new("context"), RuntimeInstanceId: new("runtime"), SessionId: new("session"),
		}.Build(),
		ProviderId: new("provider"), ModelId: new("model"),
	}.Build())

	// Act: prepare and execute the public extension request.
	operation, err := service.Prepare(t.Context(), "selection-operation", request)
	require.NoError(t, err)
	result, err := operation.Run(t.Context(), nil)
	operation.Release()

	// Assert: the direct port result maps to one complete typed selection result with ordered issues.
	require.NoError(t, err)
	require.NotNil(t, result.GetSelection())
	assert.Equal(t, "provider", result.GetSelection().GetSelection().GetProviderId())
	assert.Equal(t, "model", result.GetSelection().GetSelection().GetModelId())
	assert.Equal(t, "high", result.GetSelection().GetSelection().GetReasoningChoice())
	require.Len(t, result.GetSelection().GetIssues(), 3)
	assert.Equal(
		t,
		extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_HANDLER_ERROR,
		result.GetSelection().GetIssues()[0].GetCode(),
	)
	assert.Equal(t, "first issue", result.GetSelection().GetIssues()[0].GetMessage())
	assert.Equal(
		t,
		extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_INVALID_HANDLER_ACTION,
		result.GetSelection().GetIssues()[1].GetCode(),
	)
	assert.Equal(
		t,
		extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_OBSERVER_ERROR,
		result.GetSelection().GetIssues()[2].GetCode(),
	)
}

// TestSelectionRequestRejectsMissingRequiredFields verifies transport validation before shared admission.
func TestSelectionFailureMappingsUsePublicCodes(t *testing.T) {
	t.Parallel()

	// Arrange: define every shared selection category exposed at extension preparation or execution.
	testCases := []struct {
		name       string
		sharedCode string
		publicCode string
		prepare    bool
	}{
		{name: "busy", sharedCode: "busy", publicCode: "BUSY", prepare: true},
		{name: "not found", sharedCode: "not_found", publicCode: "NOT_FOUND", prepare: true},
		{
			name: "reasoning unsupported", sharedCode: "reasoning_unsupported",
			publicCode: "REASONING_UNSUPPORTED", prepare: true,
		},
		{name: "stale context", sharedCode: "stale_context", publicCode: "STALE_CONTEXT", prepare: false},
		{name: "model unavailable", sharedCode: "model_unavailable", publicCode: "MODEL_UNAVAILABLE", prepare: false},
		{
			name: "credential unavailable", sharedCode: "credential_unavailable",
			publicCode: "CREDENTIAL_UNAVAILABLE", prepare: false,
		},
		{
			name: "extension rejected", sharedCode: "extension_rejected",
			publicCode: "EXTENSION_REJECTED", prepare: false,
		},
		{
			name: "extension unavailable", sharedCode: "extension_unavailable",
			publicCode: "EXTENSION_UNAVAILABLE", prepare: false,
		},
		{name: "internal", sharedCode: "internal", publicCode: "INTERNAL", prepare: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange one classified source with complete diagnostic text.
			source := selectionFailureValue{code: testCase.sharedCode, cause: errors.New("complete selection cause")}

			// Act: map preparation or accepted-operation failure at the public boundary.
			var mapped error
			if testCase.prepare {
				mapped = mapSelectionRejection(source)
			} else {
				_, mapped = mapSelectionResult(SelectionResult{
					Selection: model.Selection{}, Committed: false, Issues: nil, Source: source,
				})
			}

			// Assert: public category is exact and complete source text remains in the chain.
			require.Error(t, mapped)
			assert.Contains(t, mapped.Error(), "complete selection cause")
			if testCase.prepare {
				var rejection *extensionsdk.RejectionError
				require.ErrorAs(t, mapped, &rejection)
				assert.Equal(t, testCase.publicCode, rejection.Code())
			} else {
				var failure *extensionsdk.FailureError
				require.ErrorAs(t, mapped, &failure)
				assert.Equal(t, testCase.publicCode, failure.Code())
			}
		})
	}
}

// TestSelectionResultPreservesPurePreCommitCancellation verifies cancellation is not wrapped as a failure.
func TestSelectionResultPreservesPurePreCommitCancellation(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		// name identifies the canceled pre-commit phase.
		name string
		// code is the shared category that wrapped cancellation before projection.
		code string
	}{
		{name: "binding protection", code: selectionCodeInternal},
		{name: "credential validation", code: selectionCodeCredentialUnavailable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange pure cancellation wrapped by the phase's typed selection error.
			source := selectionFailureValue{code: testCase.code, cause: context.Canceled}

			// Act through extension completion projection.
			_, err := mapSelectionResult(SelectionResult{
				Selection: model.Selection{}, Committed: false, Issues: nil, Source: source,
			})

			// Assert shared operation handling can classify cancellation without an explicit failure wrapper.
			require.ErrorIs(t, err, context.Canceled)
			var failure *extensionsdk.FailureError
			assert.False(t, errors.As(err, &failure))
		})
	}
}

// TestSelectionResultKeepsMixedFailureCategory verifies cancellation does not hide an independent real failure.
func TestSelectionResultKeepsMixedFailureCategory(t *testing.T) {
	t.Parallel()

	// Arrange runtime loss joined with concurrent caller cancellation.
	runtimeErr := errors.New("selection handler runtime exited")
	source := selectionFailureValue{
		code: selectionCodeExtensionUnavailable, cause: errors.Join(context.Canceled, runtimeErr),
	}

	// Act through extension completion projection.
	_, err := mapSelectionResult(SelectionResult{
		Selection: model.Selection{}, Committed: false, Issues: nil, Source: source,
	})

	// Assert the owning failure category and both complete causes remain available.
	var failure *extensionsdk.FailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, publicSelectionCodeExtensionUnavailable, failure.Code())
	assert.ErrorIs(t, err, context.Canceled)
	assert.ErrorIs(t, err, runtimeErr)
	assert.Contains(t, err.Error(), "selection handler runtime exited")
}

// TestSelectionRequestRejectsMissingRequiredFields verifies transport validation before shared admission.
func TestSelectionRequestRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	// Arrange: omit the required model identifier from a complete context reference.
	controller := gomock.NewController(t)
	service := New(
		NewMockModelOperations(controller),
		NewMockContextOperations(controller),
		NewMockRuntimeOperations(controller),
		"extension",
		"runtime",
	)
	service.BindSelection(NewMockModelSelection(controller))
	request := new(extensionpb.ExtensionRequest)
	request.SetSelectModel(extensionpb.SelectModelRequest_builder{
		Context: extensionpb.ExtensionContextRef_builder{
			ContextId: new("context"), RuntimeInstanceId: new("runtime"), SessionId: new("session"),
		}.Build(),
		ProviderId: new("provider"), ModelId: new(""),
	}.Build())

	// Act: prepare the malformed public request.
	_, err := service.Prepare(t.Context(), "selection-operation", request)

	// Assert: malformed fields are rejected before context or selection dependencies are called.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}

// selectionFailureValue supplies one typed failure value for public mapping tests.
type selectionFailureValue struct {
	// code identifies the shared internal category.
	code string
	// cause contains complete diagnostic text.
	cause error
}

// Error returns complete source text.
func (e selectionFailureValue) Error() string { return e.cause.Error() }

// Unwrap exposes the source cause.
func (e selectionFailureValue) Unwrap() error { return e.cause }

// ModelSelectionCode returns the shared internal category.
func (e selectionFailureValue) ModelSelectionCode() string { return e.code }
