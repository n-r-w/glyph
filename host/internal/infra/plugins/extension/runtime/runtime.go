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

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
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
func (r *Runtime) Register(ctx context.Context) (startup.PendingRegistration, error) {
	request := new(extensionpb.HostRequest)
	request.SetRegister(new(extensionpb.RegisterRequest))
	started, err := r.connection.Start(ctx, r.operationID(), request)
	if err != nil {
		r.Close()
		return startup.PendingRegistration{}, fmt.Errorf("start extension registration: %w", err)
	}
	completed, err := started.Wait(ctx, nil)
	if err != nil {
		r.Close()
		return startup.PendingRegistration{}, fmt.Errorf("register extension: %w", err)
	}
	registration, err := mapRegistration(completed.GetRegister())
	if err != nil {
		r.Close()
		return startup.PendingRegistration{}, fmt.Errorf("validate extension registration: %w", err)
	}
	return registration, nil
}

// Execute waits synchronously for one tool operation on the shared Extension connection.
func (r *Runtime) Execute(
	ctx context.Context,
	toolName string,
	argumentsJSON []byte,
	handleProgress tool.ProgressHandler,
) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return tool.Result{}, fmt.Errorf("execute extension tool %q: %w", toolName, err)
	}

	request := new(extensionpb.HostRequest)
	request.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new(toolName), ArgumentsJson: argumentsJSON,
	}.Build())
	operationID := r.operationID()
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
			r.Close()
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

// Close stops the extension stream and process and waits for cleanup.
func (r *Runtime) Close() {
	if err := r.connection.Close(); err != nil && !isBenignCancellationError(err) {
		slog.ErrorContext(r.context, "Close Extension operation stream",
			slog.String("peer_kind", "extension"),
			slog.Any("error", err),
		)
	}
	r.client.Close()
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

// isBenignCancellationError reports expected cancellation settlement during target or connection shutdown.
func isBenignCancellationError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	if _, ok := errors.AsType[*extensionsdk.CanceledError](err); ok {
		return true
	}
	if rejected, ok := errors.AsType[*extensionsdk.RejectionError](err); ok {
		return rejected.Code() == "TARGET_NOT_ACTIVE"
	}
	return false
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
	r.Close()
	return fmt.Errorf(
		"%w: execute extension tool %q: %w",
		extensionruntime.ErrExtensionUnavailable,
		toolName,
		err,
	)
}

// toolExecutionProtocolViolation stops the invalid stream and preserves the operation context.
func toolExecutionProtocolViolation(r *Runtime, toolName string, err error) error {
	r.Close()
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

// mapRegistration maps raw tool and handler protocol payloads without applying Host capability policy.
func mapRegistration(response *extensionpb.RegisterResponse) (startup.PendingRegistration, error) {
	if response == nil {
		return startup.PendingRegistration{}, errors.New("registration response is missing")
	}
	tools := make([]startup.RawToolDescriptor, 0, len(response.GetTools()))
	for _, descriptor := range response.GetTools() {
		tools = append(tools, mapToolDescriptor(descriptor))
	}
	handlers := make([]startup.RawHandlerDescriptor, 0, len(response.GetHandlers()))
	for _, handler := range response.GetHandlers() {
		if handler == nil {
			handlers = append(
				handlers,
				startup.RawHandlerDescriptor{Present: false, ID: "", Kind: startup.RawHandlerKindUnspecified},
			)
			continue
		}
		handlers = append(
			handlers,
			startup.RawHandlerDescriptor{
				Present: true,
				ID:      handler.GetId(),
				Kind:    mapRawHandlerKind(handler.GetKind()),
			},
		)
	}
	return startup.PendingRegistration{ID: "", Path: "", Tools: tools, Handlers: handlers}, nil
}

// mapRawHandlerKind maps supported public kinds and maps other values to the invalid zero kind.
func mapRawHandlerKind(kind extensionpb.HandlerKind) startup.RawHandlerKind {
	switch kind {
	case extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST:
		return startup.RawHandlerKindSessionBeforeTreeRequest
	case extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_RESULT:
		return startup.RawHandlerKindSessionBeforeTreeResult
	case extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE:
		return startup.RawHandlerKindSessionTree
	case extensionpb.HandlerKind_HANDLER_KIND_UNSPECIFIED:
		return startup.RawHandlerKindUnspecified
	default:
		return startup.RawHandlerKindUnspecified
	}
}

// mapToolDescriptor maps one optional public descriptor without validating tool policy.
func mapToolDescriptor(descriptor *extensionpb.ToolDescriptor) startup.RawToolDescriptor {
	if descriptor == nil {
		return startup.RawToolDescriptor{
			Present:             false,
			Name:                "",
			Description:         "",
			InputSchemaJSON:     nil,
			ConstrainedSampling: mo.None[startup.RawConstrainedSampling](),
		}
	}
	return startup.RawToolDescriptor{
		Present:             true,
		Name:                descriptor.GetName(),
		Description:         descriptor.GetDescription(),
		InputSchemaJSON:     bytes.Clone(descriptor.GetInputSchemaJson()),
		ConstrainedSampling: mapRawConstrainedSampling(descriptor.GetConstrainedSampling()),
	}
}

// mapRawConstrainedSampling preserves the selected protocol configuration and invalid values.
func mapRawConstrainedSampling(constraint *extensionpb.ConstrainedSampling) mo.Option[startup.RawConstrainedSampling] {
	if constraint == nil {
		return mo.None[startup.RawConstrainedSampling]()
	}
	raw := startup.RawConstrainedSampling{
		Kind:                 startup.RawConstrainedSamplingMissing,
		JSONSchemaPresent:    false,
		JSONSchemaStrictness: startup.RawJSONSchemaStrictnessUnspecified,
		Grammar:              startup.RawGrammar{Present: false, Lark: mo.None[string](), Regex: mo.None[string]()},
	}
	switch constraint.WhichConfig() {
	case extensionpb.ConstrainedSampling_JsonSchema_case:
		raw.Kind = startup.RawConstrainedSamplingJSONSchema
		config := constraint.GetJsonSchema()
		if config != nil {
			raw.JSONSchemaPresent = true
			raw.JSONSchemaStrictness = startup.RawJSONSchemaStrictness(config.GetStrictness())
		}
	case extensionpb.ConstrainedSampling_Grammar_case:
		raw.Kind = startup.RawConstrainedSamplingGrammar
		config := constraint.GetGrammar()
		if config != nil {
			raw.Grammar.Present = true
			if config.HasLark() {
				raw.Grammar.Lark = mo.Some(config.GetLark())
			}
			if config.HasRegex() {
				raw.Grammar.Regex = mo.Some(config.GetRegex())
			}
		}
	case extensionpb.ConstrainedSampling_Config_not_set_case:
	default:
		raw.Kind = startup.RawConstrainedSamplingInvalid
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
