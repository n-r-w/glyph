//go:build integration

package app

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// ordinaryObserverEnvironment selects the real extension lifecycle failure.
	ordinaryObserverEnvironment = "GLYPH_ORDINARY_OBSERVER_FAILURE"
	// ordinaryObserverCause is the complete failure exposed by the public fixture.
	ordinaryObserverCause = "ordinary lifecycle observer full unique original cause"
	// ordinaryObserverLaterSignal records invocation of the second lifecycle observer.
	ordinaryObserverLaterSignal = "ordinary-later-called"
)

// ordinaryObserverTransport supplies completed model work without external network access.
func ordinaryObserverTransport(t *testing.T) *requestBodies {
	t.Helper()
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		position := bodies.append(body)
		response := finalResponseSSE
		if strings.Contains(os.Getenv(ordinaryObserverEnvironment), "TOOL_EXECUTION") && position <= 2 {
			response = strings.NewReplacer(
				`"bash"`, `"external"`, `{\"command\":\"printf tool-ok\"}`, `{\"mode\":\"ordinary\"}`,
			).Replace(toolResponseSSE)
		}
		return &http.Response{
			StatusCode:       http.StatusOK,
			Body:             io.NopCloser(strings.NewReader(response)),
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
	}).AnyTimes()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	return bodies
}

// TestPublicOrdinaryObserverFailure verifies ordinary and terminal observer failures stop real public runs.
func TestPublicOrdinaryObserverFailure(t *testing.T) {
	for _, test := range []struct {
		name string
		kind extensionv1.HandlerKind
	}{
		{name: "AGENT_START", kind: extensionv1.HandlerKind_HANDLER_KIND_AGENT_START},
		{name: "TURN_START", kind: extensionv1.HandlerKind_HANDLER_KIND_TURN_START},
		{name: "MESSAGE_START", kind: extensionv1.HandlerKind_HANDLER_KIND_MESSAGE_START},
		{name: "MESSAGE_UPDATE", kind: extensionv1.HandlerKind_HANDLER_KIND_MESSAGE_UPDATE},
		{name: "MESSAGE_END", kind: extensionv1.HandlerKind_HANDLER_KIND_MESSAGE_END},
		{name: "TURN_END", kind: extensionv1.HandlerKind_HANDLER_KIND_TURN_END},
		{name: "AGENT_END", kind: extensionv1.HandlerKind_HANDLER_KIND_AGENT_END},
		{name: "AGENT_SETTLED", kind: extensionv1.HandlerKind_HANDLER_KIND_AGENT_SETTLED},
		{name: "TOOL_EXECUTION_START", kind: extensionv1.HandlerKind_HANDLER_KIND_TOOL_EXECUTION_START},
		{name: "TOOL_EXECUTION_UPDATE", kind: extensionv1.HandlerKind_HANDLER_KIND_TOOL_EXECUTION_UPDATE},
		{name: "TOOL_EXECUTION_END", kind: extensionv1.HandlerKind_HANDLER_KIND_TOOL_EXECUTION_END},
	} {
		kind := test.name
		t.Run(kind, func(t *testing.T) {
			// Arrange a real extension with two ordered observers and deterministic model work.
			t.Setenv(ordinaryObserverEnvironment, test.kind.String())
			signals := t.TempDir()
			t.Setenv(externalSignalsEnvironment, signals)
			paths := testPaths(t, codexSettings(""))
			writeProgrammaticCredentials(t, paths)
			bodies := ordinaryObserverTransport(t)
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
			defer fixture.closeOwner(t)

			// Act through Programmatic Control until the actual operation terminal.
			failure, _ := requestProgrammaticFailure(t, fixture, userRequest("ordinary-failure", "first request"))

			// Assert failure stops the selected chain and keeps the gate and extension usable.
			require.Equal(t, "EXTENSION_FAILED", failure.Code)
			require.Contains(t, failure.Message, ordinaryObserverCause)
			_, err := os.Stat(filepath.Join(signals, ordinaryObserverLaterSignal))
			require.ErrorIs(t, err, os.ErrNotExist)
			before := len(bodies.snapshot())
			if kind == "AGENT_START" || kind == "TURN_START" || kind == "MESSAGE_START" {
				require.Zero(t, before)
			} else {
				require.Equal(t, 1, before)
			}
			history := sendProgrammaticOperation(
				t,
				fixture,
				"retained-history",
				func(request *programmaticv1.OpenRequest) {
					programmaticRequest(request).SetGetMessages(new(programmaticv1.GetMessages))
				},
			).GetMessages().
				GetEntries()
			if kind == "AGENT_START" {
				require.Empty(t, history)
			} else {
				require.Equal(t, "first request", history[0].GetUser().GetContent()[0].GetText())
			}
			switch kind {
			case "MESSAGE_END", "TURN_END", "AGENT_END", "AGENT_SETTLED":
				require.Len(t, history, 2)
				require.Equal(t, "Request complete.", history[1].GetModel().GetText())
			case "TOOL_EXECUTION_UPDATE", "TOOL_EXECUTION_END":
				require.Len(t, history, 3)
				require.Equal(t, "call-1", history[2].GetToolResult().GetCallId())
				require.NotEmpty(t, history[2].GetToolResult().GetContents())
			}
			completeProgrammaticRequest(t, fixture, userRequest("healthy-next", "second request"))
			additional := 1
			if strings.HasPrefix(kind, "TOOL_EXECUTION") {
				additional = 2
			}
			require.Len(t, bodies.snapshot(), before+additional)
			_, err = os.Stat(filepath.Join(signals, ordinaryObserverLaterSignal))
			require.NoError(t, err)
			state := sendProgrammaticOperation(t, fixture, "idle", func(request *programmaticv1.OpenRequest) {
				programmaticRequest(request).SetGetModels(new(programmaticv1.GetModels))
			})
			require.NotNil(t, state)
		})
	}
}
