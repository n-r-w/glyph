//go:build integration

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// inputTraceEnvironment names the trace file written by the input reference extension.
	inputTraceEnvironment = "GLYPH_INPUT_TRACE"
	// inputHandledText is user text that the input reference fully handles.
	inputHandledText = "glyph-handle-input please"
	// inputTextSuffix is appended to every text block by the input reference.
	inputTextSuffix = " glyph-input"
	// inputRemovedImageText replaces every image block in the input reference replacement.
	inputRemovedImageText = "[image removed]"
	// inputFailureText is the complete ordinary error of the input failure scenario.
	inputFailureText = "reference input handler failed"
	// inputInvalidActionText is the complete cause of the invalid-action scenario.
	inputInvalidActionText = `middleware handler "input-text" returned invalid user-input action 0`
	// extensionFailedCode is the public category of input-chain failures.
	extensionFailedCode = "EXTENSION_FAILED"
)

// inputHandlers lists the registration order of the input reference handlers.
var inputHandlers = []string{"input-handle", "input-text", "input-images"}

// buildInputFixture builds the public-only input reference with the selected executable scenario.
func buildInputFixture(t *testing.T, mode string) string {
	t.Helper()
	directory := t.TempDir()
	command := exec.CommandContext(t.Context(), "go", "build", "-o", filepath.Join(directory, mode), "./cmd/input")
	command.Dir = filepath.Join(repoRoot(t), "testdata", "external-plugins")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return directory
}

// enableInputTrace makes the input reference extension record each invocation.
func enableInputTrace(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input-trace.tsv")
	t.Setenv(inputTraceEnvironment, path)
	return path
}

// readInputTrace returns the recorded invocations; an absent trace means no invocation.
func readInputTrace(t *testing.T, path string) []contextTraceEntry {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return readContextTrace(t, path)
}

// decodeInputInvocation decodes one traced public user-input payload.
func decodeInputInvocation(t *testing.T, entry contextTraceEntry) *extensionpb.UserInputInvocation {
	t.Helper()
	invocation := new(extensionpb.UserInputInvocation)
	require.NoError(t, protojson.Unmarshal([]byte(entry.payload), invocation))
	return invocation
}

// handledContent creates one text block that the input reference fully handles.
func handledContent() []*programmaticv1.UserContent {
	content := new(programmaticv1.UserContent)
	content.SetText(inputHandledText)
	return []*programmaticv1.UserContent{content}
}

// transformedFollowUp is the follow-up message after the input reference transformed its text.
func transformedFollowUp() []userInputBlock {
	return []userInputBlock{{Text: userInputFollowUpText + inputTextSuffix, MediaType: "", Data: nil}}
}

// transformedFollowUpProvider is the provider content of the transformed follow-up message.
const transformedFollowUpProvider = `[{"type":"input_text","text":"` + userInputFollowUpText + inputTextSuffix + `"}]`

// assertImageChainTrace checks the order and the original/current values of a complete input chain.
func assertImageChainTrace(t *testing.T, entries []contextTraceEntry) {
	t.Helper()
	require.GreaterOrEqual(t, len(entries), len(inputHandlers))
	assert.Equal(t, inputHandlers, contextTraceHandlers(entries[:len(inputHandlers)]))
	images := decodeInputInvocation(t, entries[2])
	original := images.GetOriginal().GetContent()
	current := images.GetCurrent().GetContent()
	require.Len(t, original, 3)
	require.Len(t, current, 3)
	assert.Equal(t, userInputFirstText, original[0].GetText())
	assert.Equal(t, userInputImageData, original[1].GetImage().GetData())
	assert.Equal(t, userInputLastText, original[2].GetText())
	assert.Equal(t, userInputFirstText+inputTextSuffix, current[0].GetText())
	assert.Equal(t, userInputImageData, current[1].GetImage().GetData())
	assert.Equal(t, userInputLastText+inputTextSuffix, current[2].GetText())
}

// TestPublicInputMiddlewareProgrammatic verifies replacement, final validation, handling and failures.
//
//nolint:paralleltest,maintidx // The scenarios replace process-global provider HTTP transport and environment.
func TestPublicInputMiddlewareProgrammatic(t *testing.T) {
	removedImageMessage := []userInputBlock{
		{Text: userInputFirstText + inputTextSuffix, MediaType: "", Data: nil},
		{Text: inputRemovedImageText, MediaType: "", Data: nil},
		{Text: userInputLastText + inputTextSuffix, MediaType: "", Data: nil},
	}
	retainedImageMessage := []userInputBlock{
		{Text: userInputFirstText + inputTextSuffix, MediaType: "", Data: nil},
		{Text: "", MediaType: userInputMediaType, Data: userInputImageData},
		{Text: userInputLastText + inputTextSuffix, MediaType: "", Data: nil},
	}
	for _, test := range []struct {
		// name identifies the scenario.
		name string
		// mode selects the reference executable.
		mode string
		// imageModel selects the image-capable default model.
		imageModel bool
		// first contains the first request.
		first []*programmaticv1.UserContent
		// code is the expected failure category of the first request; empty means completion.
		code string
		// cause is the expected complete failure text.
		cause string
		// handlers lists the handlers invoked for the first request.
		handlers []string
		// users contains the expected session history.
		users [][]userInputBlock
		// providerFirst is the expected provider content of the first request; empty means no request.
		providerFirst string
	}{
		{
			name: "removed image on text-only model", mode: "input", imageModel: false,
			first: programmaticUserContent(), code: "", cause: "", handlers: inputHandlers,
			users: [][]userInputBlock{removedImageMessage, transformedFollowUp()},
			providerFirst: `[{"type":"input_text","text":"` + userInputFirstText + inputTextSuffix + `"},` +
				`{"type":"input_text","text":"` + inputRemovedImageText + `"},` +
				`{"type":"input_text","text":"` + userInputLastText + inputTextSuffix + `"}]`,
		},
		{
			name: "retained image on text-only model", mode: "input-keep-images", imageModel: false,
			first: programmaticUserContent(), code: userInputInvalidCode,
			cause:    "process user input: model gpt-active does not accept image input at content 1",
			handlers: inputHandlers, users: [][]userInputBlock{transformedFollowUp()}, providerFirst: "",
		},
		{
			name: "retained image on image model", mode: "input-keep-images", imageModel: true,
			first: programmaticUserContent(), code: "", cause: "", handlers: inputHandlers,
			users: [][]userInputBlock{retainedImageMessage, transformedFollowUp()},
			providerFirst: `[{"type":"input_text","text":"` + userInputFirstText + inputTextSuffix + `"},` +
				`{"type":"input_image","detail":"auto","image_url":"data:image/png;base64,AAEC/w=="},` +
				`{"type":"input_text","text":"` + userInputLastText + inputTextSuffix + `"}]`,
		},
		{
			name: "handled input", mode: "input", imageModel: false, first: handledContent(), code: "", cause: "",
			handlers: inputHandlers[:1], users: [][]userInputBlock{transformedFollowUp()}, providerFirst: "",
		},
		{
			name: "handler failure", mode: "input-fail", imageModel: false, first: programmaticUserContent(),
			code: extensionFailedCode, cause: inputFailureText, handlers: inputHandlers[:2],
			users: [][]userInputBlock{transformedFollowUp()}, providerFirst: "",
		},
		{
			name: "invalid action", mode: "input-invalid", imageModel: false, first: programmaticUserContent(),
			code: extensionFailedCode, cause: inputInvalidActionText, handlers: inputHandlers[:2],
			users: [][]userInputBlock{transformedFollowUp()}, providerFirst: "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange the real reference extension process, Host assembly and recording provider.
			trace := enableInputTrace(t)
			paths := testPaths(t, userInputSettings(test.imageModel))
			writeProgrammaticCredentials(t, paths)
			bodies := userInputTransport(t)
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildInputFixture(t, test.mode))

			// Act by submitting the first request, a text follow-up and a history query.
			observation := runProgrammaticUserInput(t, fixture, test.first)
			fixture.closeOwner(t)

			// Assert the first result, invoked handlers, provider requests and durable history.
			assert.Equal(t, test.code, observation.FirstCode)
			assert.Contains(t, observation.FirstError, test.cause)
			assert.Equal(t, test.providerFirst != "", observation.FirstAgentEvents)
			assert.True(t, observation.FollowUpCompleted)
			entries := readInputTrace(t, trace)
			require.Len(t, entries, len(test.handlers)+len(inputHandlers))
			assert.Equal(t, test.handlers, contextTraceHandlers(entries[:len(test.handlers)]))
			if len(test.handlers) == len(inputHandlers) {
				assertImageChainTrace(t, entries)
			}
			received := bodies.snapshot()
			if test.providerFirst == "" {
				require.Len(t, received, 1)
			} else {
				require.Len(t, received, 2)
				firstContents := providerUserContent(t, received[0])
				require.Len(t, firstContents, 1)
				assert.JSONEq(t, test.providerFirst, firstContents[0])
			}
			followUpContents := providerUserContent(t, received[len(received)-1])
			assert.JSONEq(t, transformedFollowUpProvider, followUpContents[len(followUpContents)-1])
			assert.Equal(t, test.users, observation.Users)
		})
	}
}

// TestPublicInputMiddlewareUI verifies image removal, handling and failure through the public UI SDK.
func TestPublicInputMiddlewareUI(t *testing.T) {
	for _, test := range []struct {
		// name identifies the scenario.
		name string
		// mode selects the reference executable.
		mode string
		// first selects the first UI request.
		first string
		// code is the expected failure category of the first request; empty means completion.
		code string
		// requests is the expected number of provider requests.
		requests int
		// users is the expected number of user messages in the session history.
		users int
	}{
		{name: "removed image", mode: "input", first: "", code: "", requests: 2, users: 2},
		{name: "handled input", mode: "input", first: userInputHandledFirst, code: "", requests: 1, users: 1},
		{name: "handler failure", mode: "input-fail", first: "", code: extensionFailedCode, requests: 1, users: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange the real UI and reference extension processes on a text-only model.
			trace := enableInputTrace(t)
			t.Setenv(userInputFirstEnvironment, test.first)

			// Act by submitting the first request, a text follow-up and a tree query through the UI SDK.
			observation, bodies := runUIUserInput(t, userInputSettings(false), buildInputFixture(t, test.mode))

			// Assert the first result, availability recovery, provider requests and durable history.
			assert.Equal(t, test.code, observation.FirstCode)
			if test.code != "" {
				assert.Contains(t, observation.FirstError, inputFailureText)
			}
			assert.Equal(t, test.requests == 2, observation.FirstAgentEvents)
			assert.True(t, observation.FollowUpCompleted)
			assert.Len(t, bodies.snapshot(), test.requests)
			require.Len(t, observation.Users, test.users)
			assert.Equal(t, transformedFollowUp(), observation.Users[test.users-1])
			assert.NotEmpty(t, readInputTrace(t, trace))
		})
	}
}
