//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	// userInputFirstEnvironment selects the first request of the UI fixture.
	userInputFirstEnvironment = "GLYPH_USER_INPUT_FIRST"
	// userInputHandledFirst makes the UI fixture submit text that the input reference handles.
	userInputHandledFirst = "handled"
	// userInputUIBehavior selects the UI fixture that submits ordered text and image input.
	userInputUIBehavior = "user-input"
	// userInputFirstText is the text block before the image.
	userInputFirstText = "describe this"
	// userInputLastText is the text block after the image.
	userInputLastText = "after image"
	// userInputMediaType is the media type of the submitted image.
	userInputMediaType = "image/png"
	// userInputFollowUpText is the text request submitted after the image request.
	userInputFollowUpText = "text follow-up"
	// userInputInvalidCode is the public category of final input that the selected model does not accept.
	userInputInvalidCode = "INPUT_INVALID"
	// userInputInvalidCause is the complete validation cause for the image block of a text-only model.
	userInputInvalidCause = "model gpt-active does not accept image input at content 1"
	// userInputProviderContent is the provider representation of the ordered submitted blocks.
	userInputProviderContent = `[{"type":"input_text","text":"` + userInputFirstText + `"},` +
		`{"type":"input_image","detail":"auto","image_url":"data:image/png;base64,AAEC/w=="},` +
		`{"type":"input_text","text":"` + userInputLastText + `"}]`
)

// userInputImageData is the exact submitted image payload.
var userInputImageData = []byte{0, 1, 2, 255}

// userInputBlock is one observed user content block.
type userInputBlock struct {
	// Text contains a text block.
	Text string `json:"text,omitempty"`
	// MediaType identifies an image block format.
	MediaType string `json:"media_type,omitempty"`
	// Data contains image bytes.
	Data []byte `json:"data,omitempty"`
}

// userInputObservation is the public result of a first request followed by one text request.
type userInputObservation struct {
	// FirstCode is the failure category of the first request; empty means completion.
	FirstCode string `json:"first_code"`
	// FirstError is the complete failure text of the first request.
	FirstError string `json:"first_error"`
	// FirstAgentEvents reports any agent-event payload for the first request.
	FirstAgentEvents bool `json:"first_agent_events"`
	// FollowUpCompleted reports completion of the following text request.
	FollowUpCompleted bool `json:"follow_up_completed"`
	// Users contains the user messages of the session history in order.
	Users [][]userInputBlock `json:"users"`
}

// userInputSettings selects a text-only or an image-capable default model.
func userInputSettings(imageModel bool) string {
	if imageModel {
		return strings.Replace(configuredRequestSettings, "defaultModel: gpt-active", "defaultModel: gpt-test", 1)
	}
	return configuredRequestSettings
}

// userInputTransport records every provider request body and completes each request.
func userInputTransport(t *testing.T) *requestBodies {
	t.Helper()
	bodies := new(requestBodies)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		bodies.append(body)
		return runtimeFailureHTTPResponse(finalResponseSSE), nil
	}).AnyTimes()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	return bodies
}

// programmaticUserContent creates the ordered text, image, and text blocks.
func programmaticUserContent() []*programmaticv1.UserContent {
	first := new(programmaticv1.UserContent)
	first.SetText(userInputFirstText)
	image := new(programmaticv1.UserImage)
	image.SetMediaType(userInputMediaType)
	image.SetData(userInputImageData)
	imageContent := new(programmaticv1.UserContent)
	imageContent.SetImage(image)
	last := new(programmaticv1.UserContent)
	last.SetText(userInputLastText)
	return []*programmaticv1.UserContent{first, imageContent, last}
}

// uiUserContent creates the ordered text, image, and text blocks.
func uiUserContent() []*uiv1.UserContent {
	first := new(uiv1.UserContent)
	first.SetText(userInputFirstText)
	image := new(uiv1.UserImage)
	image.SetMediaType(userInputMediaType)
	image.SetData(userInputImageData)
	imageContent := new(uiv1.UserContent)
	imageContent.SetImage(image)
	last := new(uiv1.UserContent)
	last.SetText(userInputLastText)
	return []*uiv1.UserContent{first, imageContent, last}
}

// runProgrammaticUserInput submits the first request, then a text request, and reads the history.
func runProgrammaticUserInput(
	t *testing.T,
	fixture *programmaticFixture,
	first []*programmaticv1.UserContent,
) userInputObservation {
	t.Helper()
	observation := userInputObservation{
		FirstCode: "", FirstError: "", FirstAgentEvents: false, FollowUpCompleted: false, Users: nil,
	}
	imageRequest := testProgrammaticRequest("first-request", func(request *programmaticv1.ControllerRequest) {
		payload := new(programmaticv1.UserRequest)
		payload.SetContent(first)
		request.SetUserRequest(payload)
	})
	require.NoError(t, fixture.stream.Send(imageRequest))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != imageRequest.GetOperationId() {
			continue
		}
		event := response.GetEvent()
		if event.GetProgress().GetAgentEvent() != nil {
			observation.FirstAgentEvents = true
		}
		if failed := event.GetFailed(); failed != nil {
			observation.FirstCode = failed.GetFailure().GetCode()
			observation.FirstError = failed.GetFailure().GetMessage()
			break
		}
		if event.HasCompleted() {
			break
		}
		require.False(t, event.HasRejected() || event.HasCanceled(), "first request was rejected or canceled")
	}
	completeProgrammaticRequest(t, fixture, userRequest("follow-up-request", userInputFollowUpText))
	observation.FollowUpCompleted = true
	messages := sendProgrammaticOperation(t, fixture, "messages", func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetGetMessages(new(programmaticv1.GetMessages))
	}).GetMessages().GetEntries()
	for _, entry := range messages {
		if !entry.HasUser() {
			continue
		}
		blocks := make([]userInputBlock, 0, len(entry.GetUser().GetContent()))
		for _, content := range entry.GetUser().GetContent() {
			blocks = append(blocks, userInputBlock{
				Text:      content.GetText(),
				MediaType: content.GetImage().GetMediaType(),
				Data:      content.GetImage().GetData(),
			})
		}
		observation.Users = append(observation.Users, blocks)
	}
	return observation
}

// runUserInputUIFixture submits the selected first request, then a text request, and records the session history.
func runUserInputUIFixture(ctx context.Context, host *uisdk.Host) error {
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	observation := userInputObservation{
		FirstCode: "", FirstError: "", FirstAgentEvents: false, FollowUpCompleted: false, Users: nil,
	}
	request := new(uiv1.UIRequest)
	first := uiUserContent()
	if os.Getenv(userInputFirstEnvironment) == userInputHandledFirst {
		handled := new(uiv1.UserContent)
		handled.SetText(inputHandledText)
		first = []*uiv1.UserContent{handled}
	}
	request.SetSubmit(uiv1.SubmitCommand_builder{Content: first}.Build())
	started, err := host.Start(ctx, "image-submit", request)
	if err != nil {
		return err
	}
	idle := false
	_, imageErr := waitUIOperation(ctx, host, "image-submit", started, func(notification *uisdk.Notification) {
		if event := notification.ConnectionEvent(); event != nil {
			idle = event.GetAvailabilityChanged().GetAvailability() == uiv1.Availability_AVAILABILITY_IDLE
		}
		if notification.OperationID() == "image-submit" && notification.Progress().GetAgentEvent() != nil {
			observation.FirstAgentEvents = true
		}
	})
	if imageErr != nil {
		failure, classified := errors.AsType[*uisdk.FailureError](imageErr)
		if !classified {
			return imageErr
		}
		observation.FirstCode = failure.Code()
		observation.FirstError = failure.Error()
	}
	if !idle {
		if err = waitForIdle(ctx, host); err != nil {
			return err
		}
	}
	followUp := new(uiv1.UIRequest)
	text := new(uiv1.UserContent)
	text.SetText(userInputFollowUpText)
	followUp.SetSubmit(uiv1.SubmitCommand_builder{Content: []*uiv1.UserContent{text}}.Build())
	started, err = host.Start(ctx, "follow-up-submit", followUp)
	if err != nil {
		return err
	}
	if _, err = waitUIOperation(ctx, host, "follow-up-submit", started, nil); err != nil {
		return err
	}
	observation.FollowUpCompleted = true
	treeRequest := new(uiv1.UIRequest)
	treeRequest.SetGetSessionTree(new(uiv1.GetSessionTreeCommand))
	started, err = host.Start(ctx, "tree", treeRequest)
	if err != nil {
		return err
	}
	tree, err := waitUIOperation(ctx, host, "tree", started, nil)
	if err != nil {
		return err
	}
	for _, entry := range tree.GetSessionTree().GetTree().GetEntries() {
		if !entry.HasUser() {
			continue
		}
		blocks := make([]userInputBlock, 0, len(entry.GetUser().GetContent()))
		for _, content := range entry.GetUser().GetContent() {
			blocks = append(blocks, userInputBlock{
				Text:      content.GetText(),
				MediaType: content.GetImage().GetMediaType(),
				Data:      content.GetImage().GetData(),
			})
		}
		observation.Users = append(observation.Users, blocks)
	}
	encoded, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	if err = os.WriteFile(os.Getenv(appUITraceEnvironment), encoded, 0o600); err != nil {
		return err
	}
	return host.Close(ctx)
}

// runUIUserInput runs the UI fixture through the assembled Host and returns its observation.
func runUIUserInput(t *testing.T, settings, extensions string) (userInputObservation, *requestBodies) {
	t.Helper()
	paths := testPaths(t, settings)
	writeProgrammaticCredentials(t, paths)
	bodies := userInputTransport(t)
	trace := filepath.Join(t.TempDir(), "user-input.json")
	t.Setenv(appUIBehaviorEnvironment, userInputUIBehavior)
	t.Setenv(appUITraceEnvironment, trace)
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "User_Input_UI")
	require.NoError(t, runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI, Headless: headless.Command{}, ExtensionDirectory: extensions,
		UIDirectory: uiDirectory, UIID: "user-input-ui", SocketPath: "",
	}, &bytes.Buffer{}, &bytes.Buffer{}))
	encoded, err := os.ReadFile(trace)
	require.NoError(t, err)
	var observation userInputObservation
	require.NoError(t, json.Unmarshal(encoded, &observation))
	return observation, bodies
}

// providerUserContent returns the content of every user message of one provider request in order.
func providerUserContent(t *testing.T, body []byte) []string {
	t.Helper()
	var request struct {
		// Input contains the ordered provider history.
		Input []struct {
			// Role identifies the message author.
			Role string `json:"role"`
			// Content contains the message content.
			Content jsontext.Value `json:"content"`
		} `json:"input"`
	}
	require.NoError(t, json.Unmarshal(body, &request))
	var contents []string
	for _, item := range request.Input {
		if item.Role == "user" {
			contents = append(contents, string(item.Content))
		}
	}
	return contents
}

// TestPublicUserInputAcrossClients verifies ordered text and image input and final-input validation.
//
//nolint:paralleltest // The scenarios replace process-global provider HTTP transport and environment.
func TestPublicUserInputAcrossClients(t *testing.T) {
	expectedImageMessage := []userInputBlock{
		{Text: userInputFirstText, MediaType: "", Data: nil},
		{Text: "", MediaType: userInputMediaType, Data: userInputImageData},
		{Text: userInputLastText, MediaType: "", Data: nil},
	}
	expectedFollowUpMessage := []userInputBlock{{Text: userInputFollowUpText, MediaType: "", Data: nil}}
	followUpProviderContent := `[{"type":"input_text","text":"` + userInputFollowUpText + `"}]`
	for _, client := range []string{"programmatic", "ui"} {
		for _, imageModel := range []bool{true, false} {
			name := client + "/text-only-model"
			if imageModel {
				name = client + "/image-model"
			}
			t.Run(name, func(t *testing.T) {
				// Arrange the assembled client boundary and a provider that records every request.
				var observation userInputObservation
				var bodies *requestBodies

				// Act by submitting text and image input, then a text request.
				if client == "programmatic" {
					paths := testPaths(t, userInputSettings(imageModel))
					writeProgrammaticCredentials(t, paths)
					bodies = userInputTransport(t)
					fixture := startProgrammaticFixture(t, paths)
					observation = runProgrammaticUserInput(t, fixture, programmaticUserContent())
					fixture.closeOwner(t)
				} else {
					observation, bodies = runUIUserInput(t, userInputSettings(imageModel), t.TempDir())
				}

				// Assert provider requests, the public result, and the session history.
				actualBodies := bodies.snapshot()
				assert.True(t, observation.FollowUpCompleted)
				if imageModel {
					assert.Empty(t, observation.FirstCode, observation.FirstError)
					assert.True(t, observation.FirstAgentEvents)
					require.Len(t, actualBodies, 2)
					imageContents := providerUserContent(t, actualBodies[0])
					require.Len(t, imageContents, 1)
					assert.JSONEq(t, userInputProviderContent, imageContents[0])
					assert.Equal(
						t,
						[][]userInputBlock{expectedImageMessage, expectedFollowUpMessage},
						observation.Users,
					)
					return
				}
				assert.Equal(t, userInputInvalidCode, observation.FirstCode)
				assert.Contains(t, observation.FirstError, userInputInvalidCause)
				assert.False(t, observation.FirstAgentEvents)
				require.Len(t, actualBodies, 1, "the rejected first request must not reach the provider")
				followUpContents := providerUserContent(t, actualBodies[0])
				require.Len(t, followUpContents, 1)
				assert.JSONEq(t, followUpProviderContent, followUpContents[0])
				assert.Equal(t, [][]userInputBlock{expectedFollowUpMessage}, observation.Users)
			})
		}
	}
}
