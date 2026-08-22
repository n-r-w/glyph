package extension

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestServiceListTools verifies the complete standard-extension catalog for the read tool.
func TestServiceListTools(t *testing.T) {
	t.Parallel()

	// Arrange: serve the controller through the generated gRPC contract.
	readTool := NewMockReadTool(gomock.NewController(t))
	client := newTestClient(t, readTool)

	// Act: request the fixed startup catalog.
	response, err := client.ListTools(t.Context(), &extensionv1.ListToolsRequest{})

	// Assert: expose the complete read, edit, and bash catalog.
	require.NoError(t, err)
	require.Len(t, response.GetTools(), 3)
	descriptor := response.GetTools()[0]
	assert.Equal(t, "read", descriptor.GetName())
	assert.NotEmpty(t, descriptor.GetDescription())
	assert.JSONEq(t, `{
		"type":"object",
		"properties":{"path":{"type":"string","description":"Path to the text file to read."}},
		"required":["path"],
		"additionalProperties":false
	}`, string(descriptor.GetInputSchemaJson()))
	strict := descriptor.GetConstrainedSampling().GetJsonSchema().GetStrictness()
	assert.Equal(t, extensionv1.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_PREFER, strict)
	assert.Equal(t, "edit", response.GetTools()[1].GetName())
	assert.Equal(t, "bash", response.GetTools()[2].GetName())
}

// TestServiceExecuteRead verifies typed argument decoding and one terminal successful result.
func TestServiceExecuteRead(t *testing.T) {
	t.Parallel()

	// Arrange: require the read use case to receive the validated path.
	readTool := NewMockReadTool(gomock.NewController(t))
	readTool.EXPECT().Read(gomock.Any(), "notes.txt").Return("first\nsecond\n", nil)
	client := newTestClient(t, readTool)

	// Act: execute read through the generated server-streaming contract.
	events, err := receiveExecution(t, client, &extensionv1.ExecuteRequest{
		ToolName:      "read",
		ArgumentsJson: []byte(`{"path":"notes.txt"}`),
	})

	// Assert: emit exactly one terminal result and preserve the complete file text.
	require.NoError(t, err)
	require.Len(t, events, 1)
	result := events[0].GetResult()
	require.NotNil(t, result)
	assert.Equal(t, "first\nsecond\n", result.GetContent())
	assert.False(t, result.GetIsError())
}

// TestServiceExecuteReadError verifies that read failures become terminal model-visible tool errors.
func TestServiceExecuteReadError(t *testing.T) {
	t.Parallel()

	// Arrange: make the read use case reject the requested file.
	readTool := NewMockReadTool(gomock.NewController(t))
	readTool.EXPECT().Read(gomock.Any(), "missing.txt").Return("", errors.New("file does not exist"))
	client := newTestClient(t, readTool)

	// Act: execute read for the missing file.
	events, err := receiveExecution(t, client, &extensionv1.ExecuteRequest{
		ToolName:      "read",
		ArgumentsJson: []byte(`{"path":"missing.txt"}`),
	})

	// Assert: keep the transport successful and return one terminal error result.
	require.NoError(t, err)
	require.Len(t, events, 1)
	result := events[0].GetResult()
	require.NotNil(t, result)
	assert.True(t, result.GetIsError())
	assert.ErrorContains(t, errors.New(result.GetContent()), "file does not exist")
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
			events, err := receiveExecution(t, client, &extensionv1.ExecuteRequest{
				ToolName:      "read",
				ArgumentsJson: argumentsJSON,
			})

			// Assert: return one terminal tool error without a gRPC protocol failure.
			require.NoError(t, err)
			require.Len(t, events, 1)
			result := events[0].GetResult()
			require.NotNil(t, result)
			assert.True(t, result.GetIsError())
			assert.NotEmpty(t, result.GetContent())
		})
	}
}

// TestServiceExecuteRejectsUnknownTool verifies that dispatch is restricted to the listed read tool.
func TestServiceExecuteRejectsUnknownTool(t *testing.T) {
	t.Parallel()

	// Arrange: use a strict mock whose absence of expectations rejects use-case calls.
	readTool := NewMockReadTool(gomock.NewController(t))
	client := newTestClient(t, readTool)

	// Act: request an unregistered tool.
	events, err := receiveExecution(t, client, &extensionv1.ExecuteRequest{
		ToolName:      "write",
		ArgumentsJson: []byte(`{"path":"notes.txt"}`),
	})

	// Assert: return one terminal tool error without invoking read.
	require.NoError(t, err)
	require.Len(t, events, 1)
	result := events[0].GetResult()
	require.NotNil(t, result)
	assert.True(t, result.GetIsError())
	assert.ErrorContains(t, errors.New(result.GetContent()), "unknown tool")
}

// newTestClient serves one controller over an in-memory gRPC connection.
func newTestClient(t *testing.T, readTool ReadTool) extensionv1.ExtensionServiceClient {
	t.Helper()

	service, err := New(
		readTool,
		NewMockEditTool(gomock.NewController(t)),
		NewMockBashTool(gomock.NewController(t)),
	)
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
