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
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"

	"github.com/n-r-w/glyph/host/internal/infra/persistence"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
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

// newRuntimeFailureTransport returns a mock provider response with optional synchronization.
func newRuntimeFailureTransport(
	t *testing.T,
	body *atomic.Value,
	requests *atomic.Int32,
	started chan<- struct{},
	release <-chan struct{},
) *MockHTTPRoundTripper {
	t.Helper()
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(
		func(*http.Request) (*http.Response, error) {
			if requests.Add(1) != 1 {
				return nil, errors.New("runtime failure transport received a dependent provider request")
			}
			if started != nil {
				started <- struct{}{}
				<-release
			}
			responseBody, ok := body.Load().(string)
			if !ok {
				return nil, errors.New("runtime failure transport has no response body")
			}
			return &http.Response{
				StatusCode:       http.StatusOK,
				Body:             io.NopCloser(strings.NewReader(responseBody)),
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
		},
	)
	return transport
}

// newProgrammaticTransport returns deterministic cancellation behavior without network access.
func newProgrammaticTransport(
	t *testing.T,
	requestCount *atomic.Int32,
	started chan<- struct{},
) *MockHTTPRoundTripper {
	t.Helper()
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(
		func(request *http.Request) (*http.Response, error) {
			switch requestCount.Add(1) {
			case 1:
				if started != nil {
					// The signal proves that provider transport owns the active request before cancellation.
					started <- struct{}{}
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
		},
	)
	return transport
}

// writeProgrammaticCredentials stores credentials accepted by the deterministic provider.
func writeProgrammaticCredentials(t *testing.T, paths persistence.Paths) {
	t.Helper()
	accessToken := semanticAccessToken(t, "account")
	payload := fmt.Sprintf(
		`{"version":1,"providers":{"openai-codex":{"access_token":%q,`+
			`"refresh_token":"refresh","account_id":"account",`+
			`"expires_at":"2099-01-01T00:00:00Z"}}}`,
		accessToken,
	)
	require.NoError(t, os.WriteFile(paths.CredentialsFile, []byte(payload), 0o600))
}

// userRequest builds one user-request operation frame.
func userRequest(operationID, text string) *programmaticv1.OpenRequest {
	return testProgrammaticRequest(operationID, func(request *programmaticv1.ControllerRequest) {
		payload := new(programmaticv1.UserRequest)
		payload.SetText(text)
		request.SetUserRequest(payload)
	})
}

// cancelRequest builds one targeted cancellation operation frame.
func cancelRequest(operationID, targetOperationID string) *programmaticv1.OpenRequest {
	return testProgrammaticRequest(operationID, func(request *programmaticv1.ControllerRequest) {
		payload := new(operationv1.CancelOperation)
		payload.SetTargetOperationId(targetOperationID)
		request.SetCancel(payload)
	})
}

// runStateRequest builds one run-state operation frame.
func runStateRequest(operationID string) *programmaticv1.OpenRequest {
	return testProgrammaticRequest(operationID, func(request *programmaticv1.ControllerRequest) {
		request.SetGetRunState(new(programmaticv1.GetRunState))
	})
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

// getModelsRequest builds one model-catalog operation frame.
func getModelsRequest(operationID string) *programmaticv1.OpenRequest {
	return testProgrammaticRequest(operationID, func(request *programmaticv1.ControllerRequest) {
		request.SetGetModels(new(programmaticv1.GetModels))
	})
}

// selectModelRequest builds one model-selection operation frame.
func selectModelRequest(operationID, providerID, modelID string) *programmaticv1.OpenRequest {
	return testProgrammaticRequest(operationID, func(request *programmaticv1.ControllerRequest) {
		payload := new(programmaticv1.SelectModel)
		payload.SetProviderId(providerID)
		payload.SetModelId(modelID)
		request.SetSelectModel(payload)
	})
}

// programmaticRequest returns the mutable nested request payload used by fixtures.
func programmaticRequest(request *programmaticv1.OpenRequest) *programmaticv1.ControllerRequest {
	if !request.HasRequest() {
		request.SetRequest(new(programmaticv1.ControllerRequest))
	}
	return request.GetRequest()
}

// sendProgrammaticOperation sends one request and waits for its terminal lifecycle event.
func sendProgrammaticOperation(
	t *testing.T,
	fixture *programmaticFixture,
	operationID string,
	configure func(*programmaticv1.OpenRequest),
) *programmaticv1.HostCompleted {
	t.Helper()
	request := new(programmaticv1.OpenRequest)
	request.SetOperationId(operationID)
	request.SetRequest(new(programmaticv1.ControllerRequest))
	configure(request)
	return completeProgrammaticRequest(t, fixture, request)
}

// sendProgrammaticFailure sends one request and returns its Failed machine code.
func sendProgrammaticFailure(
	t *testing.T,
	fixture *programmaticFixture,
	operationID string,
	configure func(*programmaticv1.OpenRequest),
) string {
	t.Helper()
	request := new(programmaticv1.OpenRequest)
	request.SetOperationId(operationID)
	request.SetRequest(new(programmaticv1.ControllerRequest))
	configure(request)
	require.NoError(t, fixture.stream.Send(request))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() == operationID && response.GetEvent().HasFailed() {
			return response.GetEvent().GetFailed().GetCode()
		}
	}
}

// completeProgrammaticRequest sends an initialized request and waits for completion.
func completeProgrammaticRequest(
	t *testing.T,
	fixture *programmaticFixture,
	request *programmaticv1.OpenRequest,
) *programmaticv1.HostCompleted {
	t.Helper()
	require.NoError(t, fixture.stream.Send(request))
	operationID := request.GetOperationId()
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != operationID || !response.HasEvent() {
			continue
		}
		event := response.GetEvent()
		if event.HasCompleted() {
			return event.GetCompleted()
		}
		if event.HasRejected() {
			require.FailNow(t, "Programmatic operation was rejected", "code: %s", event.GetRejected().GetCode())
		}
		if event.HasFailed() {
			require.FailNow(t, "Programmatic operation failed", "code: %s", event.GetFailed().GetCode())
		}
		if event.HasCanceled() {
			require.FailNow(t, "Programmatic operation was canceled")
		}
	}
}

// selectReasoningRequest builds a reasoning-selection operation request.
func selectReasoningRequest(
	operationID string,
	choice programmaticv1.ReasoningChoice,
) *programmaticv1.OpenRequest {
	return testProgrammaticRequest(operationID, func(request *programmaticv1.ControllerRequest) {
		payload := new(programmaticv1.SelectReasoningChoice)
		payload.SetChoice(choice)
		request.SetSelectReasoningChoice(payload)
	})
}

// testProgrammaticRequest creates one initialized operation request envelope.
func testProgrammaticRequest(
	operationID string,
	configure func(*programmaticv1.ControllerRequest),
) *programmaticv1.OpenRequest {
	request := new(programmaticv1.OpenRequest)
	request.SetOperationId(operationID)
	payload := new(programmaticv1.ControllerRequest)
	configure(payload)
	request.SetRequest(payload)
	return request
}
