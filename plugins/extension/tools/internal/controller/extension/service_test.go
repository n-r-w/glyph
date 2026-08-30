//go:build integration

package extension

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestServiceListTools verifies the complete standard-extension catalog.
func TestServiceListTools(t *testing.T) {
	t.Parallel()

	// Arrange: serve the controller through the generated gRPC contract.
	readTool := NewMockReadTool(gomock.NewController(t))
	client := newTestClient(t, readTool)

	// Act: request the fixed startup catalog.
	response, err := client.ListTools(t.Context(), &extensionv1.ListToolsRequest{})

	// Assert: expose the complete standard tool catalog.
	require.NoError(t, err)
	require.Len(t, response.GetTools(), 7)
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
	assert.Equal(t, "Apply ordered unique exact text replacements to a project file.", response.GetTools()[2].GetDescription())
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

	// Act: execute read through the generated server-streaming contract.
	events, err := receiveExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName:      new("read"),
		ArgumentsJson: []byte(`{"path":"notes.txt","offset":2,"limit":3}`),
	}.Build())

	// Assert: emit exactly one terminal result and preserve the complete file text.
	require.NoError(t, err)
	require.Len(t, events, 1)
	result := events[0].GetResult()
	require.NotNil(t, result)
	assert.Equal(t, "first\nsecond\n", result.GetContents()[0].GetText())
	require.Len(t, result.GetContents(), 1)
	assert.Equal(t, "first\nsecond\n", result.GetContents()[0].GetText())
	assert.False(t, result.GetIsError())
}

func TestServiceExecuteGrepDispatchesValidatedArguments(t *testing.T) {
	t.Parallel()

	searchTool := NewMockSearchTool(gomock.NewController(t))
	searchTool.EXPECT().Grep(gomock.Any(), GrepArguments{Pattern: "needle", Path: "src", Glob: "", IgnoreCase: false, Literal: false, Context: 0, Limit: mo.Some(uint(2))}).Return("src/a.go:1:needle\n", nil)
	client := newTestClientWithTools(t, NewMockReadTool(gomock.NewController(t)), NewMockWriteTool(gomock.NewController(t)), NewMockEditTool(gomock.NewController(t)), searchTool)

	events, err := receiveExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName: new("grep"), ArgumentsJson: []byte(`{"pattern":"needle","path":"src","limit":2}`),
	}.Build())

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "src/a.go:1:needle\n", events[0].GetResult().GetContents()[0].GetText())
	assert.False(t, events[0].GetResult().GetIsError())
}

// TestServiceExecuteReadImage verifies typed image bytes reach the extension result.
func TestServiceExecuteReadImage(t *testing.T) {
	t.Parallel()

	image := []byte{1, 2, 3}
	readTool := NewMockReadTool(gomock.NewController(t))
	readTool.EXPECT().Read(gomock.Any(), "image.unknown", mo.None[uint](), mo.None[uint]()).Return(
		ReadResult{Text: mo.None[string](), Image: mo.Some(ReadImage{MediaType: "image/png", Data: image})}, nil,
	)

	events, err := receiveExecution(t, newTestClient(t, readTool), extensionv1.ExecuteRequest_builder{
		ToolName: new("read"), ArgumentsJson: []byte(`{"path":"image.unknown"}`),
	}.Build())

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "image/png", events[0].GetResult().GetContents()[0].GetImage().GetMediaType())
	assert.Equal(t, image, events[0].GetResult().GetContents()[0].GetImage().GetData())
}

// TestServiceExecuteWrite verifies public write dispatch and success output.
func TestServiceExecuteWrite(t *testing.T) {
	t.Parallel()

	writeTool := NewMockWriteTool(gomock.NewController(t))
	writeTool.EXPECT().Write(gomock.Any(), "nested/notes.txt", "content").Return(nil)
	client := newTestClientWithTools(
		t,
		NewMockReadTool(gomock.NewController(t)),
		writeTool,
		NewMockEditTool(gomock.NewController(t)),
		NewMockSearchTool(gomock.NewController(t)),
	)

	events, err := receiveExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName: new("write"), ArgumentsJson: []byte(`{"path":"nested/notes.txt","content":"content"}`),
	}.Build())

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.False(t, events[0].GetResult().GetIsError())
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
	events, err := receiveExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName:      new("read"),
		ArgumentsJson: []byte(`{"path":"missing.txt"}`),
	}.Build())

	// Assert: keep the transport successful and return one terminal error result.
	require.NoError(t, err)
	require.Len(t, events, 1)
	result := events[0].GetResult()
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
			events, err := receiveExecution(t, client, extensionv1.ExecuteRequest_builder{
				ToolName:      new("read"),
				ArgumentsJson: argumentsJSON,
			}.Build())

			// Assert: return one terminal tool error without a gRPC protocol failure.
			require.NoError(t, err)
			require.Len(t, events, 1)
			result := events[0].GetResult()
			require.NotNil(t, result)
			assert.True(t, result.GetIsError())
			assert.NotEmpty(t, result.GetContents()[0].GetText())
		})
	}
}

// TestEditSchemaRejectsEmptySource verifies validation rejects input before edit dispatch.
func TestEditSchemaRejectsEmptySource(t *testing.T) {
	t.Parallel()

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

	events, err := receiveExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName:      new("edit"),
		ArgumentsJson: arguments,
	}.Build())

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.True(t, events[0].GetResult().GetIsError())
}

// TestServiceExecuteRejectsUnknownTool verifies that dispatch is restricted to the listed read tool.
func TestServiceExecuteRejectsUnknownTool(t *testing.T) {
	t.Parallel()

	// Arrange: use a strict mock whose absence of expectations rejects use-case calls.
	readTool := NewMockReadTool(gomock.NewController(t))
	client := newTestClient(t, readTool)

	// Act: request an unregistered tool.
	events, err := receiveExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName:      new("unknown"),
		ArgumentsJson: []byte(`{"path":"notes.txt"}`),
	}.Build())

	// Assert: return one terminal tool error without invoking read.
	require.NoError(t, err)
	require.Len(t, events, 1)
	result := events[0].GetResult()
	require.NotNil(t, result)
	assert.True(t, result.GetIsError())
	assert.ErrorContains(t, errors.New(result.GetContents()[0].GetText()), "unknown tool")
}

// newTestClient serves one controller over an in-memory gRPC connection.
func newTestClient(t *testing.T, readTool ReadTool) extensionv1.ExtensionServiceClient {
	return newTestClientWithTools(t, readTool, NewMockWriteTool(gomock.NewController(t)), NewMockEditTool(gomock.NewController(t)), NewMockSearchTool(gomock.NewController(t)))
}

// newTestClientWithTools serves selected use cases over an in-memory gRPC connection.
func newTestClientWithTools(t *testing.T, readTool ReadTool, writeTool WriteTool, editTool EditTool, searchTool SearchTool) extensionv1.ExtensionServiceClient {
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

// newTestClientWithAllTools serves the selected tool implementations through bufconn.
func newTestClientWithAllTools(
	t *testing.T,
	readTool ReadTool,
	writeTool WriteTool,
	editTool EditTool,
	bashTool BashTool,
	searchTool SearchTool,
) extensionv1.ExtensionServiceClient {
	t.Helper()

	service, err := New(readTool, writeTool, editTool, bashTool, searchTool)
	require.NoError(t, err)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	extensionv1.RegisterExtensionServiceServer(server, service)
	go func() {
		assert.NoError(t, server.Serve(listener))
	}()
	t.Cleanup(func() {
		server.Stop()
		assert.NoError(t, listener.Close())
	})

	connection, err := grpc.NewClient(
		"passthrough:///extension-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, connection.Close())
	})
	return extensionv1.NewExtensionServiceClient(connection)
}

// receiveExecution collects the complete finite execution stream for assertions.
func receiveExecution(
	t *testing.T,
	client extensionv1.ExtensionServiceClient,
	request *extensionv1.ExecuteRequest,
) ([]*extensionv1.ExecuteResponse, error) {
	t.Helper()

	stream, err := client.Execute(t.Context(), request)
	if err != nil {
		return nil, err
	}

	events := make([]*extensionv1.ExecuteResponse, 0, 1)
	for {
		event, receiveErr := stream.Recv()
		if errors.Is(receiveErr, io.EOF) {
			return events, nil
		}
		if receiveErr != nil {
			return nil, receiveErr
		}
		events = append(events, event)
	}
}
