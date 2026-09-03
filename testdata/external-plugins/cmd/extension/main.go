// Package main provides an external Extension command built only from public Glyph packages.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// signalsEnvironment names the directory used for process synchronization.
	signalsEnvironment = "GLYPH_EXTERNAL_SIGNALS"
	// toolName identifies the fixture's only registered tool.
	toolName = "external"
	// invalidArgumentCode classifies invalid fixture requests.
	invalidArgumentCode = "INVALID_ARGUMENT"
	// internalFailureCode classifies the fixture's explicit operation failure.
	internalFailureCode = "INTERNAL"
	// ordinaryMode selects immediate successful execution.
	ordinaryMode = "ordinary"
	// failureMode selects classified execution failure.
	failureMode = "fail"
	// cancellationMode selects execution blocked until targeted cancellation.
	cancellationMode = "cancel"
	// shutdownMode selects execution blocked until connection shutdown.
	shutdownMode = "shutdown"
	// signalFileMode restricts process synchronization files to their owner.
	signalFileMode = 0o600
	// gatePollInterval bounds process cleanup gate observation latency.
	gatePollInterval = 10 * time.Millisecond
)

// service implements the public Extension SDK contract.
type service struct {
	// signals stores the process synchronization directory.
	signals string
}

// registerOperation returns the fixture catalog.
type registerOperation struct{}

// handleOperation returns an empty handler result when directly admitted.
type handleOperation struct{}

// executeOperation owns one mode-specific tool invocation.
type executeOperation struct {
	// signals stores the process synchronization directory.
	signals string
	// mode selects ordinary, failure, cancellation, or shutdown behavior.
	mode string
}

// executeArguments is the public JSON input accepted by the fixture tool.
type executeArguments struct {
	// Mode selects ordinary, failure, cancellation, or shutdown behavior.
	Mode string `json:"mode"`
}

var (
	// Compile-time assertions prove the fixture implements only public SDK interfaces.
	_ extensionsdk.Service           = (*service)(nil)
	_ extensionsdk.RegisterOperation = (*registerOperation)(nil)
	_ extensionsdk.HandleOperation   = (*handleOperation)(nil)
	_ extensionsdk.ExecuteOperation  = (*executeOperation)(nil)
)

// main serves the external Extension fixture through the public SDK.
func main() {
	extensionsdk.Serve(&service{signals: os.Getenv(signalsEnvironment)})
}

// PrepareRegister admits the fixture registration operation.
func (*service) PrepareRegister(
	context.Context,
	*extensionv1.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	return &registerOperation{}, nil
}

// PrepareHandle rejects handler work because the fixture registers no handlers.
func (*service) PrepareHandle(
	context.Context,
	*extensionv1.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("external fixture registers no handlers"))
}

// PrepareExecute validates and admits one fixture tool operation.
func (s *service) PrepareExecute(
	_ context.Context,
	request *extensionv1.ExecuteRequest,
) (extensionsdk.ExecuteOperation, error) {
	if request.GetToolName() != toolName {
		return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("external fixture tool name is invalid"))
	}
	arguments := executeArguments{}
	if err := json.Unmarshal(request.GetArgumentsJson(), &arguments); err != nil {
		return nil, extensionsdk.Reject(invalidArgumentCode, err)
	}
	switch arguments.Mode {
	case ordinaryMode, failureMode, cancellationMode, shutdownMode:
		return &executeOperation{signals: s.signals, mode: arguments.Mode}, nil
	default:
		return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("external fixture mode is invalid"))
	}
}

// Run returns the public catalog for the external tool.
func (*registerOperation) Run(context.Context) (*extensionv1.RegisterResponse, error) {
	return extensionv1.RegisterResponse_builder{
		Tools: []*extensionv1.ToolDescriptor{extensionv1.ToolDescriptor_builder{
			Name: new(toolName), Description: new("Exercise the public Extension SDK."),
			InputSchemaJson: []byte(`{"type":"object"}`), ConstrainedSampling: nil,
		}.Build()},
		Handlers: nil,
	}.Build(), nil
}

// Release frees the registration operation, which owns no reservation.
func (*registerOperation) Release() {}

// Run returns an empty handler result for interface completeness.
func (*handleOperation) Run(context.Context) (*extensionv1.HandleResponse, error) {
	return new(extensionv1.HandleResponse), nil
}

// Release frees the handler operation, which owns no reservation.
func (*handleOperation) Release() {}

// Run completes, fails, or blocks according to the requested fixture mode.
func (operation *executeOperation) Run(
	ctx context.Context,
	_ *extensionsdk.ProgressReporter,
) (*extensionv1.ToolResult, error) {
	switch operation.mode {
	case ordinaryMode:
		return extensionv1.ToolResult_builder{
			Contents: []*extensionv1.ToolResultContent{
				//nolint:exhaustruct_v5 // The public builder sets only the active text field.
				extensionv1.ToolResultContent_builder{Text: new("ordinary complete")}.Build(),
			},
			IsError: new(false),
		}.Build(), nil
	case failureMode:
		return nil, extensionsdk.Fail(internalFailureCode, errors.New("complete external Extension failure"))
	default:
		signal(operation.signals, operation.mode+"-run-started")
		<-ctx.Done()
		return nil, context.Cause(ctx)
	}
}

// Release holds blocked cleanup at a process-visible gate before terminal delivery.
func (operation *executeOperation) Release() {
	if operation.mode == cancellationMode || operation.mode == shutdownMode {
		prefix := operation.mode + "-cleanup-"
		signal(operation.signals, prefix+"started")
		waitForSignal(operation.signals, prefix+"gate")
		signal(operation.signals, prefix+"finished")
	}
}

// signal writes one child-process synchronization marker.
func signal(directory, name string) {
	if err := os.WriteFile(filepath.Join(directory, name), nil, signalFileMode); err != nil {
		panic(err)
	}
}

// waitForSignal holds process cleanup until the root test opens its gate.
func waitForSignal(directory, name string) {
	path := filepath.Join(directory, name)
	ticker := time.NewTicker(gatePollInterval)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			panic(err)
		}
		<-ticker.C
	}
}
