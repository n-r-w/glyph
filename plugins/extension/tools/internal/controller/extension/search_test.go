//go:build integration

package extension

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

func TestServiceExecuteFindAndListDispatch(t *testing.T) {
	t.Parallel()
	search := NewMockSearchTool(gomock.NewController(t))
	search.EXPECT().
		Find(gomock.Any(), FindArguments{Pattern: "**/*.go", Path: "src", Limit: mo.Some(uint(2))}).
		Return("src/a.go\n", nil)
	search.EXPECT().List(gomock.Any(), ListArguments{Path: "src", Limit: mo.Some(uint(2))}).Return("a.go\n", nil)
	client := newTestClientWithTools(
		t,
		NewMockReadTool(gomock.NewController(t)),
		NewMockWriteTool(gomock.NewController(t)),
		NewMockEditTool(gomock.NewController(t)),
		search,
	)
	for _, request := range []*extensionv1.ExecuteRequest{
		extensionv1.ExecuteRequest_builder{
			ToolName: new("find"),
			ArgumentsJson: []byte(
				`{"pattern":"**/*.go","path":"src","limit":2}`,
			),
		}.Build(),
		extensionv1.ExecuteRequest_builder{ToolName: new("ls"), ArgumentsJson: []byte(`{"path":"src","limit":2}`)}.Build(),
	} {
		events, err := receiveExecution(t, client, request)
		require.NoError(t, err)
		require.False(t, events[0].GetResult().GetIsError())
	}
}

func TestServiceRejectsInvalidSearchArgumentsBeforeDispatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		tool      string
		arguments string
	}{
		{name: "grep missing pattern", tool: "grep", arguments: `{}`},
		{name: "grep wrong type", tool: "grep", arguments: `{"pattern":1}`},
		{name: "grep invalid minimum", tool: "grep", arguments: `{"pattern":"x","limit":0}`},
		{name: "grep additional property", tool: "grep", arguments: `{"pattern":"x","extra":true}`},
		{name: "find missing pattern", tool: "find", arguments: `{}`},
		{name: "find wrong type", tool: "find", arguments: `{"pattern":1}`},
		{name: "find invalid minimum", tool: "find", arguments: `{"pattern":"*","limit":0}`},
		{name: "find additional property", tool: "find", arguments: `{"pattern":"*","extra":true}`},
		{name: "ls wrong type", tool: "ls", arguments: `{"path":1}`},
		{name: "ls invalid minimum", tool: "ls", arguments: `{"limit":0}`},
		{name: "ls additional property", tool: "ls", arguments: `{"extra":true}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			search := NewMockSearchTool(gomock.NewController(t))
			client := newTestClientWithTools(
				t,
				NewMockReadTool(gomock.NewController(t)),
				NewMockWriteTool(gomock.NewController(t)),
				NewMockEditTool(gomock.NewController(t)),
				search,
			)

			events, err := receiveExecution(t, client, extensionv1.ExecuteRequest_builder{
				ToolName: new(testCase.tool), ArgumentsJson: []byte(testCase.arguments),
			}.Build())

			require.NoError(t, err)
			require.Len(t, events, 1)
			require.True(t, events[0].GetResult().GetIsError())
			require.NotEmpty(t, events[0].GetResult().GetContents()[0].GetText())
		})
	}
}

func TestServiceReturnsSearchOperationErrorsToModel(t *testing.T) {
	t.Parallel()
	search := NewMockSearchTool(gomock.NewController(t))
	search.EXPECT().Grep(gomock.Any(), GrepArguments{
		Pattern: "x", Path: "", Glob: "", IgnoreCase: false, Literal: false, Context: 0, Limit: mo.None[uint](),
	}).Return("", errors.New("grep failed"))
	search.EXPECT().Find(gomock.Any(), FindArguments{Pattern: "*", Path: "", Limit: mo.None[uint]()}).
		Return("", errors.New("find failed"))
	search.EXPECT().
		List(gomock.Any(), ListArguments{Path: "", Limit: mo.None[uint]()}).
		Return("", errors.New("ls failed"))
	client := newTestClientWithTools(
		t,
		NewMockReadTool(gomock.NewController(t)),
		NewMockWriteTool(gomock.NewController(t)),
		NewMockEditTool(gomock.NewController(t)),
		search,
	)
	cases := []struct {
		tool      string
		arguments string
		want      string
	}{
		{tool: "grep", arguments: `{"pattern":"x"}`, want: "grep failed"},
		{tool: "find", arguments: `{"pattern":"*"}`, want: "find failed"},
		{tool: "ls", arguments: `{}`, want: "ls failed"},
	}
	for _, testCase := range cases {
		events, err := receiveExecution(t, client, extensionv1.ExecuteRequest_builder{
			ToolName: new(testCase.tool), ArgumentsJson: []byte(testCase.arguments),
		}.Build())
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.True(t, events[0].GetResult().GetIsError())
		require.Contains(t, events[0].GetResult().GetContents()[0].GetText(), testCase.want)
	}
}

func TestGrepSchemaUsesLimit(t *testing.T) {
	t.Parallel()
	schema, err := compileSchema(grepToolName, grepInputSchemaJSON)
	require.NoError(t, err)
	_, err = validateArguments(schema, []byte(`{"pattern":"a","limit":1}`))
	require.NoError(t, err)
	_, err = validateArguments(schema, []byte(`{"pattern":"a","matchLimit":1}`))
	require.Error(t, err)
}
