// Package runtime connects Glyph Host to one Extension Contract v1 process.
package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"sync/atomic"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// Runtime owns one extension process connection.
type Runtime struct {
	// context carries runtime values into asynchronous cancellation and shutdown diagnostics.
	context context.Context
	// client owns the extension process connection.
	client *extensionsdk.Client
	// connection owns the asynchronous operation stream.
	connection *extensionsdk.Connection
	// nextOperationID allocates extension-local Host operation identifiers.
	nextOperationID atomic.Uint64
}

var _ extensionruntime.ExtensionRuntime = (*Runtime)(nil)

// Start connects to one extension process command.
func Start(ctx context.Context, command *exec.Cmd) (*Runtime, error) {
	client, err := extensionsdk.Connect(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start extension runtime: %w", err)
	}
	connection, err := client.Open(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("start extension operation stream: %w", err)
	}
	return &Runtime{
		context:         context.WithoutCancel(ctx),
		client:          client,
		connection:      connection,
		nextOperationID: atomic.Uint64{},
	}, nil
}

// Register invokes registration and maps the raw protocol payload.
func (r *Runtime) Register(ctx context.Context) (extensionruntime.Registration, error) {
	request := new(extensionpb.HostRequest)
	request.SetRegister(new(extensionpb.RegisterRequest))
	started, err := r.connection.Start(ctx, r.operationID(), request)
	if err != nil {
		_ = r.Close()
		return extensionruntime.Registration{}, fmt.Errorf("start extension registration: %w", err)
	}
	completed, err := started.Wait(ctx, nil)
	if err != nil {
		_ = r.Close()
		return extensionruntime.Registration{}, fmt.Errorf("register extension: %w", err)
	}
	registration, err := mapRegistration(completed.GetRegister())
	if err != nil {
		_ = r.Close()
		return extensionruntime.Registration{}, fmt.Errorf("validate extension registration: %w", err)
	}
	return registration, nil
}

// Execute waits synchronously for one tool operation on the shared Extension connection.
func (r *Runtime) Execute(
	ctx context.Context,
	toolName string,
	argumentsJSON []byte,
	handleProgress tool.ProgressHandler,
	binding extension.Context,
) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return tool.Result{}, fmt.Errorf("execute extension tool %q: %w", toolName, err)
	}

	request := new(extensionpb.HostRequest)
	request.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new(toolName), ArgumentsJson: argumentsJSON, Context: mapContext(binding),
	}.Build())
	operationID := r.operationID()
	slog.DebugContext(ctx, "invoke extension tool", "operation_id", operationID, "extension_id", binding.ExtensionID,
		"runtime_instance_id", binding.RuntimeInstanceID, "session_id", binding.SessionID)
	started, err := r.connection.Start(ctx, operationID, request)
	if err != nil {
		return tool.Result{}, r.executionError(ctx, toolName, err)
	}
	var progressDeliveryErr error
	completed, err := started.Wait(ctx, func(progress *extensionpb.ToolProgress) error {
		mapped, mapErr := mapProgress(progress)
		if mapErr != nil {
			return mapErr
		}
		progressDeliveryErr = handleProgress(mapped)
		return progressDeliveryErr
	})
	if err != nil {
		var cancellationErr error
		connectionFailed := isConnectionFailure(err)
		if shouldCancelFailedExecution(ctx.Err(), progressDeliveryErr, err) {
			cancellationErr = r.cancelOperation(context.WithoutCancel(ctx), operationID)
		}
		if connectionFailed || isConnectionFailure(cancellationErr) {
			_ = r.Close()
		}
		var primaryErr error
		switch {
		case ctx.Err() != nil:
			primaryErr = fmt.Errorf("execute extension tool %q: %w", toolName, ctx.Err())
		case progressDeliveryErr != nil:
			primaryErr = fmt.Errorf("deliver extension progress: %w", progressDeliveryErr)
		default:
			primaryErr = r.executionError(ctx, toolName, err)
		}
		return tool.Result{}, errors.Join(primaryErr, cancellationErr)
	}
	result := completed.GetTool()
	if result == nil {
		return tool.Result{}, r.protocolViolation(errors.New("tool completion payload is missing"))
	}
	contents, err := mapResultContents(result.GetContents())
	if err != nil {
		return tool.Result{}, r.protocolViolation(err)
	}
	return tool.Result{Contents: contents, IsError: result.GetIsError()}, nil
}

// Done closes when the extension process terminates.
func (r *Runtime) Done() <-chan struct{} { return r.client.Done() }

// Close joins the stream and process and returns retained completion failures on every call.
// Early operation cleanup leaves these failures available for final Host collection.
func (r *Runtime) Close() error {
	// The first stop cause belongs to operation/runtime reporting, not resource cleanup.
	if diagnostic := connectionDiagnostic(r.connection.Close()); diagnostic != nil {
		slog.ErrorContext(r.context, "Close Extension operation stream",
			slog.String("peer_kind", "extension"),
			slog.Any("error", diagnostic),
		)
	}
	r.client.Close()
	return r.connection.CompletionFailures()
}

// operationID allocates one connection-local operation identifier.
func (r *Runtime) operationID() string {
	return "host-extension-" + strconv.FormatUint(r.nextOperationID.Add(1), 10)
}

// cancelOperation starts cancellation and joins the target through the cancellation terminal result.
func (r *Runtime) cancelOperation(ctx context.Context, targetID string) error {
	cancellation, err := r.connection.Cancel(ctx, r.operationID(), targetID)
	if err != nil {
		return fmt.Errorf("start cancellation for extension operation %q: %w", targetID, err)
	}
	if _, err = cancellation.Wait(ctx); err != nil {
		return fmt.Errorf("wait for cancellation of extension operation %q: %w", targetID, err)
	}
	return nil
}

// shouldCancelFailedExecution reports whether usable connection work can still require cancellation.
func shouldCancelFailedExecution(ctxErr, progressDeliveryErr, operationErr error) bool {
	if isConnectionFailure(operationErr) {
		return false
	}
	return ctxErr != nil || progressDeliveryErr != nil || !isExtensionTerminalError(operationErr)
}

// isExtensionTerminalError reports an operation terminal outcome that leaves the stream usable.
func isExtensionTerminalError(err error) bool {
	var rejection *extensionsdk.RejectionError
	var failure *extensionsdk.FailureError
	var canceled *extensionsdk.CanceledError
	return errors.As(err, &rejection) || errors.As(err, &failure) || errors.As(err, &canceled)
}

// isConnectionFailure reports a gRPC connection status independently from caller cancellation.
func isConnectionFailure(err error) bool {
	if err == nil || isExtensionTerminalError(err) {
		return false
	}
	_, hasStatus := status.FromError(err)
	return hasStatus
}

// executionError preserves cancellation while treating other stream failures as process unavailability.
func (r *Runtime) executionError(ctx context.Context, toolName string, err error) error {
	if isExtensionTerminalError(err) {
		return fmt.Errorf("execute extension tool %q: %w", toolName, err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("execute extension tool %q: %w", toolName, ctxErr)
	}
	if status.Code(err) == codes.Canceled {
		return fmt.Errorf("execute extension tool %q: %w", toolName, context.Canceled)
	}
	if status.Code(err) == codes.FailedPrecondition {
		return toolExecutionProtocolViolation(r, toolName, err)
	}
	_ = r.Close()
	return fmt.Errorf(
		"%w: execute extension tool %q: %w",
		extensionruntime.ErrExtensionUnavailable,
		toolName,
		err,
	)
}

// toolExecutionProtocolViolation stops the invalid stream and preserves the operation context.
func toolExecutionProtocolViolation(r *Runtime, toolName string, err error) error {
	_ = r.Close()
	return fmt.Errorf(
		"%w: execute extension tool %q: extension protocol violation: %w",
		extensionruntime.ErrExtensionUnavailable,
		toolName,
		err,
	)
}

// protocolViolation fails and joins the shared connection after Host payload validation rejects peer data.
func (r *Runtime) protocolViolation(cause error) error {
	connectionErr := r.connection.Fail(cause)
	r.client.Close()
	return fmt.Errorf(
		"%w: extension protocol violation: %w",
		extensionruntime.ErrExtensionUnavailable,
		connectionErr,
	)
}

// mapRegistration decodes process declarations without supplying trusted identity or validating capabilities.
func mapRegistration(response *extensionpb.RegisterResponse) (extensionruntime.Registration, error) {
	if response == nil {
		return extensionruntime.Registration{}, errors.New("registration response is missing")
	}
	tools := make([]mo.Option[extensionruntime.ToolDeclaration], len(response.GetTools()))
	for index, descriptor := range response.GetTools() {
		tools[index] = mapToolDescriptor(descriptor)
	}
	handlers := make([]mo.Option[extensionruntime.HandlerDeclaration], len(response.GetHandlers()))
	for index, handler := range response.GetHandlers() {
		handlers[index] = mo.None[extensionruntime.HandlerDeclaration]()
		if handler != nil {
			handlers[index] = mo.Some(
				extensionruntime.HandlerDeclaration{ID: handler.GetId(), Kind: int32(handler.GetKind())},
			)
		}
	}
	return extensionruntime.Registration{Tools: tools, Handlers: handlers}, nil
}

// mapToolDescriptor decodes one optional tool declaration without capability acceptance.
func mapToolDescriptor(descriptor *extensionpb.ToolDescriptor) mo.Option[extensionruntime.ToolDeclaration] {
	if descriptor == nil {
		return mo.None[extensionruntime.ToolDeclaration]()
	}
	return mo.Some(extensionruntime.ToolDeclaration{
		Name:        descriptor.GetName(),
		Description: descriptor.GetDescription(),
		InputSchemaJSON: bytes.Clone(
			descriptor.GetInputSchemaJson(),
		),
		Constraint: mapRawConstrainedSampling(descriptor.GetConstrainedSampling()),
	})
}

// mapRawConstrainedSampling retains selected configuration presence and raw values.
func mapRawConstrainedSampling(
	constraint *extensionpb.ConstrainedSampling,
) mo.Option[extensionruntime.ConstraintDeclaration] {
	if constraint == nil {
		return mo.None[extensionruntime.ConstraintDeclaration]()
	}
	raw := extensionruntime.ConstraintDeclaration{
		Kind:       extensionruntime.ConstraintMissing,
		Present:    false,
		Strictness: 0,
		Lark:       mo.None[string](),
		Regex:      mo.None[string](),
	}
	switch constraint.WhichConfig() {
	case extensionpb.ConstrainedSampling_JsonSchema_case:
		raw.Kind = extensionruntime.ConstraintJSONSchema
		if config := constraint.GetJsonSchema(); config != nil {
			raw.Present = true
			raw.Strictness = int32(config.GetStrictness())
		}
	case extensionpb.ConstrainedSampling_Grammar_case:
		raw.Kind = extensionruntime.ConstraintGrammar
		if config := constraint.GetGrammar(); config != nil {
			raw.Present = true
			if config.HasLark() {
				raw.Lark = mo.Some(config.GetLark())
			}
			if config.HasRegex() {
				raw.Regex = mo.Some(config.GetRegex())
			}
		}
	case extensionpb.ConstrainedSampling_Config_not_set_case:
	default:
		raw.Kind = extensionruntime.ConstraintInvalid
	}
	return mo.Some(raw)
}

// mapResultContents converts ordered extension result blocks into domain values.
func mapResultContents(contents []*extensionpb.ToolResultContent) ([]tool.ResultContent, error) {
	if len(contents) == 0 {
		return nil, errors.New("result contents are empty")
	}
	return lo.MapErr(contents, func(content *extensionpb.ToolResultContent, index int) (tool.ResultContent, error) {
		if content == nil {
			return tool.ResultContent{}, fmt.Errorf("result content %d is missing", index)
		}
		switch content.WhichContent() {
		case extensionpb.ToolResultContent_Text_case:
			return tool.ResultContent{
				Kind: tool.ResultContentText, Text: mo.Some(content.GetText()), Image: mo.None[tool.ResultImage](),
			}, nil
		case extensionpb.ToolResultContent_Image_case:
			image := content.GetImage()
			if image == nil || image.GetMediaType() == "" || len(image.GetData()) == 0 {
				return tool.ResultContent{}, fmt.Errorf("result image %d is invalid", index)
			}
			return tool.ResultContent{
				Kind: tool.ResultContentImage, Text: mo.None[string](), Image: mo.Some(tool.ResultImage{
					MediaType: image.GetMediaType(), Data: bytes.Clone(image.GetData()),
				}),
			}, nil
		case extensionpb.ToolResultContent_Content_not_set_case:
			return tool.ResultContent{}, fmt.Errorf("result content %d is missing", index)
		default:
			return tool.ResultContent{}, fmt.Errorf("result content %d is invalid", index)
		}
	})
}

// mapProgress maps the closed public enum into a Host infrastructure value.
func mapProgress(progress *extensionpb.ToolProgress) (tool.Progress, error) {
	if progress == nil {
		return tool.Progress{}, errors.New("progress payload is missing")
	}
	var channel tool.ProgressChannel
	switch progress.GetChannel() {
	case extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS:
		channel = tool.ProgressChannelStatus
	case extensionpb.ProgressChannel_PROGRESS_CHANNEL_STDOUT:
		channel = tool.ProgressChannelStdout
	case extensionpb.ProgressChannel_PROGRESS_CHANNEL_STDERR:
		channel = tool.ProgressChannelStderr
	case extensionpb.ProgressChannel_PROGRESS_CHANNEL_UNSPECIFIED:
		return tool.Progress{}, errors.New("progress channel is unspecified")
	default:
		return tool.Progress{}, fmt.Errorf("progress channel %q is invalid", progress.GetChannel().String())
	}
	return tool.Progress{Channel: channel, Content: progress.GetContent()}, nil
}
