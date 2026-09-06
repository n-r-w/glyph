// Package main provides an external Extension command built only from public Glyph packages.
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// signalsEnvironment names the directory used for process synchronization.
	signalsEnvironment = "GLYPH_EXTERNAL_SIGNALS"
	// lifecycleEnvironment enables the agent-start observer scenario.
	lifecycleEnvironment = "GLYPH_EXTERNAL_LIFECYCLE"
	// toolName identifies the fixture's only registered tool.
	toolName = "external"
	// invalidArgumentCode classifies invalid fixture requests.
	invalidArgumentCode = "INVALID_ARGUMENT"
	// internalFailureCode classifies the fixture's explicit operation failure.
	internalFailureCode = "INTERNAL"
	// ordinaryMode selects immediate successful execution.
	ordinaryMode = "ordinary"
	// cataloguesMode reads model and provider catalogs through the invocation context.
	cataloguesMode = "catalogs"
	// staleCataloguesMode exercises a retained binding instead of the current invocation binding.
	staleCataloguesMode = "stale-catalogs"
	// configuredRequestMode executes one explicit configured-model request.
	configuredRequestMode = "configured-request"
	// sessionStateMode appends or recovers one durable hidden checkpoint.
	sessionStateMode = "session-state"
	// failureMode selects classified execution failure.
	failureMode = "fail"
	// cancellationMode selects execution blocked until targeted cancellation.
	cancellationMode = "cancel"
	// shutdownMode selects execution blocked until connection shutdown.
	shutdownMode = "shutdown"
	// navigationRequestHandlerID identifies the fixture's pre-commit navigation handler.
	navigationRequestHandlerID = "append-before-navigation"
	// navigationObserverID identifies the fixture's committed-navigation observer.
	navigationObserverID = "append-after-navigation"
	// agentStartObserverID identifies the fixture's model-assisted lifecycle observer.
	agentStartObserverID = "observe-agent-start"
	// observerMessageEntryType identifies messages appended by the navigation observer.
	observerMessageEntryType = "navigation-observer"
	// observerMessageText is the exact observer-appended text.
	observerMessageText = "observer appended"
	// requestMessageText is the exact pre-commit handler-appended text.
	requestMessageText = "request handler appended"
	// signalFileMode restricts process synchronization files to their owner.
	signalFileMode = 0o600
	// gatePollInterval bounds process cleanup gate observation latency.
	gatePollInterval = 10 * time.Millisecond
)

// service implements the public Extension SDK contract.
type service struct {
	// contextMutex protects the retained binding across concurrent invocations.
	contextMutex sync.Mutex
	// savedContext retains the first observed binding for stale-context scenarios.
	savedContext *extensionsdk.ExtensionContext
	// signals stores the process synchronization directory.
	signals string
	// savedMessageID identifies the message that activates the navigation observer.
	savedMessageID string
	// savedCheckpointID identifies the target that activates and cancels from the request handler.
	savedCheckpointID string
	// lifecycleEnabled reports whether this process registers the agent-start observer scenario.
	lifecycleEnabled bool
}

// registerOperation returns the fixture catalog.
type registerOperation struct {
	// lifecycleEnabled adds the model-assisted agent-start observer when requested.
	lifecycleEnabled bool
}

// handleOperation optionally appends one message after a selected navigation commit.
type handleOperation struct {
	// context supplies nested public Host operations.
	context *extensionsdk.ExtensionContext
	// request contains the committed navigation metadata.
	request *extensionv1.HandleRequest
	// expectedMessageID selects whether this observer appends.
	expectedMessageID string
	// expectedCheckpointID selects whether the pre-commit handler appends and cancels.
	expectedCheckpointID string
	// lifecycle reports that this operation observes agent start.
	lifecycle bool
}

// executeOperation owns one mode-specific tool invocation.
type executeOperation struct {
	// savedContext is the binding retained by the extension rather than refreshed by Host.
	savedContext *extensionsdk.ExtensionContext
	// signals stores the process synchronization directory.
	signals string
	// mode selects ordinary, failure, cancellation, or shutdown behavior.
	mode string
	// service retains public append identity for a later observer invocation.
	service *service
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
	extensionsdk.Serve(&service{
		signals: os.Getenv(signalsEnvironment), contextMutex: sync.Mutex{}, savedContext: nil,
		savedMessageID: "", savedCheckpointID: "", lifecycleEnabled: os.Getenv(lifecycleEnvironment) == "1",
	})
}

// PrepareRegister admits the fixture registration operation.
func (s *service) PrepareRegister(
	context.Context,
	*extensionv1.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	return &registerOperation{lifecycleEnabled: s.lifecycleEnabled}, nil
}

// PrepareHandle admits the fixture's committed-navigation observer.
func (s *service) PrepareHandle(
	ctx context.Context,
	request *extensionv1.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	if request == nil || request.GetHandlerId() != navigationObserverID &&
		request.GetHandlerId() != navigationRequestHandlerID && request.GetHandlerId() != agentStartObserverID {
		return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("external fixture handler request is invalid"))
	}
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	s.contextMutex.Lock()
	expectedMessageID := s.savedMessageID
	expectedCheckpointID := s.savedCheckpointID
	s.contextMutex.Unlock()
	return &handleOperation{
		context: binding, request: request, expectedMessageID: expectedMessageID,
		expectedCheckpointID: expectedCheckpointID, lifecycle: request.GetHandlerId() == agentStartObserverID,
	}, nil
}

// PrepareExecute validates and admits one fixture tool operation.
func (s *service) PrepareExecute(
	ctx context.Context,
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
	case ordinaryMode,
		cataloguesMode,
		staleCataloguesMode,
		configuredRequestMode,
		sessionStateMode,
		failureMode,
		cancellationMode,
		shutdownMode:
		binding, err := extensionsdk.ContextFrom(ctx)
		if err != nil {
			return nil, err
		}
		s.contextMutex.Lock()
		if s.savedContext == nil && arguments.Mode == cataloguesMode {
			s.savedContext = binding
		}
		saved := s.savedContext
		s.contextMutex.Unlock()
		return &executeOperation{signals: s.signals, mode: arguments.Mode, savedContext: saved, service: s}, nil
	default:
		return nil, extensionsdk.Reject(invalidArgumentCode, errors.New("external fixture mode is invalid"))
	}
}

// Run returns the public catalog for the external tool.
func (operation *registerOperation) Run(context.Context) (*extensionv1.RegisterResponse, error) {
	handlers := []*extensionv1.HandlerDescriptor{
		extensionv1.HandlerDescriptor_builder{
			Id:   new(navigationRequestHandlerID),
			Kind: new(extensionv1.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST),
		}.Build(),
		extensionv1.HandlerDescriptor_builder{
			Id: new(navigationObserverID), Kind: new(extensionv1.HandlerKind_HANDLER_KIND_SESSION_TREE),
		}.Build(),
	}
	if operation.lifecycleEnabled {
		handlers = append(handlers, extensionv1.HandlerDescriptor_builder{
			Id: new(agentStartObserverID), Kind: new(extensionv1.HandlerKind_HANDLER_KIND_AGENT_START),
		}.Build())
	}
	return extensionv1.RegisterResponse_builder{
		Tools: []*extensionv1.ToolDescriptor{extensionv1.ToolDescriptor_builder{
			Name: new(toolName), Description: new("Exercise the public Extension SDK."),
			InputSchemaJson: []byte(`{"type":"object"}`), ConstrainedSampling: nil,
		}.Build()},
		Handlers: handlers,
	}.Build(), nil
}

// Release frees the registration operation, which owns no reservation.
func (*registerOperation) Release() {}

// Run appends and awaits one independent message only for the retained selected message.
func (operation *handleOperation) Run(ctx context.Context) (*extensionv1.HandleResponse, error) {
	if operation.lifecycle {
		if err := observeAgentStart(ctx, operation.context, operation.request.GetLifecycle()); err != nil {
			return nil, err
		}
		response := new(extensionv1.HandleResponse)
		response.SetLifecycle(new(extensionv1.LifecycleAction))
		return response, nil
	}
	if request := operation.request.GetSessionBeforeTreeRequest(); request != nil {
		if operation.expectedCheckpointID != "" &&
			request.GetCurrentRequest().GetTargetEntryId() == operation.expectedCheckpointID {
			if err := operation.appendMessage(ctx, requestMessageText); err != nil {
				return nil, err
			}
			response := new(extensionv1.HandleResponse)
			response.SetSessionBeforeTreeRequest(extensionv1.SessionBeforeTreeRequestAction_builder{
				Cancel: new(true), RequestAction: nil, Request: nil, ResultAction: nil, Result: nil,
			}.Build())
			return response, nil
		}
		response := new(extensionv1.HandleResponse)
		response.SetSessionBeforeTreeRequest(extensionv1.SessionBeforeTreeRequestAction_builder{
			Cancel: new(false), RequestAction: new(extensionv1.RequestAction_REQUEST_ACTION_PRESERVE),
			Request: nil, ResultAction: new(extensionv1.ResultAction_RESULT_ACTION_PRESERVE), Result: nil,
		}.Build())
		return response, nil
	}
	if operation.expectedMessageID != "" &&
		operation.request.GetSessionTree().GetTargetEntryId() == operation.expectedMessageID {
		if err := operation.appendMessage(ctx, observerMessageText); err != nil {
			return nil, err
		}
	}
	response := new(extensionv1.HandleResponse)
	response.SetSessionTree(new(extensionv1.SessionTreeAction))
	return response, nil
}

// appendMessage appends and awaits one visible message through the invocation context.
func (operation *handleOperation) appendMessage(ctx context.Context, text string) error {
	appendOperation, err := operation.context.StartAppendExtensionMessage(
		ctx,
		extensionv1.AppendExtensionMessageRequest_builder{
			Context: nil, EntryType: new(observerMessageEntryType), Text: new(text),
			Visibility: new(extensionv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE),
		}.Build(),
	)
	if err != nil {
		return err
	}
	result, err := appendOperation.Wait(ctx)
	if err != nil {
		return err
	}
	if len(result.GetIssues()) != 0 {
		return errors.New("navigation handler append returned a delivery issue")
	}
	return nil
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
	case cataloguesMode:
		return readCatalogues(ctx)
	case staleCataloguesMode:
		return operation.readRetainedCatalogues(ctx)
	case configuredRequestMode:
		return requestConfiguredModel(ctx)
	case sessionStateMode:
		return exerciseSessionState(ctx, operation.service)
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
