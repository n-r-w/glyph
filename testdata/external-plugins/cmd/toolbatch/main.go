// Package main supplies signal-driven tools through the public Extension SDK.
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/samber/mo"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// concurrentTool identifies the tool that permits concurrent execution.
	concurrentTool = "concurrent"
	// serialTool identifies the tool whose calls are serial barriers.
	serialTool = "serial"
	// inputSchema defines the test signal endpoint and call identity.
	inputSchema = `{"type":"object","properties":{"url":{"type":"string"},"id":{"type":"string"},` +
		`"terminate":{"type":"boolean"},"abort":{"type":"boolean"},"observer_abort":{"type":"boolean"}},` +
		`"required":["url","id"],"additionalProperties":false}`
)

// service admits the fixture's public tool operations.
type service struct {
	// mutex protects the one-shot observer target across tool and lifecycle invocations.
	mutex sync.Mutex
	// observerURL is the signal endpoint for an armed turn-end abort.
	observerURL mo.Option[string]
}

// The compile-time assertion checks the fixture service against the public SDK contract.
var _ extensionsdk.Service = (*service)(nil)

// registration declares the tools consumed by the public scenario.
type registration struct{}

// The compile-time assertion checks the fixture registration against the public SDK contract.
var _ extensionsdk.RegisterOperation = (*registration)(nil)

// execution retains the signal endpoint of one admitted call.
type execution struct {
	// url is the test-owned rendezvous endpoint.
	url string
	// id identifies the call at the rendezvous endpoint.
	id string
	// terminate is the requested run completion signal.
	terminate bool
	// abort requests cancellation after queue acceptance and the test signal.
	abort bool
}

// The compile-time assertion checks the fixture execution against the public SDK contract.
var _ extensionsdk.ExecuteOperation = (*execution)(nil)

// main serves the fixture over the public Extension Contract.
func main() { extensionsdk.Serve(&service{mutex: sync.Mutex{}, observerURL: mo.None[string]()}) }

// PrepareRegister admits registration of both tools.
func (s *service) PrepareRegister(
	context.Context,
	*extensionpb.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	return &registration{}, nil
}

// PrepareHandle captures and clears the one-shot target for a live turn-end observer.
func (s *service) PrepareHandle(
	_ context.Context,
	request *extensionpb.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	if request.GetHandlerId() != turnEndAbortObserverID || request.GetLifecycle().GetTurnEnd() == nil {
		return nil, errors.New("turn-end observer invocation is required")
	}
	s.mutex.Lock()
	target := s.observerURL
	s.observerURL = mo.None[string]()
	s.mutex.Unlock()
	return &turnEndAbortObserver{url: target}, nil
}

// PrepareExecute decodes the test-owned signal endpoint.
func (s *service) PrepareExecute(
	_ context.Context,
	request *extensionpb.ExecuteRequest,
) (extensionsdk.ExecuteOperation, error) {
	var arguments struct {
		URL           string `json:"url"`
		ID            string `json:"id"`
		Terminate     bool   `json:"terminate"`
		Abort         bool   `json:"abort"`
		ObserverAbort bool   `json:"observer_abort"`
	}
	if err := json.Unmarshal(request.GetArgumentsJson(), &arguments); err != nil {
		return nil, err
	}
	if arguments.ObserverAbort {
		s.mutex.Lock()
		s.observerURL = mo.Some(arguments.URL)
		s.mutex.Unlock()
	}
	return &execution{url: arguments.URL, id: arguments.ID, terminate: arguments.Terminate, abort: arguments.Abort}, nil
}

// Run declares one concurrent tool and one undeclared serial tool.
func (r *registration) Run(context.Context) (*extensionpb.RegisterResponse, error) {
	names := []string{concurrentTool, serialTool}
	tools := make([]*extensionpb.ToolDescriptor, 0, len(names))
	for _, name := range names {
		var concurrent *bool
		if name == concurrentTool {
			concurrent = new(true)
		}
		tools = append(tools, extensionpb.ToolDescriptor_builder{
			Name: new(name), Description: new("Wait for the test signal."), InputSchemaJson: []byte(inputSchema),
			ConstrainedSampling: nil, ReplacesTool: nil, Concurrent: concurrent,
		}.Build())
	}
	return extensionpb.RegisterResponse_builder{Tools: tools, Handlers: []*extensionpb.HandlerDescriptor{
		extensionpb.HandlerDescriptor_builder{
			Id:   new(turnEndAbortObserverID),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_TURN_END),
		}.Build(),
	}}.Build(), nil
}

// Release frees registration admission.
func (r *registration) Release() {}

// Release frees execution admission.
func (e *execution) Release() {}

// Run waits for an HTTP response controlled by the test's completion signal.
func (e *execution) Run(ctx context.Context, _ *extensionsdk.ProgressReporter) (*extensionpb.ToolResult, error) {
	if e.abort {
		if err := e.queueAbortMessages(ctx); err != nil {
			return nil, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, e.url+"/"+e.id, http.NoBody)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(response.Body)
	if bodyErr := errors.Join(readErr, response.Body.Close()); bodyErr != nil {
		return nil, bodyErr
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signal endpoint: %s: %s", response.Status, string(body))
	}
	if e.abort {
		return nil, e.abortRun(ctx)
	}
	return extensionpb.ToolResult_builder{Contents: []*extensionpb.ToolResultContent{
		//nolint:exhaustruct_v5 // Only the text content variant is active.
		extensionpb.ToolResultContent_builder{Text: new(string(body))}.Build(),
	}, IsError: new(false), Terminate: new(e.terminate)}.Build(), nil
}
