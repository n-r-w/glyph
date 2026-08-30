//go:build integration

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"

	"net/http"
	"os"

	"path/filepath"

	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"

	"google.golang.org/grpc/credentials/insecure"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"

	"github.com/n-r-w/glyph/host/internal/infra/persistence"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// programmaticFixture owns one generated client connection and its RPC process.
type programmaticFixture struct {
	cancel       context.CancelFunc
	connection   *grpc.ClientConn
	stream       grpc.BidiStreamingClient[programmaticv1.OpenRequest, programmaticv1.OpenResponse]
	result       <-chan error
	stdoutReader *bufio.Reader
	socketPath   string
}

// startProgrammaticFixture starts an RPC process without extension executables.
func startProgrammaticFixture(t *testing.T, paths persistence.Paths) *programmaticFixture {
	t.Helper()
	return startProgrammaticFixtureWithExtension(t, paths, t.TempDir())
}

// startProgrammaticFixtureWithExtension starts an RPC process and reads its socket announcement.
func startProgrammaticFixtureWithExtension(
	t *testing.T,
	paths persistence.Paths,
	extensionDirectory string,
) *programmaticFixture {
	t.Helper()
	socketDirectory, err := os.MkdirTemp("/tmp", "glyph-rpc-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(socketDirectory)) })
	return startProgrammaticFixtureAtPath(
		t, paths, extensionDirectory, filepath.Join(socketDirectory, "control.sock"),
	)
}

// startProgrammaticFixtureAtPath starts an RPC process at an explicit or automatic socket path.
func startProgrammaticFixtureAtPath(
	t *testing.T,
	paths persistence.Paths,
	extensionDirectory string,
	socketPath string,
) *programmaticFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	reader, writer := io.Pipe()
	results := make(chan error, 1)
	unusedUIDirectory := filepath.Join(t.TempDir(), "must-not-load")
	go func() {
		results <- runWithPaths(ctx, paths, cli.Command{
			Mode:               cli.ModeRPC,
			ExtensionDirectory: extensionDirectory,
			UIDirectory:        unusedUIDirectory,
			UIID:               "must-not-load",
			SocketPath:         socketPath,
			Headless:           headless.Command{},
		}, writer, io.Discard)
		_ = writer.Close()
	}()

	stdoutReader := bufio.NewReader(reader)
	line, err := stdoutReader.ReadString('\n')
	require.NoError(t, err)
	var announcement struct {
		Socket string `json:"socket"`
	}
	require.NoError(t, json.Unmarshal([]byte(line), &announcement))
	if socketPath != "" {
		assert.Equal(t, socketPath, announcement.Socket)
	}
	connection, err := grpc.NewClient(
		"unix://"+announcement.Socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	client := programmaticv1.NewProgrammaticControlServiceClient(connection)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	return &programmaticFixture{
		cancel:       cancel,
		connection:   connection,
		stream:       stream,
		result:       results,
		stdoutReader: stdoutReader,
		socketPath:   announcement.Socket,
	}
}

// closeOwner closes the client send side and requires a clean process result.
func (fixture *programmaticFixture) closeOwner(t *testing.T) {
	t.Helper()
	require.NoError(t, fixture.stream.CloseSend())
	require.NoError(t, <-fixture.result)
	fixture.assertClosed(t)
}

// assertClosed verifies transport, stdout, and socket cleanup.
func (fixture *programmaticFixture) assertClosed(t *testing.T) {
	t.Helper()
	require.NoError(t, fixture.connection.Close())
	fixture.assertStdout(t)
	_, err := os.Lstat(fixture.socketPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	fixture.cancel()
}

// assertStdout verifies that the announcement was the only stdout content.
func (fixture *programmaticFixture) assertStdout(t *testing.T) {
	t.Helper()
	remaining, err := io.ReadAll(fixture.stdoutReader)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

// programmaticTransport blocks the first run and completes the second run.
type runtimeFailureTransport struct {
	body     *atomic.Value
	requests *atomic.Int32
	started  chan<- struct{}
	release  <-chan struct{}
}

func (transport runtimeFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if transport.requests.Add(1) != 1 {
		return nil, errors.New("runtime failure transport received a dependent provider request")
	}
	if transport.started != nil {
		transport.started <- struct{}{}
		<-transport.release
	}
	body, ok := transport.body.Load().(string)
	if !ok {
		return nil, errors.New("runtime failure transport has no response body")
	}
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header),
		Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0,
		TransferEncoding: nil, Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
	}, nil
}

type programmaticTransport struct {
	requestCount *atomic.Int32
	started      chan<- struct{}
}

// RoundTrip returns deterministic provider behavior without network access.
func (transport programmaticTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	switch transport.requestCount.Add(1) {
	case 1:
		if transport.started != nil {
			// The signal proves that provider transport owns the active request before cancellation.
			transport.started <- struct{}{}
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	case 2:
		return &http.Response{
			StatusCode:       http.StatusOK,
			Body:             io.NopCloser(bytes.NewBufferString(finalResponseSSE)),
			Header:           make(http.Header),
			Status:           "",
			Proto:            "",
			ProtoMajor:       0,
			ProtoMinor:       0,
			ContentLength:    0,
			TransferEncoding: nil,
			Close:            false,
			Uncompressed:     false,
			Trailer:          nil,
			Request:          nil,
			TLS:              nil,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected programmatic provider request")
	}
}

// writeProgrammaticCredentials stores credentials accepted by the deterministic provider.
func writeProgrammaticCredentials(t *testing.T, paths persistence.Paths) {
	t.Helper()
	accessToken := semanticAccessToken(t, "account")
	payload := fmt.Sprintf(`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`, accessToken)
	require.NoError(t, os.WriteFile(paths.CredentialsFile, []byte(payload), 0o600))
}

// userRequest builds a generated user-request frame.
func userRequest(correlationID, text string) *programmaticv1.OpenRequest {
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId: new(correlationID),
		UserRequest: programmaticv1.UserRequest_builder{
			Text: new(text),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
	}.Build()
}

// abortRequest builds a generated abort frame.
func abortRequest(correlationID string) *programmaticv1.OpenRequest {
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active Abort field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId:  new(correlationID),
		Abort:          programmaticv1.Abort_builder{}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
	}.Build()
}

// runStateRequest builds a generated run-state frame.
func runStateRequest(correlationID string) *programmaticv1.OpenRequest {
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active GetRunState field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId:  new(correlationID),
		GetRunState:    programmaticv1.GetRunState_builder{}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
	}.Build()
}

// programmaticModelCatalogSettings defines exact text-only and text-and-image catalog fixtures.
func programmaticModelCatalogSettings() string {
	return codexSettings("") + `      - id: gpt-vision
        input: [text, image]
        contextWindow: 262144
        maxTokens: 32768
        toolCapabilities: {}
        pricing:
          input: 0
          output: 0
          cacheRead: 0
          cacheWrite: 0
        reasoning:
          supported: false
          choices: [off]
          default: off
`
}

// getModelsRequest builds a generated model-catalog frame.
func getModelsRequest(correlationID string) *programmaticv1.OpenRequest {
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active GetModels field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId:  new(correlationID),
		GetModels:      programmaticv1.GetModels_builder{}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
	}.Build()
}

// selectModelRequest builds a generated model-selection frame.
func selectModelRequest(correlationID, providerID, modelID string) *programmaticv1.OpenRequest {
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active SelectModel field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId: new(correlationID),
		SelectModel: programmaticv1.SelectModel_builder{
			ProviderId: new(providerID),
			ModelId:    new(modelID),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
	}.Build()
}

// selectReasoningRequest builds a generated reasoning-selection frame.
func selectReasoningRequest(
	correlationID string,
	level programmaticv1.ReasoningChoice,
) *programmaticv1.OpenRequest {
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active SelectReasoningChoice field.
	return programmaticv1.OpenRequest_builder{
		CorrelationId: new(correlationID),
		SelectReasoningChoice: programmaticv1.SelectReasoningChoice_builder{
			Choice: level.Enum(),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
	}.Build()
}
