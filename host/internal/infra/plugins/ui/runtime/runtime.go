// Package runtime adapts the public UI SDK to Host UI lifecycle contracts.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// Factory starts UI candidates through the public SDK.
type Factory struct{}

var _ hostui.RuntimeFactory = (*Factory)(nil)

// Runtime owns one connected UI process and its single stream.
type Runtime struct {
	client   *uisdk.Client
	openOnce sync.Once
	channel  hostui.Channel
	openErr  error
}

var _ hostui.Runtime = (*Runtime)(nil)

// channel maps provider-neutral frames and commands to the generated contract.
type channel struct {
	stream uipb.UIService_OpenClient
	mutex  sync.Mutex
}

var _ hostui.Channel = (*channel)(nil)

// NewFactory creates a UI runtime factory.
func NewFactory() *Factory {
	return &Factory{}
}

// Start launches one trusted local UI candidate and validates its fixed capabilities.
func (*Factory) Start(ctx context.Context, candidate domainui.Candidate) (hostui.Runtime, error) {
	//nolint:gosec // The catalog contains trusted local UI plugin executables.
	command := exec.CommandContext(context.WithoutCancel(ctx), candidate.Path)
	client, err := uisdk.Connect(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start UI %q: %w", candidate.ID, err)
	}
	return &Runtime{client: client, openOnce: sync.Once{}, channel: nil, openErr: nil}, nil
}

// Capabilities returns the immutable capabilities retrieved before stream creation.
func (r *Runtime) Capabilities() domainui.Capabilities {
	return domainui.Capabilities{ControlsTerminal: r.client.Capabilities().GetControlsTerminal()}
}

// Open opens and reuses the one persistent UI lifecycle stream.
func (r *Runtime) Open(ctx context.Context) (hostui.Channel, error) {
	r.openOnce.Do(func() {
		stream, err := r.client.Service().Open(ctx)
		if err != nil {
			r.openErr = fmt.Errorf("open UI stream: %w", err)
			return
		}
		r.channel = &channel{stream: stream, mutex: sync.Mutex{}}
	})
	return r.channel, r.openErr
}

// Close stops the selected UI process once.
func (r *Runtime) Close() {
	r.client.Close()
}

// Send writes one Host frame synchronously through the serialized stream path.
func (c *channel) Send(frame domainui.Frame) error {
	mapped, err := mapFrame(frame)
	if err != nil {
		return err
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if sendErr := c.stream.Send(mapped); sendErr != nil {
		return fmt.Errorf("send UI frame: %w", sendErr)
	}
	return nil
}

// Receive blocks until the UI sends one command or the stream terminates.
func (c *channel) Receive() (domainui.Command, error) {
	command, err := c.stream.Recv()
	if err != nil {
		return domainui.Command{}, fmt.Errorf("receive UI command: %w", err)
	}
	return mapCommand(command)
}

// mapFrame converts one provider-neutral frame without exposing internal objects.
func mapFrame(frame domainui.Frame) (*uipb.OpenRequest, error) {
	switch frame.Kind {
	case domainui.FrameInitialization:
		return &uipb.OpenRequest{Content: &uipb.OpenRequest_Initialization{
			Initialization: mapInitialization(frame.Initialization),
		}}, nil
	case domainui.FrameLifecycle:
		return &uipb.OpenRequest{Content: &uipb.OpenRequest_Lifecycle{
			Lifecycle: mapLifecycle(frame.Lifecycle),
		}}, nil
	case domainui.FrameAuthorization:
		return &uipb.OpenRequest{Content: &uipb.OpenRequest_Authorization{
			Authorization: &uipb.AuthorizationRequest{Url: frame.AuthorizationURL},
		}}, nil
	case domainui.FrameInformation:
		return &uipb.OpenRequest{Content: &uipb.OpenRequest_Information{
			Information: &uipb.Information{Text: frame.Text},
		}}, nil
	case domainui.FrameError:
		return &uipb.OpenRequest{Content: &uipb.OpenRequest_Error{
			Error: &uipb.Error{Text: frame.Text, RetryAuthentication: frame.RetryAuthentication},
		}}, nil
	default:
		return nil, errors.New("map UI frame: payload is required")
	}
}

// mapInitialization converts one complete startup state.
func mapInitialization(initialization domainui.Initialization) *uipb.Initialization {
	startup := make([]*uipb.StartupContent, 0, len(initialization.StartupContent))
	for _, content := range initialization.StartupContent {
		startup = append(startup, &uipb.StartupContent{
			Severity: mapSeverity(content.Severity),
			Text:     content.Text,
		})
	}
	extensions := make([]*uipb.ExtensionAvailability, 0, len(initialization.Extensions))
	for _, extension := range initialization.Extensions {
		extensions = append(extensions, &uipb.ExtensionAvailability{
			PluginId: extension.PluginID,
			Tools:    append([]string(nil), extension.Tools...),
			Path:     extension.Path,
		})
	}
	return &uipb.Initialization{
		SelectedUiId:   initialization.SelectedUIID,
		StartupContent: startup,
		Extensions:     extensions,
		Availability:   mapAvailability(initialization.Availability),
	}
}

// mapLifecycle converts one explicit lifecycle payload.
func mapLifecycle(event domainui.Lifecycle) *uipb.LifecycleEvent {
	return &uipb.LifecycleEvent{
		Type:            mapLifecycleType(event.Type),
		RunId:           event.RunID,
		Position:        int32(event.Position), //nolint:gosec // Runnable model output indexes cannot approach int32 limits.
		Text:            event.Text,
		ToolCallId:      event.ToolCallID,
		ToolName:        event.ToolName,
		ProgressChannel: mapProgressChannel(event.ProgressChannel),
		IsError:         event.IsError,
		Outcome:         event.Outcome,
		ErrorMessage:    event.ErrorMessage,
		Availability:    mapAvailability(event.Availability),
	}
}

// mapCommand validates one generated UI command.
func mapCommand(command *uipb.OpenResponse) (domainui.Command, error) {
	switch {
	case command.GetSubmit() != nil:
		return domainui.Command{Kind: domainui.CommandSubmit, Text: command.GetSubmit().GetText()}, nil
	case command.GetStop() != nil:
		return domainui.Command{Kind: domainui.CommandStop, Text: ""}, nil
	case command.GetRetryAuthentication() != nil:
		return domainui.Command{Kind: domainui.CommandRetryAuthentication, Text: ""}, nil
	case command.GetQuit() != nil:
		return domainui.Command{Kind: domainui.CommandQuit, Text: ""}, nil
	default:
		return domainui.Command{}, errors.New("receive UI command: payload is required")
	}
}

// mapSeverity converts startup severity to the public contract.
func mapSeverity(value domainui.ContentSeverity) uipb.ContentSeverity {
	switch value {
	case domainui.ContentSeverityInformation:
		return uipb.ContentSeverity_CONTENT_SEVERITY_INFORMATION
	case domainui.ContentSeverityError:
		return uipb.ContentSeverity_CONTENT_SEVERITY_ERROR
	case domainui.ContentSeverityWarning:
		return uipb.ContentSeverity_CONTENT_SEVERITY_WARNING
	default:
		return uipb.ContentSeverity_CONTENT_SEVERITY_INFORMATION
	}
}

// mapAvailability converts Host availability to the public contract.
func mapAvailability(value domainui.Availability) uipb.Availability {
	return uipb.Availability(value)
}

// mapLifecycleType converts Host lifecycle identity to the public contract.
func mapLifecycleType(value domainui.LifecycleType) uipb.LifecycleType {
	return uipb.LifecycleType(value)
}

// mapProgressChannel converts Host tool progress identity to the public contract.
func mapProgressChannel(value domainui.ProgressChannel) uipb.ProgressChannel {
	return uipb.ProgressChannel(value)
}
