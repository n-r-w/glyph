package plugin

import (
	"bytes"

	"testing"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"

	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
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
			_, err := mapLifecycle(modelContentLifecycle(testCase.outer, testCase.nested, testCase.kind))
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
			_, err := mapLifecycle(modelContentLifecycle(
				testCase.outer,
				testCase.nested,
				uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			))
			require.NoError(t, err)
		})
	}
}

// TestMapLifecycleAcceptsPresentZeroPositionAndEmptyText verifies present zero values survive mapping.
func TestMapLifecycleAcceptsPresentZeroPositionAndEmptyText(t *testing.T) {
	t.Parallel()

	event, err := mapLifecycle(uiv1.LifecycleEvent_builder{
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
			Position: new(int32(0)),
			Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			Text:     new(""),
		}.Build(),
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
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
			build := func(isError *bool) *uiv1.LifecycleEvent {
				contents := []*uiv1.ToolResultContent(nil)
				if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT {
					contents = []*uiv1.ToolResultContent{uiv1.ToolResultContent_builder{
						Text:  new(""),
						Image: nil,
					}.Build()}
				}
				return roundTripLifecycle(t, uiv1.LifecycleEvent_builder{
					Type: new(lifecycleType), RunId: new("run"), Text: nil,
					ToolCallId: new("call"), ToolName: new("tool"), ProgressChannel: nil,
					IsError: isError, Outcome: nil, ErrorMessage: nil, Availability: nil,
					ModelContent: nil, ModelResponse: nil, ToolCallPreview: nil, FinalToolCall: nil,
					ToolResultContents: contents,
				}.Build())
			}

			_, err := mapLifecycle(build(nil))
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
		{name: "missing kind", item: uiv1.ModelResponseContent_builder{Kind: nil, Text: new(""), ToolCall: nil}.Build()},
		{name: "unspecified kind", item: uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED), Text: new(""), ToolCall: nil}.Build()},
		{name: "unknown kind", item: uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind(99)), Text: new(""), ToolCall: nil}.Build()},
		{name: "missing text", item: uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT), Text: nil, ToolCall: nil}.Build()},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := mapLifecycle(messageEndLifecycle(t, []*uiv1.ModelResponseContent{test.item}))
			require.Error(t, err)
		})
	}

	event, err := mapLifecycle(messageEndLifecycle(t, []*uiv1.ModelResponseContent{valid}))
	require.NoError(t, err)
	require.Len(t, event.ModelResponseContent, 1)
	assert.Equal(t, mo.Some(""), event.ModelResponseContent[0].Text)
}

// TestOpenRejectsConflictingModelContentDiscriminatorsAsInvalidArgument verifies stream error ownership.
func TestOpenRejectsConflictingModelContentDiscriminatorsAsInvalidArgument(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	factory := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)
	runDone := make(chan struct{})
	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(bytes.NewBuffer(nil))
	session.EXPECT().Output().Return(&bytes.Buffer{})
	factory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(program)
	program.EXPECT().Run().DoAndReturn(func() error { <-runDone; return nil })
	program.EXPECT().Send(gomock.Any()).AnyTimes()
	program.EXPECT().Quit().Do(func() { close(runDone) })
	session.EXPECT().Close().Return(nil)

	client := uisdk.TestClient(t, New(terminal, factory))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(initializationRequest()))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Initialization: nil,
		Lifecycle: modelContentLifecycle(
			uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
			uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
		),
		Authorization:         nil,
		Information:           nil,
		Error:                 nil,
		ModelSelectionChanged: nil,
		SessionList:           nil,
		SessionChanged:        nil,
		SessionInformation:    nil,
	}.Build()))
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
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

			_, err := mapLifecycle(modelContentLifecycleWithText(
				testCase.outer,
				testCase.nested,
				uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
				"",
			))
			require.ErrorContains(t, err, "model content text")
		})
	}
}

// TestOpenRejectsInactiveModelContentTextAsInvalidArgument verifies mapper errors keep gRPC ownership.
func TestOpenRejectsInactiveModelContentTextAsInvalidArgument(t *testing.T) {
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

			mockController := gomock.NewController(t)
			terminal := NewMockTerminal(mockController)
			session := NewMockTerminalSession(mockController)
			factory := NewMockProgramFactory(mockController)
			program := NewMockProgram(mockController)
			runDone := make(chan struct{})
			terminal.EXPECT().Open().Return(session, nil)
			session.EXPECT().Input().Return(bytes.NewBuffer(nil))
			session.EXPECT().Output().Return(&bytes.Buffer{})
			factory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(program)
			program.EXPECT().Run().DoAndReturn(func() error { <-runDone; return nil })
			program.EXPECT().Send(gomock.Any()).AnyTimes()
			program.EXPECT().Quit().Do(func() { close(runDone) })
			session.EXPECT().Close().Return(nil)

			client := uisdk.TestClient(t, New(terminal, factory))
			stream, err := client.Open(t.Context())
			require.NoError(t, err)
			require.NoError(t, stream.Send(initializationRequest()))
			require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
				Initialization: nil,
				Lifecycle: modelContentLifecycleWithText(
					testCase.outer,
					testCase.nested,
					uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
					"malformed",
				),
				Authorization: nil, Information: nil, Error: nil, ModelSelectionChanged: nil,
				SessionList:        nil,
				SessionChanged:     nil,
				SessionInformation: nil,
			}.Build()))
			require.NoError(t, stream.CloseSend())
			_, err = stream.Recv()
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}
