// Package plugin maps the public UI contract to the standard terminal presentation.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// Controller maps the public UI stream to one terminal presentation program.
type Controller struct {
	uiv1.UnimplementedUIServiceServer

	terminal Terminal
	programs ProgramFactory
}

var _ uiv1.UIServiceServer = (*Controller)(nil)

// New creates the standard TUI plugin controller.
func New(terminal Terminal, programs ProgramFactory) *Controller {
	return &Controller{
		UnimplementedUIServiceServer: uiv1.UnimplementedUIServiceServer{},
		terminal:                     terminal,
		programs:                     programs,
	}
}

// GetCapabilities returns immutable capabilities without opening the terminal.
func (*Controller) GetCapabilities(
	context.Context,
	*uiv1.GetCapabilitiesRequest,
) (*uiv1.GetCapabilitiesResponse, error) {
	return uiv1.GetCapabilitiesResponse_builder{
		ControlsTerminal: new(true),
	}.Build(), nil
}

// Open runs one initialized Host-to-TUI stream until either side completes.
func (controller *Controller) Open(
	stream grpc.BidiStreamingServer[uiv1.OpenRequest, uiv1.OpenResponse],
) (returnErr error) {
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, "receive initialization: %v", err)
	}
	if first.GetInitialization() == nil {
		return status.Error(codes.FailedPrecondition, "initialization must be the first UI frame")
	}
	initial, err := mapRequest(first)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "map initialization: %v", err)
	}

	terminalSession, err := controller.terminal.Open()
	if err != nil {
		return status.Errorf(codes.Internal, "open terminal: %v", err)
	}
	defer func() {
		if closeErr := terminalSession.Close(); closeErr != nil && returnErr == nil {
			returnErr = status.Errorf(codes.Internal, "close terminal: %v", closeErr)
		}
	}()

	var sendMutex sync.Mutex
	emit := func(command presentationdomain.Command) error {
		response, mapErr := mapCommand(command)
		if mapErr != nil {
			return mapErr
		}
		sendMutex.Lock()
		defer sendMutex.Unlock()
		if sendErr := stream.Send(response); sendErr != nil {
			return fmt.Errorf("send UI command: %w", sendErr)
		}
		return nil
	}
	program := controller.programs.New(initial, terminalSession.Input(), terminalSession.Output(), emit)
	programResult := make(chan error, 1)
	go func() {
		programResult <- program.Run()
	}()
	receiveResult := make(chan error, 1)
	go receiveFrames(stream, program, receiveResult)

	select {
	case runErr := <-programResult:
		if runErr != nil {
			return status.Errorf(codes.Internal, "run TUI: %v", runErr)
		}
		return nil
	case receiveErr := <-receiveResult:
		program.Quit()
		runErr := <-programResult
		if runErr != nil {
			return status.Errorf(codes.Internal, "run TUI: %v", runErr)
		}
		if errors.Is(receiveErr, io.EOF) {
			return nil
		}
		return receiveErr
	}
}

// receiveFrames maps the ordered Host stream into the active presentation program.
func receiveFrames(
	stream grpc.BidiStreamingServer[uiv1.OpenRequest, uiv1.OpenResponse],
	program Program,
	result chan<- error,
) {
	for {
		request, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				result <- io.EOF
			} else {
				result <- status.Errorf(codes.Unavailable, "receive Host frame: %v", err)
			}
			return
		}
		event, err := mapRequest(request)
		if err != nil {
			result <- status.Errorf(codes.InvalidArgument, "map Host frame: %v", err)
			return
		}
		if event.Kind == presentationdomain.EventInitialization {
			result <- status.Error(codes.FailedPrecondition, "initialization may only be sent once")
			return
		}
		program.Send(event)
	}
}

// mapRequest validates and projects one public UI frame into presentation state.
func mapRequest(request *uiv1.OpenRequest) (presentationdomain.Event, error) {
	if request == nil {
		return presentationdomain.Event{}, errors.New("frame is nil")
	}
	if initialization := request.GetInitialization(); initialization != nil {
		return mapInitialization(initialization)
	}
	if lifecycle := request.GetLifecycle(); lifecycle != nil {
		return mapLifecycle(lifecycle)
	}
	if event, handled, err := mapSessionRequest(request); handled {
		return event, err
	}
	if event, handled, err := mapTextRequest(request); handled {
		return event, err
	}
	if safeError := request.GetError(); safeError != nil {
		if !safeError.HasText() {
			return presentationdomain.Event{}, errors.New("error text is required")
		}
		if !safeError.HasRetryAuthentication() {
			return presentationdomain.Event{}, errors.New("error retry authentication is required")
		}
		availability := mo.None[presentationdomain.Availability]()
		if safeError.GetRetryAuthentication() {
			availability = mo.Some(presentationdomain.AvailabilityAuthenticationFailed)
		}
		return presentationdomain.Event{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventError,
			Startup:              nil,
			Extensions:           nil,
			Availability:         availability,
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[presentationdomain.OutputStream](),
			Text:                 mo.Some(safeError.GetText()),
			Contents:             mo.None[[]presentationdomain.Content](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			SessionInfo:          mo.None[presentationdomain.SessionInfo](),
			Sessions:             nil,
		}, nil
	}
	if changed := request.GetModelSelectionChanged(); changed != nil {
		selection, err := mapModelSelection(changed.GetSelection())
		if err != nil {
			return presentationdomain.Event{}, err
		}
		return presentationdomain.Event{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventModelSelectionChanged,
			Startup:              nil,
			Extensions:           nil,
			Availability:         mo.None[presentationdomain.Availability](),
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[presentationdomain.OutputStream](),
			Text:                 mo.None[string](),
			Contents:             mo.None[[]presentationdomain.Content](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.Some(selection),
			SessionInfo:          mo.None[presentationdomain.SessionInfo](),
			Sessions:             nil,
		}, nil
	}
	return presentationdomain.Event{}, errors.New("frame content is missing")
}

// mapTextRequest maps authorization and information payloads that share text-only presentation state.
func mapTextRequest(request *uiv1.OpenRequest) (presentationdomain.Event, bool, error) {
	var kind presentationdomain.EventKind
	var text string
	if authorization := request.GetAuthorization(); authorization != nil {
		if !authorization.HasUrl() {
			return presentationdomain.Event{}, true, errors.New("authorization URL is required")
		}
		kind = presentationdomain.EventAuthorization
		text = authorization.GetUrl()
	} else if information := request.GetInformation(); information != nil {
		if !information.HasText() {
			return presentationdomain.Event{}, true, errors.New("information text is required")
		}
		kind = presentationdomain.EventInformation
		text = information.GetText()
	} else {
		return presentationdomain.Event{}, false, nil
	}
	return presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 kind,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.Some(text),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	}, true, nil
}

// mapSessionRequest validates lifecycle frames before they enter TUI state.
func mapSessionRequest(request *uiv1.OpenRequest) (presentationdomain.Event, bool, error) {
	if listed := request.GetSessionList(); listed != nil {
		summaries := make([]presentationdomain.SessionSummary, 0, len(listed.GetSessions()))
		for _, value := range listed.GetSessions() {
			mapped, err := mapSessionSummary(value)
			if err != nil {
				return presentationdomain.Event{}, true, err
			}
			summaries = append(summaries, mapped)
		}
		return sessionEvent(
			presentationdomain.EventSessionList, mo.None[presentationdomain.SessionInfo](), summaries, nil,
		), true, nil
	}
	if changed := request.GetSessionChanged(); changed != nil {
		info, err := mapSessionInfo(changed.GetInfo())
		if err != nil {
			return presentationdomain.Event{}, true, err
		}
		restored, err := mapRestoredTranscript(changed.GetEntries())
		if err != nil {
			return presentationdomain.Event{}, true, err
		}
		return sessionEvent(
			presentationdomain.EventSessionChanged, mo.Some(info), nil, restored,
		), true, nil
	}
	if information := request.GetSessionInformation(); information != nil {
		info, err := mapSessionInfo(information.GetInfo())
		if err != nil {
			return presentationdomain.Event{}, true, err
		}
		return sessionEvent(
			presentationdomain.EventSessionInformation, mo.Some(info), nil, nil,
		), true, nil
	}
	return presentationdomain.Event{}, false, nil
}

// sessionEvent initializes fields that are absent from session lifecycle frames.
func sessionEvent(
	kind presentationdomain.EventKind,
	info mo.Option[presentationdomain.SessionInfo],
	sessions []presentationdomain.SessionSummary,
	restored []presentationdomain.Line,
) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript:   restored,
		Kind:                 kind,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          info,
		Sessions:             sessions,
	}
}

// mapRestoredTranscript rebuilds public transcript lines without replaying lifecycle events.
func mapRestoredTranscript(entries []*uiv1.SessionEntry) ([]presentationdomain.Line, error) {
	lines := make([]presentationdomain.Line, 0, len(entries))
	for _, entry := range entries {
		if user := entry.GetUser(); user != nil {
			contents, text, err := mapRestoredContents(user.GetContent())
			if err != nil {
				return nil, err
			}
			lines = append(lines, presentationdomain.Line{
				Kind: presentationdomain.LineUser, ToolName: mo.None[string](), Status: mo.None[string](),
				Text: mo.Some(text), Contents: mo.Some(contents),
			})
			continue
		}
		if response := entry.GetModel(); response != nil {
			mapped, err := mapRestoredModelResponse(response)
			if err != nil {
				return nil, err
			}
			lines = append(lines, mapped...)
			continue
		}
		if result := entry.GetToolResult(); result != nil {
			mapped, err := mapRestoredToolResult(result)
			if err != nil {
				return nil, err
			}
			lines = append(lines, mapped)
		}
	}
	return lines, nil
}

// mapRestoredContents maps ordered user text and images with owned image bytes.
func mapRestoredContents(contents []*uiv1.UserContent) ([]presentationdomain.Content, string, error) {
	mapped := make([]presentationdomain.Content, 0, len(contents))
	var text strings.Builder
	for index, content := range contents {
		if content == nil {
			return nil, "", fmt.Errorf("restored user content %d is missing", index)
		}
		switch content.WhichContent() {
		case uiv1.UserContent_Text_case:
			value := content.GetText()
			mapped = append(mapped, presentationdomain.Content{
				Text: mo.Some(value), MediaType: mo.None[string](), Data: mo.None[[]byte](),
			})
			text.WriteString(value)
		case uiv1.UserContent_Image_case:
			image := content.GetImage()
			if image == nil || image.GetMediaType() == "" {
				return nil, "", fmt.Errorf("restored user image %d is invalid", index)
			}
			data := bytes.Clone(image.GetData())
			mapped = append(mapped, presentationdomain.Content{
				Text: mo.None[string](), MediaType: mo.Some(image.GetMediaType()), Data: mo.Some(data),
			})
			text.WriteString(imagePlaceholder(image.GetMediaType(), len(data)))
		case uiv1.UserContent_Content_not_set_case:
			return nil, "", fmt.Errorf("restored user content %d is missing", index)
		default:
			return nil, "", fmt.Errorf("restored user content %d is invalid", index)
		}
	}
	return mapped, text.String(), nil
}

// mapRestoredModelResponse keeps stored model content and diagnostics in display order.
func mapRestoredModelResponse(response *uiv1.ModelResponse) ([]presentationdomain.Line, error) {
	lines := make([]presentationdomain.Line, 0, len(response.GetContent()))
	for _, content := range response.GetContent() {
		if call := content.GetToolCall(); call != nil {
			arguments, err := json.Marshal(call.GetArguments().AsMap())
			if err != nil {
				return nil, fmt.Errorf("map restored tool call: %w", err)
			}
			lines = append(lines, presentationdomain.Line{
				Kind: presentationdomain.LineToolStatus, ToolName: mo.Some(call.GetName()),
				Status: mo.Some("arguments"), Text: mo.Some(string(arguments)),
				Contents: mo.None[[]presentationdomain.Content](),
			})
			continue
		}
		kind := presentationdomain.LineModel
		switch content.GetKind() {
		case uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED,
			uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT:
		case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL:
			kind = presentationdomain.LineRefusal
		case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING:
			kind = presentationdomain.LineReasoning
		}
		lines = append(lines, presentationdomain.Line{
			Kind: kind, ToolName: mo.None[string](), Status: mo.None[string](),
			Text: mo.Some(content.GetText()), Contents: mo.None[[]presentationdomain.Content](),
		})
	}
	for _, diagnostic := range response.GetDiagnostics() {
		lines = append(lines, presentationdomain.Line{
			Kind: presentationdomain.LineInformation, ToolName: mo.None[string](), Status: mo.None[string](),
			Text:     mo.Some(diagnostic.GetCode() + ": " + diagnostic.GetMessage()),
			Contents: mo.None[[]presentationdomain.Content](),
		})
	}
	if outcome := response.GetOutcome(); outcome == "aborted" || outcome == "failed" {
		if response.HasErrorMessage() {
			lines = append(lines, presentationdomain.Line{
				Kind: presentationdomain.LineError, ToolName: mo.None[string](), Status: mo.None[string](),
				Text: mo.Some(response.GetErrorMessage()), Contents: mo.None[[]presentationdomain.Content](),
			})
		}
	}
	return lines, nil
}

// mapRestoredToolResult uses the same terminal line kinds as live tool completion.
func mapRestoredToolResult(result *uiv1.ToolResult) (presentationdomain.Line, error) {
	contents, err := mapContents(result.GetContents(), true)
	if err != nil {
		return presentationdomain.Line{}, fmt.Errorf("map restored tool result: %w", err)
	}
	kind := presentationdomain.LineToolDone
	if result.GetIsError() {
		kind = presentationdomain.LineToolError
	}
	return presentationdomain.Line{
		Kind: kind, ToolName: mo.Some(result.GetToolName()), Status: mo.None[string](),
		Text: mo.Some(restoredToolResultText(contents)), Contents: mo.Some(contents),
	}, nil
}

func restoredToolResultText(contents []presentationdomain.Content) string {
	var result strings.Builder
	for _, content := range contents {
		if text, present := content.Text.Get(); present {
			result.WriteString(text)
			continue
		}
		mediaType, hasMediaType := content.MediaType.Get()
		data, hasData := content.Data.Get()
		if hasMediaType && hasData {
			result.WriteString(imagePlaceholder(mediaType, len(data)))
		}
	}
	return result.String()
}

func imagePlaceholder(mediaType string, size int) string {
	return fmt.Sprintf("[image %s, %d bytes]", mediaType, size)
}

// mapSessionInfo validates required identity, project, and timestamp fields while preserving optional values.
func mapSessionInfo(value *uiv1.SessionInfo) (presentationdomain.SessionInfo, error) {
	if value == nil || !value.HasId() || !value.HasWorkingDirectory() ||
		!value.HasCreatedTime() || !value.HasUpdateTime() {
		return presentationdomain.SessionInfo{}, errors.New("session information is incomplete")
	}
	return presentationdomain.SessionInfo{
		ID:               value.GetId(),
		Name:             value.GetName(),
		NamePresent:      value.HasName(),
		WorkingDirectory: value.GetWorkingDirectory(),
		StoragePath:      value.GetStoragePath(),
		StoragePresent:   value.HasStoragePath(),
		CreatedAt:        value.GetCreatedTime().AsTime(),
		UpdatedAt:        value.GetUpdateTime().AsTime(),
	}, nil
}

// mapSessionSummary validates one selector row and preserves first-user-text presence.
func mapSessionSummary(value *uiv1.SessionSummary) (presentationdomain.SessionSummary, error) {
	if value == nil || !value.HasInfo() || !value.HasTotalMessages() {
		return presentationdomain.SessionSummary{}, errors.New("session summary is incomplete")
	}
	info, err := mapSessionInfo(value.GetInfo())
	if err != nil {
		return presentationdomain.SessionSummary{}, err
	}
	return presentationdomain.SessionSummary{
		Info:          info,
		FirstUserText: value.GetFirstUserText(),
		TextPresent:   value.HasFirstUserText(),
		TotalMessages: value.GetTotalMessages(),
	}, nil
}

// mapInitialization validates the complete first frame before the TUI takes terminal ownership.
func mapInitialization(initialization *uiv1.Initialization) (presentationdomain.Event, error) {
	if !initialization.HasSelectedUiId() {
		return presentationdomain.Event{}, errors.New("selected UI ID is required")
	}
	if !initialization.HasAvailability() {
		return presentationdomain.Event{}, errors.New("availability is required")
	}
	availability, err := mapAvailability(initialization.GetAvailability())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	selection, err := mapModelSelection(initialization.GetModelSelection())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	startup, err := mapInitializationStartup(initialization.GetStartupContent())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	extensions, err := mapInitializationExtensions(initialization.GetExtensions())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	models, err := mapInitializationModels(initialization.GetModels())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	sessionInfo, err := mapSessionInfo(initialization.GetSessionInfo())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	event := presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInitialization,
		Startup:              startup,
		Extensions:           extensions,
		Availability:         mo.Some(availability),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               models,
		ModelSelection:       mo.Some(selection),
		SessionInfo:          mo.Some(sessionInfo),
		Sessions:             nil,
	}
	return event, nil
}

// mapInitializationStartup validates and maps startup lines.
func mapInitializationStartup(contents []*uiv1.StartupContent) ([]presentationdomain.Line, error) {
	return lo.MapErr(contents, func(content *uiv1.StartupContent, _ int) (presentationdomain.Line, error) {
		if !content.HasSeverity() {
			return presentationdomain.Line{}, errors.New("startup content severity is required")
		}
		if !content.HasText() {
			return presentationdomain.Line{}, errors.New("startup content text is required")
		}
		var kind presentationdomain.LineKind
		switch content.GetSeverity() {
		case uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION:
			kind = presentationdomain.LineInformation
		case uiv1.ContentSeverity_CONTENT_SEVERITY_ERROR:
			kind = presentationdomain.LineError
		case uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING:
			kind = presentationdomain.LineWarning
		case uiv1.ContentSeverity_CONTENT_SEVERITY_UNSPECIFIED:
			return presentationdomain.Line{}, errors.New("startup content severity is unspecified")
		default:
			return presentationdomain.Line{}, fmt.Errorf("unknown startup content severity %d", content.GetSeverity())
		}
		return presentationdomain.Line{
			Kind:     kind,
			Text:     mo.Some(content.GetText()),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		}, nil
	})
}

// mapInitializationExtensions validates and maps extension availability.
func mapInitializationExtensions(extensions []*uiv1.ExtensionAvailability) ([]presentationdomain.Extension, error) {
	return lo.MapErr(extensions, func(extension *uiv1.ExtensionAvailability, _ int) (presentationdomain.Extension, error) {
		if !extension.HasPluginId() {
			return presentationdomain.Extension{}, errors.New("extension plugin ID is required")
		}
		if !extension.HasPath() {
			return presentationdomain.Extension{}, errors.New("extension path is required")
		}
		return presentationdomain.Extension{
			ID:    extension.GetPluginId(),
			Path:  extension.GetPath(),
			Tools: slices.Clone(extension.GetTools()),
		}, nil
	})
}

// mapInitializationModels validates and maps configured models.
func mapInitializationModels(models []*uiv1.ConfiguredModel) ([]presentationdomain.ConfiguredModel, error) {
	return lo.MapErr(models, func(configured *uiv1.ConfiguredModel, _ int) (presentationdomain.ConfiguredModel, error) {
		if !configured.HasProviderId() {
			return presentationdomain.ConfiguredModel{}, errors.New("configured model provider ID is required")
		}
		if !configured.HasModelId() {
			return presentationdomain.ConfiguredModel{}, errors.New("configured model ID is required")
		}
		reasoning := configured.GetReasoning()
		if reasoning == nil {
			return presentationdomain.ConfiguredModel{}, errors.New("model reasoning capabilities are missing")
		}
		if !reasoning.HasSupported() {
			return presentationdomain.ConfiguredModel{}, errors.New("model reasoning support is required")
		}
		if !reasoning.HasDefaultChoice() {
			return presentationdomain.ConfiguredModel{}, errors.New("model reasoning default choice is required")
		}
		choices, err := lo.MapErr(
			reasoning.GetChoices(),
			func(choice uiv1.ReasoningChoice, _ int) (presentationdomain.ReasoningChoice, error) {
				return mapReasoningChoice(choice)
			},
		)
		if err != nil {
			return presentationdomain.ConfiguredModel{}, err
		}
		defaultChoice, err := mapReasoningChoice(reasoning.GetDefaultChoice())
		if err != nil {
			return presentationdomain.ConfiguredModel{}, err
		}
		return presentationdomain.ConfiguredModel{
			ProviderID: configured.GetProviderId(),
			ModelID:    configured.GetModelId(),
			Reasoning: presentationdomain.ReasoningCapabilities{
				Supported: reasoning.GetSupported(),
				Choices:   choices,
				Default:   defaultChoice,
			},
		}, nil
	})
}

// mapModelSelection validates one Host-confirmed selection.
func mapModelSelection(selection *uiv1.ModelSelection) (presentationdomain.ModelSelection, error) {
	if selection == nil {
		return presentationdomain.ModelSelection{}, errors.New("model selection is invalid")
	}
	if !selection.HasProviderId() {
		return presentationdomain.ModelSelection{}, errors.New("model selection provider ID is required")
	}
	if !selection.HasModelId() {
		return presentationdomain.ModelSelection{}, errors.New("model selection model ID is required")
	}
	if !selection.HasReasoningChoice() {
		return presentationdomain.ModelSelection{}, errors.New("model selection reasoning choice is required")
	}
	if selection.GetProviderId() == "" || selection.GetModelId() == "" {
		return presentationdomain.ModelSelection{}, errors.New("model selection is invalid")
	}
	level, err := mapReasoningChoice(selection.GetReasoningChoice())
	if err != nil {
		return presentationdomain.ModelSelection{}, err
	}
	return presentationdomain.ModelSelection{
		ProviderID:      selection.GetProviderId(),
		ModelID:         selection.GetModelId(),
		ReasoningChoice: level,
	}, nil
}

// mapReasoningChoice validates the complete public reasoning enum.
func mapReasoningChoice(level uiv1.ReasoningChoice) (presentationdomain.ReasoningChoice, error) {
	switch level {
	case uiv1.ReasoningChoice_REASONING_CHOICE_OFF:
		return presentationdomain.ReasoningChoiceOff, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_ON:
		return presentationdomain.ReasoningChoiceOn, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL:
		return presentationdomain.ReasoningChoiceMinimal, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_LOW:
		return presentationdomain.ReasoningChoiceLow, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM:
		return presentationdomain.ReasoningChoiceMedium, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_HIGH:
		return presentationdomain.ReasoningChoiceHigh, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH:
		return presentationdomain.ReasoningChoiceXHigh, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_MAX:
		return presentationdomain.ReasoningChoiceMax, nil
	case uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED:
		return presentationdomain.ReasoningChoiceUnspecified, errors.New("reasoning choice is unspecified")
	default:
		return presentationdomain.ReasoningChoiceUnspecified, fmt.Errorf("unknown reasoning choice %d", level)
	}
}

func mapLifecycle(lifecycle *uiv1.LifecycleEvent) (presentationdomain.Event, error) {
	if lifecycle == nil {
		return presentationdomain.Event{}, errors.New("lifecycle event is nil")
	}
	if err := validateLifecycleEnvelope(lifecycle); err != nil {
		return presentationdomain.Event{}, err
	}
	event := presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventUnspecified,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	}

	var err error
	switch lifecycle.GetType() {
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START:
		event.Kind = presentationdomain.EventTurnStarted
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START:
		event.Kind = presentationdomain.EventModelDelta
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
		err = mapModelLifecycle(&event, lifecycle)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END:
		err = mapToolCallLifecycle(&event, lifecycle)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
		err = mapToolLifecycle(&event, lifecycle)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED:
		err = mapTerminalLifecycle(&event, lifecycle)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED:
		if !lifecycle.HasAvailability() {
			return presentationdomain.Event{}, errors.New("availability is missing")
		}
		availability, mapErr := mapAvailability(lifecycle.GetAvailability())
		if mapErr != nil {
			return presentationdomain.Event{}, mapErr
		}
		event.Kind = presentationdomain.EventAvailability
		event.Availability = mo.Some(availability)
	case uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED:
		return presentationdomain.Event{}, errors.New("lifecycle type is unspecified")
	default:
		return presentationdomain.Event{}, fmt.Errorf("unknown lifecycle type %d", lifecycle.GetType())
	}
	if err != nil {
		return presentationdomain.Event{}, err
	}
	return event, nil
}

// lifecycleFields is a presence mask for optional LifecycleEvent payload fields.
type lifecycleFields uint16

const (
	lifecycleFieldType lifecycleFields = 1 << iota
	lifecycleFieldRunID
	lifecycleFieldText
	lifecycleFieldToolCallID
	lifecycleFieldToolName
	lifecycleFieldProgressChannel
	lifecycleFieldIsError
	lifecycleFieldOutcome
	lifecycleFieldErrorMessage
	lifecycleFieldAvailability
	lifecycleFieldModelContent
	lifecycleFieldModelResponse
	lifecycleFieldToolCallPreview
	lifecycleFieldFinalToolCall
	lifecycleFieldContents
)

// validateLifecycleEnvelope validates shared fields and rejects fields owned by inactive variants.
func validateLifecycleEnvelope(lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasType() {
		return errors.New("lifecycle type is missing")
	}
	if lifecycle.GetType() != uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED && !lifecycle.HasRunId() {
		return errors.New("lifecycle run ID is missing")
	}
	allowed, err := allowedLifecycleFields(lifecycle.GetType())
	if err != nil {
		return err
	}
	if inactive := presentLifecycleFields(lifecycle) &^ allowed; inactive != 0 {
		return fmt.Errorf("lifecycle type %d has inactive fields 0x%x", lifecycle.GetType(), inactive)
	}
	return nil
}

// allowedLifecycleFields returns the complete field set for one lifecycle variant.
func allowedLifecycleFields(lifecycleType uiv1.LifecycleType) (lifecycleFields, error) {
	base := lifecycleFieldType | lifecycleFieldRunID
	switch lifecycleType {
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START:
		return base, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
		return base | lifecycleFieldModelResponse, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START:
		return base | lifecycleFieldToolCallID | lifecycleFieldToolName |
			lifecycleFieldText | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE:
		return base | lifecycleFieldToolCallID | lifecycleFieldToolName | lifecycleFieldText |
			lifecycleFieldProgressChannel | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END:
		return base | lifecycleFieldToolCallID | lifecycleFieldToolName | lifecycleFieldText |
			lifecycleFieldIsError | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
		return base | lifecycleFieldToolCallID | lifecycleFieldToolName | lifecycleFieldText |
			lifecycleFieldIsError | lifecycleFieldErrorMessage | lifecycleFieldContents, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END:
		return base | lifecycleFieldText | lifecycleFieldIsError |
			lifecycleFieldOutcome | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED:
		return base | lifecycleFieldIsError | lifecycleFieldOutcome | lifecycleFieldErrorMessage, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED:
		return base | lifecycleFieldAvailability, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END:
		return base | lifecycleFieldModelContent, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA:
		return base | lifecycleFieldToolCallPreview, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END:
		return base | lifecycleFieldFinalToolCall, nil
	case uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED:
		return 0, errors.New("lifecycle type is unspecified")
	default:
		return 0, fmt.Errorf("unknown lifecycle type %d", lifecycleType)
	}
}

// presentLifecycleFields records Protobuf presence without collapsing valid scalar zero values.
func presentLifecycleFields(lifecycle *uiv1.LifecycleEvent) lifecycleFields {
	fields := lifecycleFieldType
	if lifecycle.HasRunId() {
		fields |= lifecycleFieldRunID
	}
	if lifecycle.HasText() {
		fields |= lifecycleFieldText
	}
	if lifecycle.HasToolCallId() {
		fields |= lifecycleFieldToolCallID
	}
	if lifecycle.HasToolName() {
		fields |= lifecycleFieldToolName
	}
	if lifecycle.HasProgressChannel() {
		fields |= lifecycleFieldProgressChannel
	}
	if lifecycle.HasIsError() {
		fields |= lifecycleFieldIsError
	}
	if lifecycle.HasOutcome() {
		fields |= lifecycleFieldOutcome
	}
	if lifecycle.HasErrorMessage() {
		fields |= lifecycleFieldErrorMessage
	}
	if lifecycle.HasAvailability() {
		fields |= lifecycleFieldAvailability
	}
	if lifecycle.HasModelContent() {
		fields |= lifecycleFieldModelContent
	}
	if lifecycle.HasModelResponse() {
		fields |= lifecycleFieldModelResponse
	}
	if lifecycle.HasToolCallPreview() {
		fields |= lifecycleFieldToolCallPreview
	}
	if lifecycle.HasFinalToolCall() {
		fields |= lifecycleFieldFinalToolCall
	}
	if len(lifecycle.GetToolResultContents()) != 0 {
		fields |= lifecycleFieldContents
	}
	return fields
}

// mapModelLifecycle preserves optional streaming and terminal model payloads.
func mapModelLifecycle(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if lifecycle.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END {
		response := lifecycle.GetModelResponse()
		event.Kind = presentationdomain.EventModelEnd
		if response == nil {
			return errors.New("model response is missing")
		}
		content, err := mapModelResponseContent(response.GetContent())
		if err != nil {
			return err
		}
		event.ModelResponseContent = content
		if response.HasErrorMessage() {
			event.ErrorText = mo.Some(response.GetErrorMessage())
		}
		if response.HasOutcome() {
			event.Status = mo.Some(response.GetOutcome())
		}
		event.Failure = mo.Some(response.HasErrorMessage() && response.GetErrorMessage() != "")
		return nil
	}
	content := lifecycle.GetModelContent()
	if content == nil {
		return errors.New("model content is missing")
	}
	if !content.HasType() {
		return errors.New("model content type is missing")
	}
	if !content.HasPosition() {
		return errors.New("model content position is missing")
	}
	if !content.HasKind() {
		return errors.New("model content kind is missing")
	}
	if err := validateModelContentText(lifecycle.GetType(), content); err != nil {
		return err
	}
	kind, err := mapModelContentDiscriminators(lifecycle.GetType(), content.GetType(), content.GetKind())
	if err != nil {
		return err
	}
	event.Kind = presentationdomain.EventModelDelta
	event.Position = mo.Some(int(content.GetPosition()))
	event.ModelContentKind = mo.Some(kind)
	if content.HasText() {
		event.Text = mo.Some(content.GetText())
	}
	return nil
}

// validateModelContentText enforces the nested text field selected by the lifecycle type.
func validateModelContentText(lifecycleType uiv1.LifecycleType, content *uiv1.ModelContent) error {
	if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA {
		if !content.HasText() {
			return errors.New("model content text is missing")
		}
		return nil
	}
	if content.HasText() {
		return errors.New("model content text must be absent")
	}
	return nil
}

// mapToolCallLifecycle validates preview and final call payloads before projection.
func mapToolCallLifecycle(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if lifecycle.GetType() != uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END {
		preview := lifecycle.GetToolCallPreview()
		if preview == nil {
			return errors.New("tool call preview is missing")
		}
		mapped, err := mapToolCallPreview(preview)
		if err != nil {
			return err
		}
		event.Kind = presentationdomain.EventToolCallPreview
		event.ToolCall = mo.Some(mapped)
		return nil
	}
	call := lifecycle.GetFinalToolCall()
	if call == nil || call.GetArguments() == nil {
		return errors.New("final tool call is missing")
	}
	if !call.HasCallId() || !call.HasName() || !call.HasPosition() {
		return errors.New("final tool call scalar is missing")
	}
	event.Kind = presentationdomain.EventToolCallFinal
	event.ToolCall = mo.Some(presentationdomain.ToolCallState{
		CallID:      call.GetCallId(),
		Name:        call.GetName(),
		Position:    int(call.GetPosition()),
		Provisional: false,
		Fields:      nil,
		Arguments:   call.GetArguments().AsMap(),
	})
	return nil
}

// mapToolLifecycle projects execution updates and terminal result payloads.
func mapToolLifecycle(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	lifecycleType := lifecycle.GetType()
	var err error
	switch int(lifecycleType) {
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START):
		err = mapToolStarted(event, lifecycle)
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE):
		err = mapToolProgress(event, lifecycle)
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END):
		err = mapToolEnded(event, lifecycle)
	case int(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT):
		err = mapToolResult(event, lifecycle)
	default:
		return fmt.Errorf("lifecycle type %d is not a tool event", lifecycleType)
	}
	if err != nil {
		return err
	}
	if lifecycle.HasToolCallId() {
		event.ToolCallID = mo.Some(lifecycle.GetToolCallId())
	}
	if lifecycle.HasToolName() {
		event.ToolName = mo.Some(lifecycle.GetToolName())
	}
	if lifecycle.HasText() {
		event.Text = mo.Some(lifecycle.GetText())
	}
	if lifecycle.HasErrorMessage() {
		event.ErrorText = mo.Some(lifecycle.GetErrorMessage())
	}
	return nil
}

func mapToolStarted(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasToolCallId() || !lifecycle.HasToolName() {
		return errors.New("started tool identity is missing")
	}
	event.Kind = presentationdomain.EventToolStarted
	event.ToolName = mo.Some(lifecycle.GetToolName())
	event.Status = mo.Some("started")
	return nil
}

func mapToolProgress(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasText() || !lifecycle.HasProgressChannel() {
		return errors.New("tool progress is missing")
	}
	return mapProgress(event, lifecycle.GetProgressChannel())
}

func mapToolEnded(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasToolCallId() || !lifecycle.HasToolName() || !lifecycle.HasIsError() {
		return errors.New("ended tool result is missing")
	}
	event.Kind = presentationdomain.EventToolEnded
	failure := lifecycle.GetIsError() || lifecycle.GetErrorMessage() != ""
	event.Failure = mo.Some(failure)
	if failure {
		event.Status = mo.Some("error")
	} else {
		event.Status = mo.Some("completed")
	}
	return nil
}

func mapToolResult(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasToolCallId() || !lifecycle.HasToolName() || !lifecycle.HasIsError() {
		return errors.New("tool result is missing")
	}
	contents, err := mapContents(lifecycle.GetToolResultContents(), false)
	if err != nil {
		return err
	}
	event.Kind = presentationdomain.EventToolResult
	event.Contents = mo.Some(contents)
	event.Failure = mo.Some(lifecycle.GetIsError() || lifecycle.GetErrorMessage() != "")
	return nil
}

// mapTerminalLifecycle preserves turn and settlement outcome presence.
func mapTerminalLifecycle(event *presentationdomain.Event, lifecycle *uiv1.LifecycleEvent) error {
	if err := validateTerminalLifecyclePresence(lifecycle); err != nil {
		return err
	}
	if lifecycle.GetType() != uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED {
		event.Kind = presentationdomain.EventTurnEnded
		if lifecycle.HasErrorMessage() {
			event.ErrorText = mo.Some(lifecycle.GetErrorMessage())
		}
		event.Failure = mo.Some(lifecycle.GetIsError() || lifecycle.GetErrorMessage() != "")
		return nil
	}
	event.Kind = presentationdomain.EventAgentSettled
	if lifecycle.HasErrorMessage() {
		event.ErrorText = mo.Some(lifecycle.GetErrorMessage())
	}
	if lifecycle.HasOutcome() {
		event.Status = mo.Some(lifecycle.GetOutcome())
	}
	if lifecycle.HasIsError() || lifecycle.HasErrorMessage() {
		event.Failure = mo.Some(lifecycle.GetIsError() || lifecycle.GetErrorMessage() != "")
	}
	if lifecycle.HasErrorMessage() && lifecycle.GetErrorMessage() != "" {
		event.Text = mo.Some(lifecycle.GetErrorMessage())
	} else if lifecycle.HasOutcome() {
		event.Text = mo.Some(lifecycle.GetOutcome())
	}
	return nil
}

func validateTerminalLifecyclePresence(lifecycle *uiv1.LifecycleEvent) error {
	if lifecycle.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END && !lifecycle.HasText() {
		return errors.New("turn summary is missing")
	}
	if lifecycle.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END && !lifecycle.HasOutcome() {
		return errors.New("agent outcome is missing")
	}
	return nil
}

// mapContents rejects malformed blocks before they reach presentation state.
func mapContents(contents []*uiv1.ToolResultContent, allowEmpty bool) ([]presentationdomain.Content, error) {
	if len(contents) == 0 && !allowEmpty {
		return nil, errors.New("tool result contents are empty")
	}
	if contents == nil {
		return nil, nil
	}
	return lo.MapErr(
		contents,
		func(content *uiv1.ToolResultContent, index int) (presentationdomain.Content, error) {
			if content == nil {
				return presentationdomain.Content{}, fmt.Errorf("tool result content %d is missing", index)
			}
			switch content.WhichContent() {
			case uiv1.ToolResultContent_Text_case:
				return presentationdomain.Content{
					Text:      mo.Some(content.GetText()),
					MediaType: mo.None[string](),
					Data:      mo.None[[]byte](),
				}, nil
			case uiv1.ToolResultContent_Image_case:
				image := content.GetImage()
				if image == nil || image.GetMediaType() == "" || !image.HasData() {
					return presentationdomain.Content{}, fmt.Errorf("tool result image %d is invalid", index)
				}
				return presentationdomain.Content{
					MediaType: mo.Some(image.GetMediaType()),
					Data:      mo.Some(bytes.Clone(image.GetData())),
					Text:      mo.None[string](),
				}, nil
			case uiv1.ToolResultContent_Content_not_set_case:
				return presentationdomain.Content{}, fmt.Errorf("tool result content %d is missing", index)
			default:
				return presentationdomain.Content{}, fmt.Errorf("tool result content %d is invalid", index)
			}
		},
	)
}

// mapModelResponseContent rejects malformed finalized blocks before projection.
func mapModelResponseContent(content []*uiv1.ModelResponseContent) ([]presentationdomain.ModelResponseContent, error) {
	result := make([]presentationdomain.ModelResponseContent, 0, len(content))
	for index, item := range content {
		if item == nil {
			return nil, fmt.Errorf("model response content %d is missing", index)
		}
		if item.GetToolCall() != nil {
			// Final tool calls already own dedicated lifecycle lines and must not become empty model text lines.
			continue
		}
		if !item.HasKind() {
			return nil, fmt.Errorf("model response content %d kind is missing", index)
		}
		kind, err := mapModelContentKind(item.GetKind())
		if err != nil {
			return nil, fmt.Errorf("model response content %d: %w", index, err)
		}
		if !item.HasText() {
			return nil, fmt.Errorf("model response content %d text is missing", index)
		}
		result = append(result, presentationdomain.ModelResponseContent{
			Kind: kind,
			Text: mo.Some(item.GetText()),
		})
	}
	return result, nil
}

// mapModelContentDiscriminators validates both nested model-content discriminators.
func mapModelContentDiscriminators(
	lifecycleType uiv1.LifecycleType,
	contentType uiv1.ModelContentType,
	contentKind uiv1.ModelContentKind,
) (presentationdomain.ModelContentKind, error) {
	expectedType, err := expectedModelContentType(lifecycleType)
	if err != nil {
		return presentationdomain.ModelContentUnspecified, err
	}
	if contentType != expectedType {
		return presentationdomain.ModelContentUnspecified, fmt.Errorf(
			"model content type %d does not match lifecycle type %d",
			contentType, lifecycleType,
		)
	}
	return mapModelContentKind(contentKind)
}

// expectedModelContentType maps each model lifecycle boundary to its matching nested type.
func expectedModelContentType(lifecycleType uiv1.LifecycleType) (uiv1.ModelContentType, error) {
	if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START {
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_START, nil
	}
	if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA {
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA, nil
	}
	if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END {
		return uiv1.ModelContentType_MODEL_CONTENT_TYPE_END, nil
	}
	return uiv1.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED, fmt.Errorf(
		"lifecycle type %d does not support model content", lifecycleType,
	)
}

// mapModelContentKind converts public content identity into the TUI presentation contract.
func mapModelContentKind(kind uiv1.ModelContentKind) (presentationdomain.ModelContentKind, error) {
	switch kind {
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT:
		return presentationdomain.ModelContentText, nil
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL:
		return presentationdomain.ModelContentRefusal, nil
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING:
		return presentationdomain.ModelContentReasoning, nil
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED:
		return presentationdomain.ModelContentUnspecified, errors.New("model content kind is unspecified")
	default:
		return presentationdomain.ModelContentUnspecified, fmt.Errorf("model content kind %d is invalid", kind)
	}
}

func mapToolCallPreview(preview *uiv1.ToolCallPreview) (presentationdomain.ToolCallState, error) {
	if !preview.HasCallId() || !preview.HasName() || !preview.HasPosition() || !preview.HasProvisional() {
		return presentationdomain.ToolCallState{}, errors.New("tool call preview scalar is missing")
	}
	fields := make([]presentationdomain.ToolCallField, len(preview.GetFields()))
	for index, field := range preview.GetFields() {
		if field == nil {
			return presentationdomain.ToolCallState{}, fmt.Errorf("tool call preview field %d is nil", index)
		}
		if !field.HasName() {
			return presentationdomain.ToolCallState{}, fmt.Errorf("tool call preview field %d name is missing", index)
		}
		mapped := presentationdomain.ToolCallField{
			Name:   field.GetName(),
			Value:  mo.None[any](),
			Prefix: mo.None[string](),
		}
		switch field.WhichContent() {
		case uiv1.ToolCallPreviewField_Value_case:
			value := field.GetValue()
			if value == nil {
				return presentationdomain.ToolCallState{}, fmt.Errorf("tool call preview field %d value is nil", index)
			}
			mapped.Value = mo.Some(value.AsInterface())
		case uiv1.ToolCallPreviewField_Prefix_case:
			mapped.Prefix = mo.Some(field.GetPrefix())
		case uiv1.ToolCallPreviewField_Content_not_set_case:
			return presentationdomain.ToolCallState{}, fmt.Errorf("tool call preview field %d content is missing", index)
		}
		fields[index] = mapped
	}
	return presentationdomain.ToolCallState{
		CallID:      preview.GetCallId(),
		Name:        preview.GetName(),
		Position:    int(preview.GetPosition()),
		Provisional: preview.GetProvisional(),
		Fields:      fields,
		Arguments:   nil,
	}, nil
}

// mapProgress validates the closed progress-channel enum and assigns its output kind.
func mapProgress(event *presentationdomain.Event, channel uiv1.ProgressChannel) error {
	switch channel {
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_STATUS:
		event.Kind = presentationdomain.EventToolProgress
		event.Status = mo.Some("progress")
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT:
		event.Kind = presentationdomain.EventToolOutput
		event.Stream = mo.Some(presentationdomain.OutputStdout)
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR:
		event.Kind = presentationdomain.EventToolOutput
		event.Stream = mo.Some(presentationdomain.OutputStderr)
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_UNSPECIFIED:
		return errors.New("tool progress channel is unspecified")
	default:
		return fmt.Errorf("unknown tool progress channel %d", channel)
	}
	return nil
}

// mapAvailability rejects unspecified or unknown Host availability values.
func mapAvailability(availability uiv1.Availability) (presentationdomain.Availability, error) {
	switch availability {
	case uiv1.Availability_AVAILABILITY_CHECKING_AUTHENTICATION:
		return presentationdomain.AvailabilityChecking, nil
	case uiv1.Availability_AVAILABILITY_AUTHENTICATING:
		return presentationdomain.AvailabilityAuthenticating, nil
	case uiv1.Availability_AVAILABILITY_AUTHENTICATION_FAILED:
		return presentationdomain.AvailabilityAuthenticationFailed, nil
	case uiv1.Availability_AVAILABILITY_IDLE:
		return presentationdomain.AvailabilityIdle, nil
	case uiv1.Availability_AVAILABILITY_RUNNING:
		return presentationdomain.AvailabilityRunning, nil
	case uiv1.Availability_AVAILABILITY_UNSPECIFIED:
		return presentationdomain.AvailabilityUnspecified, errors.New("availability is unspecified")
	default:
		return presentationdomain.AvailabilityUnspecified, fmt.Errorf("unknown availability %d", availability)
	}
}

// mapCommand validates and projects one presentation command onto the public stream.
func mapCommand(command presentationdomain.Command) (*uiv1.OpenResponse, error) {
	if response, handled, err := mapSessionCommand(command); handled {
		return response, err
	}
	switch command.Kind {
	case presentationdomain.CommandSubmit:
		text, ok := command.Text.Get()
		if !ok {
			return nil, errors.New("UI submit text is missing")
		}
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active Submit field.
		return uiv1.OpenResponse_builder{
			Submit: uiv1.SubmitCommand_builder{
				Text: new(text),
			}.Build(),
		}.Build(), nil
	case presentationdomain.CommandStop:
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active Stop field.
		return uiv1.OpenResponse_builder{
			Stop: &uiv1.StopCommand{},
		}.Build(), nil
	case presentationdomain.CommandRetryAuthentication:
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active RetryAuthentication field.
		return uiv1.OpenResponse_builder{
			RetryAuthentication: &uiv1.RetryAuthenticationCommand{},
		}.Build(), nil
	case presentationdomain.CommandQuit:
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active Quit field.
		return uiv1.OpenResponse_builder{
			Quit: &uiv1.QuitCommand{},
		}.Build(), nil
	case presentationdomain.CommandSelectModel:
		providerID, providerOK := command.ProviderID.Get()
		modelID, modelOK := command.ModelID.Get()
		if !providerOK || !modelOK {
			return nil, errors.New("UI model selection is missing")
		}
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active SelectModel field.
		return uiv1.OpenResponse_builder{
			SelectModel: uiv1.SelectModelCommand_builder{
				ProviderId: new(providerID),
				ModelId:    new(modelID),
			}.Build(),
		}.Build(), nil
	case presentationdomain.CommandSelectReasoningChoice:
		reasoningChoice, ok := command.ReasoningChoice.Get()
		if !ok {
			return nil, errors.New("UI reasoning choice is missing")
		}
		level := mapReasoningChoiceToProto(reasoningChoice)
		if level == uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED {
			return nil, errors.New("UI reasoning choice is unspecified")
		}
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active SelectReasoningChoice field.
		return uiv1.OpenResponse_builder{
			SelectReasoningChoice: uiv1.SelectReasoningChoiceCommand_builder{
				Choice: new(level),
			}.Build(),
		}.Build(), nil
	case presentationdomain.CommandCreateSession, presentationdomain.CommandListSessions,
		presentationdomain.CommandResumeSession, presentationdomain.CommandSetSessionName,
		presentationdomain.CommandGetSessionInfo:
		return nil, errors.New("UI session command was not mapped")
	case presentationdomain.CommandUnspecified:
		return nil, errors.New("UI command is unspecified")
	default:
		return nil, fmt.Errorf("unknown UI command %d", command.Kind)
	}
}

// mapSessionCommand preserves lifecycle argument presence in the protobuf oneof.
func mapSessionCommand(command presentationdomain.Command) (*uiv1.OpenResponse, bool, error) {
	switch command.Kind {
	case presentationdomain.CommandCreateSession:
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active CreateSession field.
		return uiv1.OpenResponse_builder{CreateSession: &uiv1.CreateSessionCommand{}}.Build(), true, nil
	case presentationdomain.CommandListSessions:
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active ListSessions field.
		return uiv1.OpenResponse_builder{ListSessions: &uiv1.ListSessionsCommand{}}.Build(), true, nil
	case presentationdomain.CommandGetSessionInfo:
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active GetSessionInfo field.
		return uiv1.OpenResponse_builder{GetSessionInfo: &uiv1.GetSessionInfoCommand{}}.Build(), true, nil
	case presentationdomain.CommandResumeSession:
		id, present := command.SessionID.Get()
		if !present || id == "" {
			return nil, true, errors.New("UI session ID is missing")
		}
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active ResumeSession field.
		response := uiv1.OpenResponse_builder{
			ResumeSession: uiv1.ResumeSessionCommand_builder{SessionId: new(id)}.Build(),
		}.Build()
		return response, true, nil
	case presentationdomain.CommandSetSessionName:
		name, present := command.SessionName.Get()
		if !present {
			return nil, true, errors.New("UI session name is missing")
		}
		//nolint:exhaustruct // uiv1.OpenResponse_builder sets only the active SetSessionName field.
		response := uiv1.OpenResponse_builder{
			SetSessionName: uiv1.SetSessionNameCommand_builder{Name: new(name)}.Build(),
		}.Build()
		return response, true, nil
	case presentationdomain.CommandUnspecified, presentationdomain.CommandSubmit,
		presentationdomain.CommandStop, presentationdomain.CommandRetryAuthentication,
		presentationdomain.CommandQuit, presentationdomain.CommandSelectModel,
		presentationdomain.CommandSelectReasoningChoice:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// mapReasoningChoiceToProto converts one validated presentation reasoning choice.
func mapReasoningChoiceToProto(level presentationdomain.ReasoningChoice) uiv1.ReasoningChoice {
	switch level {
	case presentationdomain.ReasoningChoiceOff:
		return uiv1.ReasoningChoice_REASONING_CHOICE_OFF
	case presentationdomain.ReasoningChoiceOn:
		return uiv1.ReasoningChoice_REASONING_CHOICE_ON
	case presentationdomain.ReasoningChoiceMinimal:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL
	case presentationdomain.ReasoningChoiceLow:
		return uiv1.ReasoningChoice_REASONING_CHOICE_LOW
	case presentationdomain.ReasoningChoiceMedium:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM
	case presentationdomain.ReasoningChoiceHigh:
		return uiv1.ReasoningChoice_REASONING_CHOICE_HIGH
	case presentationdomain.ReasoningChoiceXHigh:
		return uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH
	case presentationdomain.ReasoningChoiceMax:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MAX
	case presentationdomain.ReasoningChoiceUnspecified:
		return uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED
	default:
		return uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED
	}
}
