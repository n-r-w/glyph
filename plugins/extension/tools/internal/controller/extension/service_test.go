//go:build integration

package extension

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestServiceRegister verifies the complete standard-extension catalog.
func TestServiceRegister(t *testing.T) {
	t.Parallel()

	// Arrange: create the bundled controller through its public SDK service interface.
	readTool := NewMockReadTool(gomock.NewController(t))
	client := newTestClient(t, readTool)

	// Act: prepare and run the fixed startup catalog operation.
	operation, err := client.PrepareRegister(t.Context(), &extensionv1.RegisterRequest{})
	require.NoError(t, err)
	response, err := operation.Run(t.Context())

	// Assert: expose the complete standard tool catalog.
	require.NoError(t, err)
	require.Len(t, response.GetTools(), 7)
	assert.Empty(t, response.GetHandlers())
	descriptor := response.GetTools()[0]
	assert.Equal(t, "read", descriptor.GetName())
	assert.NotEmpty(t, descriptor.GetDescription())
	assert.JSONEq(t, `{
		"type":"object",
		"properties":{
			"path":{"type":"string","description":"Path to the file to read."},
			"offset":{"type":"integer","minimum":1,"description":"One-based line offset."},
			"limit":{"type":"integer","minimum":1,"description":"Maximum number of lines."}
		},
		"required":["path"],
		"additionalProperties":false
	}`, string(descriptor.GetInputSchemaJson()))
	strict := descriptor.GetConstrainedSampling().GetJsonSchema().GetStrictness()
	assert.Equal(t, extensionv1.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_PREFER, strict)
	assert.Equal(t, "write", response.GetTools()[1].GetName())
	assert.Equal(t, "edit", response.GetTools()[2].GetName())
	assert.NotEmpty(t, response.GetTools()[2].GetDescription())
	assert.Equal(t, "grep", response.GetTools()[3].GetName())
	assert.Equal(t, "find", response.GetTools()[4].GetName())
	assert.Equal(t, "ls", response.GetTools()[5].GetName())
	assert.Equal(t, "bash", response.GetTools()[6].GetName())
}

// TestServiceExecuteRead verifies typed argument decoding and one terminal successful result.
func TestServiceExecuteRead(t *testing.T) {
	t.Parallel()

	// Arrange: require the read use case to receive the validated path.
	readTool := NewMockReadTool(gomock.NewController(t))
	readTool.EXPECT().Read(gomock.Any(), "notes.txt", mo.Some(uint(2)), mo.Some(uint(3))).Return(
		ReadResult{Text: mo.Some("first\nsecond\n"), Image: mo.None[ReadImage]()}, nil,
	)
	client := newTestClient(t, readTool)

	// Act: prepare and run read through the public ExecuteOperation interface.
	result, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName:      new("read"),
		ArgumentsJson: []byte(`{"path":"notes.txt","offset":2,"limit":3}`),
	}.Build())

	// Assert: emit exactly one terminal result and preserve the complete file text.
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "first\nsecond\n", result.GetContents()[0].GetText())
	require.Len(t, result.GetContents(), 1)
	assert.Equal(t, "first\nsecond\n", result.GetContents()[0].GetText())
	assert.False(t, result.GetIsError())
}

// TestServiceExecuteGrepDispatchesValidatedArguments verifies validated grep arguments reach the search tool.
func TestServiceExecuteGrepDispatchesValidatedArguments(t *testing.T) {
	t.Parallel()

	// Arrange: expect the search tool to receive normalized grep arguments.
	searchTool := NewMockSearchTool(gomock.NewController(t))
	searchTool.EXPECT().Grep(gomock.Any(), GrepArguments{
		Pattern: "needle", Path: "src", Glob: "", IgnoreCase: false, Literal: false,
		Context: 0, Limit: mo.Some(uint(2)),
	}).Return("src/a.go:1:needle\n", nil)
	client := newTestClientWithTools(
		t,
		NewMockReadTool(gomock.NewController(t)),
		NewMockWriteTool(gomock.NewController(t)),
		NewMockEditTool(gomock.NewController(t)),
		searchTool,
	)

	// Act: prepare and run the public grep operation.
	result, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName: new("grep"), ArgumentsJson: []byte(`{"pattern":"needle","path":"src","limit":2}`),
	}.Build())

	// Assert: return the search output as a successful ToolResult.
	require.NoError(t, err)
	assert.Equal(t, "src/a.go:1:needle\n", result.GetContents()[0].GetText())
	assert.False(t, result.GetIsError())
}

// TestServiceExecuteReadImage verifies typed image bytes reach the extension result.
func TestServiceExecuteReadImage(t *testing.T) {
	t.Parallel()

	// Arrange: make read return one typed PNG payload.
	image := []byte{1, 2, 3}
	readTool := NewMockReadTool(gomock.NewController(t))
	readTool.EXPECT().Read(gomock.Any(), "image.unknown", mo.None[uint](), mo.None[uint]()).Return(
		ReadResult{Text: mo.None[string](), Image: mo.Some(ReadImage{MediaType: "image/png", Data: image})}, nil,
	)

	// Act: prepare and run the public read operation.
	result, err := runExecution(t, newTestClient(t, readTool), extensionv1.ExecuteRequest_builder{
		ToolName: new("read"), ArgumentsJson: []byte(`{"path":"image.unknown"}`),
	}.Build())

	// Assert: preserve the image media type and bytes.
	require.NoError(t, err)
	assert.Equal(t, "image/png", result.GetContents()[0].GetImage().GetMediaType())
	assert.Equal(t, image, result.GetContents()[0].GetImage().GetData())
}

// TestServiceExecuteWrite verifies public write dispatch and success output.
func TestServiceExecuteWrite(t *testing.T) {
	t.Parallel()

	// Arrange: expect one complete file replacement.
	writeTool := NewMockWriteTool(gomock.NewController(t))
	writeTool.EXPECT().Write(gomock.Any(), "nested/notes.txt", "content").Return(nil)
	client := newTestClientWithTools(
		t,
		NewMockReadTool(gomock.NewController(t)),
		writeTool,
		NewMockEditTool(gomock.NewController(t)),
		NewMockSearchTool(gomock.NewController(t)),
	)

	// Act: prepare and run the public write operation.
	result, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName: new("write"), ArgumentsJson: []byte(`{"path":"nested/notes.txt","content":"content"}`),
	}.Build())

	// Assert: return a successful ToolResult.
	require.NoError(t, err)
	assert.False(t, result.GetIsError())
}

// TestServiceExecuteReadError returns use-case failures as terminal tool errors.
func TestServiceExecuteReadError(t *testing.T) {
	t.Parallel()

	// Arrange: make the read use case reject the requested file.
	readTool := NewMockReadTool(gomock.NewController(t))
	readTool.EXPECT().Read(gomock.Any(), "missing.txt", mo.None[uint](), mo.None[uint]()).Return(
		ReadResult{Text: mo.None[string](), Image: mo.None[ReadImage]()}, errors.New("file does not exist"),
	)
	client := newTestClient(t, readTool)

	// Act: execute read for the missing file.
	result, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName:      new("read"),
		ArgumentsJson: []byte(`{"path":"missing.txt"}`),
	}.Build())

	// Assert: return the ordinary read failure as completed ToolResult data.
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.GetIsError())
	assert.ErrorContains(t, errors.New(result.GetContents()[0].GetText()), "file does not exist")
}

// TestServiceExecuteRejectsInvalidArguments verifies the complete read-argument schema at the extension boundary.
func TestServiceExecuteRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	testCases := map[string][]byte{
		"invalid JSON":        []byte(`{"path":`),
		"missing path":        []byte(`{}`),
		"non-string path":     []byte(`{"path":7}`),
		"additional property": []byte(`{"path":"notes.txt","offset":"1"}`),
	}
	for name, argumentsJSON := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange: use a strict mock whose absence of expectations rejects use-case calls.
			readTool := NewMockReadTool(gomock.NewController(t))
			client := newTestClient(t, readTool)

			// Act: submit arguments outside the read schema.
			result, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
				ToolName:      new("read"),
				ArgumentsJson: argumentsJSON,
			}.Build())

			// Assert: return one completed tool error without invoking the read tool.
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.True(t, result.GetIsError())
			assert.NotEmpty(t, result.GetContents()[0].GetText())
		})
	}
}

// TestEditSchemaRejectsEmptySource verifies validation rejects input before edit dispatch.
func TestEditSchemaRejectsEmptySource(t *testing.T) {
	t.Parallel()

	// Arrange: create an edit whose source text is empty.
	arguments := []byte(`{"path":"notes.txt","edits":[{"oldText":"","newText":"new"}]}`)
	schema, err := compileSchema(editToolName, editInputSchemaJSON)
	require.NoError(t, err)
	_, err = validateArguments(schema, arguments)
	require.Error(t, err)

	client := newTestClientWithTools(
		t,
		NewMockReadTool(gomock.NewController(t)),
		NewMockWriteTool(gomock.NewController(t)),
		NewMockEditTool(gomock.NewController(t)),
		NewMockSearchTool(gomock.NewController(t)),
	)

	// Act: prepare and run the invalid edit operation.
	result, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName:      new("edit"),
		ArgumentsJson: arguments,
	}.Build())

	// Assert: return schema rejection as completed ToolResult data.
	require.NoError(t, err)
	assert.True(t, result.GetIsError())
}

// TestServiceExecuteRejectsUnknownTool verifies that dispatch is restricted to the listed read tool.
func TestServiceExecuteRejectsUnknownTool(t *testing.T) {
	t.Parallel()

	// Arrange: use a strict mock whose absence of expectations rejects use-case calls.
	readTool := NewMockReadTool(gomock.NewController(t))
	client := newTestClient(t, readTool)

	// Act: request an unregistered tool.
	result, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName:      new("unknown"),
		ArgumentsJson: []byte(`{"path":"notes.txt"}`),
	}.Build())

	// Assert: return one terminal tool error without invoking read.
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.GetIsError())
	assert.ErrorContains(t, errors.New(result.GetContents()[0].GetText()), "unknown tool")
}

// newTestClient creates one controller with the selected read implementation.
func newTestClient(t *testing.T, readTool ReadTool) *Service {
	return newTestClientWithTools(
		t,
		readTool,
		NewMockWriteTool(gomock.NewController(t)),
		NewMockEditTool(gomock.NewController(t)),
		NewMockSearchTool(gomock.NewController(t)),
	)
}

// newTestClientWithTools creates a bundled controller with selected tool implementations.
func newTestClientWithTools(
	t *testing.T,
	readTool ReadTool,
	writeTool WriteTool,
	editTool EditTool,
	searchTool SearchTool,
) *Service {
	t.Helper()
	return newTestClientWithAllTools(
		t,
		readTool,
		writeTool,
		editTool,
		NewMockBashTool(gomock.NewController(t)),
		searchTool,
	)
}

// newTestClientWithAllTools creates a bundled controller with all selected tool implementations.
func newTestClientWithAllTools(
	t *testing.T,
	readTool ReadTool,
	writeTool WriteTool,
	editTool EditTool,
	bashTool BashTool,
	searchTool SearchTool,
) *Service {
	t.Helper()

	service, err := New(readTool, writeTool, editTool, bashTool, searchTool)
	require.NoError(t, err)
	return service
}

// runExecution prepares and runs one public bundled ExecuteOperation.
func runExecution(
	t *testing.T,
	client *Service,
	request *extensionv1.ExecuteRequest,
) (*extensionv1.ToolResult, error) {
	t.Helper()

	prepared, err := client.PrepareExecute(t.Context(), request)
	if err != nil {
		return nil, err
	}
	return prepared.Run(t.Context(), nil)
}
