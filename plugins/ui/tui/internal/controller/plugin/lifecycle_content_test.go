//go:build !integration

package plugin

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapLifecycleRejectsInvalidModelContentDiscriminators verifies public discriminator consistency.
func TestMapLifecycleRejectsInvalidModelContentDiscriminators(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		outer         uiv1.LifecycleType
		nested        uiv1.ModelContentType
		kind          uiv1.ModelContentKind
		errorContains string
	}{
		"start rejects text delta": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"start rejects end": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"text delta rejects start": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"text delta rejects end": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"end rejects start": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"end rejects text delta": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"present unspecified nested type": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"present unknown nested type": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType(99),
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"present unspecified kind": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED,
			errorContains: "model content kind",
		},
		"present unknown kind": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
			kind:          uiv1.ModelContentKind(99),
			errorContains: "model content kind",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange the inline payload for mapLifecycle to verify public discriminator consistency.
			// Act by invoking mapLifecycle to exercise public discriminator consistency.
			_, err := mapLifecycle(modelContentLifecycle(testCase.outer, testCase.nested, testCase.kind))
			// Assert public discriminator consistency.
			require.ErrorContains(t, err, testCase.errorContains)
		})
	}
}

// TestMapLifecycleAcceptsMatchingModelContentDiscriminators verifies each valid discriminator pair.
func TestMapLifecycleAcceptsMatchingModelContentDiscriminators(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		outer  uiv1.LifecycleType
		nested uiv1.ModelContentType
	}{
		"start": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
		},
		"text delta": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA,
		},
		"end": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange the inline payload for mapLifecycle to verify each valid discriminator pair.
			// Act by invoking mapLifecycle to exercise each valid discriminator pair.
			_, err := mapLifecycle(modelContentLifecycle(
				testCase.outer,
				testCase.nested,
				uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			))
			// Assert each valid discriminator pair.
			require.NoError(t, err)
		})
	}
}

// TestMapLifecycleAcceptsPresentZeroPositionAndEmptyText verifies present zero values survive mapping.
func TestMapLifecycleAcceptsPresentZeroPositionAndEmptyText(t *testing.T) {
	t.Parallel()
	// Arrange the inline payload for mapLifecycle to verify present zero values survive mapping.

	// Act by invoking mapLifecycle to exercise present zero values survive mapping.
	event, err := mapLifecycle(uiv1.AgentEvent_builder{
		Type:            new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
		RunId:           new("run"),
		Text:            nil,
		ToolCallId:      nil,
		ToolName:        nil,
		ProgressChannel: nil,
		IsError:         nil,
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
			Position: new(int64(0)),
			Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			Text:     new(""),
		}.Build(),
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	// Assert present zero values survive mapping.
	require.NoError(t, err)
	assert.Equal(t, mo.Some(0), event.Position)
	assert.Equal(t, mo.Some(""), event.Text)
}

// TestMapLifecycleRequiresToolFailurePresence verifies absent false differs from present false on the wire.
func TestMapLifecycleRequiresToolFailurePresence(t *testing.T) {
	t.Parallel()

	for _, lifecycleType := range []uiv1.LifecycleType{
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT,
	} {
		t.Run(lifecycleType.String(), func(t *testing.T) {
			t.Parallel()
			// Arrange build and contents for mapLifecycle to verify absent false differs from present false on the wire.
			build := func(isError *bool) *uiv1.AgentEvent {
				contents := []*uiv1.ToolResultContent(nil)
				if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT {
					contents = []*uiv1.ToolResultContent{uiv1.ToolResultContent_builder{
						Text:  new(""),
						Image: nil,
					}.Build()}
				}
				return roundTripLifecycle(t, uiv1.AgentEvent_builder{
					Type: new(lifecycleType), RunId: new("run"), Text: nil,
					ToolCallId: new("call"), ToolName: new("tool"), ProgressChannel: nil,
					IsError: isError, Outcome: nil, ErrorMessage: nil, Availability: nil,
					ModelContent: nil, ModelResponse: nil, ToolCallPreview: nil, FinalToolCall: nil,
					ToolResultContents: contents,
				}.Build())
			}

			// Act by invoking mapLifecycle to exercise absent false differs from present false on the wire.
			_, err := mapLifecycle(build(nil))
			// Assert absent false differs from present false on the wire.
			require.Error(t, err)
			event, err := mapLifecycle(build(new(false)))
			require.NoError(t, err)
			assert.Equal(t, mo.Some(false), event.Failure)
		})
	}
}

// TestMapLifecycleValidatesFinalResponseContent verifies malformed items fail and present empty text survives.
func TestMapLifecycleValidatesFinalResponseContent(t *testing.T) {
	t.Parallel()

	valid := uiv1.ModelResponseContent_builder{
		Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
		Text: new(""), ToolCall: nil,
	}.Build()
	invalid := []struct {
		name string
		item *uiv1.ModelResponseContent
	}{
		{name: "nil item", item: nil},
		{
			name: "missing kind",
			item: uiv1.ModelResponseContent_builder{Kind: nil, Text: new(""), ToolCall: nil}.Build(),
		},
		{
			name: "unspecified kind",
			item: uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED), Text: new(""), ToolCall: nil,
			}.Build(),
		},
		{
			name: "unknown kind",
			item: uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind(99)), Text: new(""), ToolCall: nil,
			}.Build(),
		},
		{
			name: "missing text",
			item: uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT), Text: nil, ToolCall: nil,
			}.Build(),
		},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange the inline payload for mapLifecycle to verify malformed items fail and present empty text survives.
			// Act by invoking mapLifecycle to exercise malformed items fail and present empty text survives.
			_, err := mapLifecycle(messageEndLifecycle(t, []*uiv1.ModelResponseContent{test.item}))
			// Assert malformed items fail and present empty text survives.
			require.Error(t, err)
		})
	}

	event, err := mapLifecycle(messageEndLifecycle(t, []*uiv1.ModelResponseContent{valid}))
	require.NoError(t, err)
	require.Len(t, event.ModelResponseContent, 1)
	assert.Equal(t, mo.Some(""), event.ModelResponseContent[0].Text)
}

// TestMapLifecycleRejectsInactiveModelContentText verifies structural variants reject nested text.
func TestMapLifecycleRejectsInactiveModelContentText(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		outer  uiv1.LifecycleType
		nested uiv1.ModelContentType
	}{
		"content start": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
		},
		"content end": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange the inline payload for mapLifecycle to verify structural variants reject nested text.

			// Act by invoking mapLifecycle to exercise structural variants reject nested text.
			_, err := mapLifecycle(modelContentLifecycleWithText(
				testCase.outer,
				testCase.nested,
				uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
				"",
			))
			// Assert structural variants reject nested text.
			require.ErrorContains(t, err, "model content text")
		})
	}
}
