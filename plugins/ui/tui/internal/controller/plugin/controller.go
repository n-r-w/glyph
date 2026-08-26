// Package plugin maps the public UI contract to the standard terminal presentation.
package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
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
	textKind := presentationdomain.EventUnspecified
	text := ""
	if authorization := request.GetAuthorization(); authorization != nil {
		if !authorization.HasUrl() {
			return presentationdomain.Event{}, errors.New("authorization URL is required")
		}
		textKind = presentationdomain.EventAuthorization
		text = authorization.GetUrl()
	} else if information := request.GetInformation(); information != nil {
		if !information.HasText() {
			return presentationdomain.Event{}, errors.New("information text is required")
		}
		textKind = presentationdomain.EventInformation
		text = information.GetText()
	}
	if textKind != presentationdomain.EventUnspecified {
		return presentationdomain.Event{
			Kind:                 textKind,
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
			ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		}, nil
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
			ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		}, nil
	}
	if changed := request.GetModelSelectionChanged(); changed != nil {
		selection, err := mapModelSelection(changed.GetSelection())
		if err != nil {
			return presentationdomain.Event{}, err
		}
		return presentationdomain.Event{
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
			ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.Some(selection),
		}, nil
	}
	return presentationdomain.Event{}, errors.New("frame content is missing")
}

// mapInitialization preserves startup severity, identities, paths, tools, and availability.
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
	event := presentationdomain.Event{
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               models,
		ModelSelection:       mo.Some(selection),
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
			Kind:               kind,
			Text:               mo.Some(content.GetText()),
			ToolName:           mo.None[string](),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
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
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
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

func validateLifecycleEnvelope(lifecycle *uiv1.LifecycleEvent) error {
	if !lifecycle.HasType() {
		return errors.New("lifecycle type is missing")
	}
	if lifecycle.GetType() != uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED && !lifecycle.HasRunId() {
		return errors.New("lifecycle run ID is missing")
	}
	return nil
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
	if lifecycle.GetType() == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA && !content.HasText() {
		return errors.New("model content text is missing")
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
	contents, err := mapToolResultContents(lifecycle.GetToolResultContents())
	if err != nil {
		return err
	}
	event.Kind = presentationdomain.EventToolResult
	event.ToolResultContents = mo.Some(contents)
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

// mapToolResultContents rejects malformed blocks before they reach presentation state.
func mapToolResultContents(contents []*uiv1.ToolResultContent) ([]presentationdomain.ToolResultContent, error) {
	if len(contents) == 0 {
		return nil, errors.New("tool result contents are empty")
	}
	return lo.MapErr(
		contents,
		func(content *uiv1.ToolResultContent, index int) (presentationdomain.ToolResultContent, error) {
			if content == nil {
				return presentationdomain.ToolResultContent{}, fmt.Errorf("tool result content %d is missing", index)
			}
			switch content.WhichContent() {
			case uiv1.ToolResultContent_Text_case:
				return presentationdomain.ToolResultContent{
					Text:      mo.Some(content.GetText()),
					MediaType: mo.None[string](),
					Data:      mo.None[[]byte](),
				}, nil
			case uiv1.ToolResultContent_Image_case:
				image := content.GetImage()
				if image == nil || image.GetMediaType() == "" || len(image.GetData()) == 0 {
					return presentationdomain.ToolResultContent{}, fmt.Errorf("tool result image %d is invalid", index)
				}
				return presentationdomain.ToolResultContent{
					MediaType: mo.Some(image.GetMediaType()),
					Data:      mo.Some(bytes.Clone(image.GetData())),
					Text:      mo.None[string](),
				}, nil
			case uiv1.ToolResultContent_Content_not_set_case:
				return presentationdomain.ToolResultContent{}, fmt.Errorf("tool result content %d is missing", index)
			default:
				return presentationdomain.ToolResultContent{}, fmt.Errorf("tool result content %d is invalid", index)
			}
		},
	)
}

// mapModelResponseContent rejects malformed finalized blocks before projection.
func mapModelResponseContent(content []*uiv1.ModelResponseContent) ([]presentationdomain.ModelResponseContent, error) {
	return lo.MapErr(
		content,
		func(item *uiv1.ModelResponseContent, index int) (presentationdomain.ModelResponseContent, error) {
			if item == nil {
				return presentationdomain.ModelResponseContent{}, fmt.Errorf("model response content %d is missing", index)
			}
			if !item.HasKind() {
				return presentationdomain.ModelResponseContent{}, fmt.Errorf("model response content %d kind is missing", index)
			}
			kind, err := mapModelContentKind(item.GetKind())
			if err != nil {
				return presentationdomain.ModelResponseContent{}, fmt.Errorf("model response content %d: %w", index, err)
			}
			if !item.HasText() {
				return presentationdomain.ModelResponseContent{}, fmt.Errorf("model response content %d text is missing", index)
			}
			return presentationdomain.ModelResponseContent{
				Kind: kind,
				Text: mo.Some(item.GetText()),
			}, nil
		},
	)
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
	case presentationdomain.CommandUnspecified:
		return nil, errors.New("UI command is unspecified")
	default:
		return nil, fmt.Errorf("unknown UI command %d", command.Kind)
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
