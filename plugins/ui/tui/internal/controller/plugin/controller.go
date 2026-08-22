// Package plugin maps the public UI contract to the standard terminal presentation.
//
//nolint:exhaustruct // Contract frames and presentation events set only fields used by their active kind.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

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
	return &Controller{terminal: terminal, programs: programs}
}

// GetCapabilities returns immutable capabilities without opening the terminal.
func (*Controller) GetCapabilities(
	context.Context,
	*uiv1.GetCapabilitiesRequest,
) (*uiv1.GetCapabilitiesResponse, error) {
	return &uiv1.GetCapabilitiesResponse{ControlsTerminal: true}, nil
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
		if lifecycle := request.GetLifecycle(); lifecycle != nil &&
			lifecycle.GetModelContent().GetKind() == uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING {
			continue
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
	if authorization := request.GetAuthorization(); authorization != nil {
		return presentationdomain.Event{
			Kind: presentationdomain.EventAuthorization,
			Text: authorization.GetUrl(),
		}, nil
	}
	if information := request.GetInformation(); information != nil {
		return presentationdomain.Event{
			Kind: presentationdomain.EventInformation,
			Text: information.GetText(),
		}, nil
	}
	if safeError := request.GetError(); safeError != nil {
		event := presentationdomain.Event{Kind: presentationdomain.EventError, Text: safeError.GetText()}
		if safeError.GetRetryAuthentication() {
			event.Availability = presentationdomain.AvailabilityAuthenticationFailed
		}
		return event, nil
	}
	return presentationdomain.Event{}, errors.New("frame content is missing")
}

// mapInitialization preserves startup severity, identities, paths, tools, and availability.
func mapInitialization(initialization *uiv1.Initialization) (presentationdomain.Event, error) {
	availability, err := mapAvailability(initialization.GetAvailability())
	if err != nil {
		return presentationdomain.Event{}, err
	}
	event := presentationdomain.Event{
		Kind:         presentationdomain.EventInitialization,
		Availability: availability,
		Startup:      make([]presentationdomain.Line, 0, len(initialization.GetStartupContent())),
		Extensions:   make([]presentationdomain.Extension, 0, len(initialization.GetExtensions())),
	}
	for _, content := range initialization.GetStartupContent() {
		var kind presentationdomain.LineKind
		switch content.GetSeverity() {
		case uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION:
			kind = presentationdomain.LineInformation
		case uiv1.ContentSeverity_CONTENT_SEVERITY_ERROR:
			kind = presentationdomain.LineError
		case uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING:
			kind = presentationdomain.LineWarning
		case uiv1.ContentSeverity_CONTENT_SEVERITY_UNSPECIFIED:
			return presentationdomain.Event{}, errors.New("startup content severity is unspecified")
		default:
			return presentationdomain.Event{}, fmt.Errorf("unknown startup content severity %d", content.GetSeverity())
		}
		event.Startup = append(event.Startup, presentationdomain.Line{Kind: kind, Text: content.GetText()})
	}
	for _, extension := range initialization.GetExtensions() {
		event.Extensions = append(event.Extensions, presentationdomain.Extension{
			ID: extension.GetPluginId(), Path: extension.GetPath(),
			Tools: append([]string(nil), extension.GetTools()...),
		})
	}
	return event, nil
}

//nolint:gocyclo // The explicit flat switch mirrors the finite lifecycle enum.
func mapLifecycle(lifecycle *uiv1.LifecycleEvent) (presentationdomain.Event, error) {
	event := presentationdomain.Event{
		Position:   0,
		ToolCallID: lifecycle.GetToolCallId(),
		ToolName:   lifecycle.GetToolName(),
		Text:       lifecycle.GetText(),
		ErrorText:  lifecycle.GetErrorMessage(),
		Status:     lifecycle.GetOutcome(),
		Failure:    lifecycle.GetIsError() || lifecycle.GetErrorMessage() != "",
	}

	switch lifecycle.GetType() {
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START:
		event.Kind = presentationdomain.EventTurnStarted
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START:
		event.Kind = presentationdomain.EventModelDelta
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START:
		event.Kind = presentationdomain.EventModelDelta
		event.Position = int(lifecycle.GetModelContent().GetPosition())
		event.ModelContentKind = mapModelContentKind(lifecycle.GetModelContent().GetKind())
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA:
		event.Kind = presentationdomain.EventModelDelta
		event.Position = int(lifecycle.GetModelContent().GetPosition())
		event.ModelContentKind = mapModelContentKind(lifecycle.GetModelContent().GetKind())
		event.Text = lifecycle.GetModelContent().GetText()
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END:
		event.Kind = presentationdomain.EventModelDelta
		event.Position = int(lifecycle.GetModelContent().GetPosition())
		event.ModelContentKind = mapModelContentKind(lifecycle.GetModelContent().GetKind())
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA:
		event.Kind = presentationdomain.EventToolCallPreview
		event.ToolCall = mapToolCallPreview(lifecycle.GetToolCallPreview())
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END:
		event.Kind = presentationdomain.EventToolCallFinal
		call := lifecycle.GetFinalToolCall()
		event.ToolCall = presentationdomain.ToolCallState{
			CallID: call.GetCallId(), Name: call.GetName(), Position: int(call.GetPosition()),
			Provisional: false, Fields: nil, Arguments: call.GetArguments().AsMap(),
		}
	case uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
		event.Kind = presentationdomain.EventModelEnd
		event.ModelResponseContent = mapModelResponseContent(lifecycle.GetModelResponse().GetContent())
		event.ErrorText = lifecycle.GetModelResponse().GetErrorMessage()
		event.Status = lifecycle.GetModelResponse().GetOutcome()
		event.Failure = event.ErrorText != ""
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START:
		event.Kind = presentationdomain.EventToolStarted
		event.Status = "started"
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE:
		if err := mapProgress(&event, lifecycle.GetProgressChannel()); err != nil {
			return presentationdomain.Event{}, err
		}
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END:
		event.Kind = presentationdomain.EventToolEnded
		if event.Failure {
			event.Status = "error"
		} else {
			event.Status = "completed"
		}
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
		event.Kind = presentationdomain.EventToolResult
	case uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END:
		event.Kind = presentationdomain.EventTurnEnded
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED:
		event.Kind = presentationdomain.EventAgentSettled
		if event.ErrorText != "" {
			event.Text = event.ErrorText
		} else {
			event.Text = lifecycle.GetOutcome()
		}
	case uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED:
		availability, err := mapAvailability(lifecycle.GetAvailability())
		if err != nil {
			return presentationdomain.Event{}, err
		}
		event.Kind = presentationdomain.EventAvailability
		event.Availability = availability
	case uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED:
		return presentationdomain.Event{}, errors.New("lifecycle type is unspecified")
	default:
		return presentationdomain.Event{}, fmt.Errorf("unknown lifecycle type %d", lifecycle.GetType())
	}

	return event, nil
}

// mapModelResponseContent keeps finalized visible text and refusal blocks while dropping reasoning.
func mapModelResponseContent(content []*uiv1.ModelResponseContent) []presentationdomain.ModelResponseContent {
	mapped := make([]presentationdomain.ModelResponseContent, 0, len(content))
	for _, item := range content {
		kind := mapModelContentKind(item.GetKind())
		if kind == presentationdomain.ModelContentUnspecified {
			continue
		}
		mapped = append(mapped, presentationdomain.ModelResponseContent{Kind: kind, Text: item.GetText()})
	}
	return mapped
}

// mapModelContentKind converts public content identity into the TUI presentation contract.
func mapModelContentKind(kind uiv1.ModelContentKind) presentationdomain.ModelContentKind {
	switch kind {
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT:
		return presentationdomain.ModelContentText
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL:
		return presentationdomain.ModelContentRefusal
	case uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED,
		uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING:
		return presentationdomain.ModelContentUnspecified
	default:
		return presentationdomain.ModelContentUnspecified
	}
}

func mapToolCallPreview(preview *uiv1.ToolCallPreview) presentationdomain.ToolCallState {
	fields := make([]presentationdomain.ToolCallField, 0, len(preview.GetFields()))
	for _, field := range preview.GetFields() {
		mapped := presentationdomain.ToolCallField{Name: field.GetName(), Value: nil, Prefix: "", Complete: false}
		switch content := field.GetContent().(type) {
		case *uiv1.ToolCallPreviewField_Value:
			mapped.Value = content.Value.AsInterface()
			mapped.Complete = true
		case *uiv1.ToolCallPreviewField_Prefix:
			mapped.Prefix = content.Prefix
		}
		fields = append(fields, mapped)
	}
	return presentationdomain.ToolCallState{
		CallID: preview.GetCallId(), Name: preview.GetName(), Position: int(preview.GetPosition()),
		Provisional: preview.GetProvisional(), Fields: fields, Arguments: nil,
	}
}

// mapProgress validates the closed progress-channel enum and assigns its output kind.
func mapProgress(event *presentationdomain.Event, channel uiv1.ProgressChannel) error {
	switch channel {
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_STATUS:
		event.Kind = presentationdomain.EventToolProgress
		event.Status = "progress"
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT:
		event.Kind = presentationdomain.EventToolOutput
		event.Stream = presentationdomain.OutputStdout
	case uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR:
		event.Kind = presentationdomain.EventToolOutput
		event.Stream = presentationdomain.OutputStderr
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
		return &uiv1.OpenResponse{Content: &uiv1.OpenResponse_Submit{
			Submit: &uiv1.SubmitCommand{Text: command.Text},
		}}, nil
	case presentationdomain.CommandStop:
		return &uiv1.OpenResponse{Content: &uiv1.OpenResponse_Stop{Stop: &uiv1.StopCommand{}}}, nil
	case presentationdomain.CommandRetryAuthentication:
		return &uiv1.OpenResponse{Content: &uiv1.OpenResponse_RetryAuthentication{
			RetryAuthentication: &uiv1.RetryAuthenticationCommand{},
		}}, nil
	case presentationdomain.CommandQuit:
		return &uiv1.OpenResponse{Content: &uiv1.OpenResponse_Quit{Quit: &uiv1.QuitCommand{}}}, nil
	case presentationdomain.CommandUnspecified:
		return nil, errors.New("UI command is unspecified")
	default:
		return nil, fmt.Errorf("unknown UI command %d", command.Kind)
	}
}
