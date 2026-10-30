// Package main demonstrates system-instruction and model-context middleware using only the public SDK.
package main

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// instructionsA identifies the first instruction transformation.
	instructionsA = "instructions-a"
	// instructionsB identifies replacement from immutable original instructions.
	instructionsB = "instructions-b"
	// contextA identifies the first history transformation.
	contextA = "context-a"
	// contextB identifies replacement from immutable original history.
	contextB = "context-b"
	// instructionsMarkerA demonstrates an instruction line replaced by the next handler.
	instructionsMarkerA = "glyph-instructions-A"
	// contextMarkerA demonstrates a request-local message replaced by the next handler.
	contextMarkerA = "glyph-context-A"
	// contextMarkerB demonstrates a request-local message derived from immutable original history.
	contextMarkerB = "glyph-context-B"
	// paragraphSeparator separates appended instruction text.
	paragraphSeparator = "\n\n"
	// failureText is a complete ordinary error with a retry-like descendant in its text.
	failureText = "reference context handler failed: unexpected EOF"
	// invalidArgument identifies rejected unsupported tool calls.
	invalidArgument = "INVALID_ARGUMENT"
	// countedMode appends the context-b invocation number to its marker.
	countedMode = "context-counted"
	// dropReplayMode removes every replay reference from the context-b replacement.
	dropReplayMode = "context-drop-replay"
)

// projectInstructions contains the example project instructions appended by instructions-b.
//
//go:embed project-instructions.md
var projectInstructions string

// service admits reference operations; the executable name selects acceptance scenarios.
type service struct {
	// mode selects a reference scenario through the executable name.
	mode string
	// failed records that the single scenario failure was already returned.
	failed atomic.Bool
	// instructionsInvocations counts instructions-a invocations in this extension process.
	instructionsInvocations atomic.Int64
	// contextInvocations counts context-b invocations in this extension process.
	contextInvocations atomic.Int64
	// traceMutex serializes trace file appends.
	traceMutex sync.Mutex
}

// Verify the reference service admits operations through the public SDK contract.
var _ extensionsdk.Service = (*service)(nil)

// registration supplies ordered public middleware declarations.
type registration struct{}

// Verify registration implements the public SDK registration contract.
var _ extensionsdk.RegisterOperation = (*registration)(nil)

// operation retains one admitted public invocation.
type operation struct {
	// service supplies the scenario and invocation counters.
	service *service
	// request contains the admitted public operation payload.
	request *extensionpb.HandleRequest
}

// Verify handler invocation implements the public SDK handler contract.
var _ extensionsdk.HandleOperation = (*operation)(nil)

// main starts the public-only reference process.
func main() {
	extensionsdk.Serve(&service{
		mode: filepath.Base(os.Args[0]), failed: atomic.Bool{}, instructionsInvocations: atomic.Int64{},
		contextInvocations: atomic.Int64{}, traceMutex: sync.Mutex{},
	})
}

// PrepareRegister admits the fixed reference catalog.
func (s *service) PrepareRegister(
	context.Context,
	*extensionpb.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	return &registration{}, nil
}

// PrepareHandle admits an invocation after SDK identity and payload validation.
func (s *service) PrepareHandle(
	_ context.Context,
	request *extensionpb.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	return &operation{service: s, request: request}, nil
}

// PrepareExecute rejects tools because this extension contributes middleware handlers only.
func (s *service) PrepareExecute(context.Context, *extensionpb.ExecuteRequest) (extensionsdk.ExecuteOperation, error) {
	return nil, extensionsdk.Reject(invalidArgument, errors.New("context reference extension defines no tools"))
}

// Run returns the complete ordered declaration list.
func (r *registration) Run(context.Context) (*extensionpb.RegisterResponse, error) {
	return extensionpb.RegisterResponse_builder{Tools: nil, Handlers: []*extensionpb.HandlerDescriptor{
		extensionpb.HandlerDescriptor_builder{
			Id:   new(instructionsA),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SYSTEM_INSTRUCTIONS),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new(instructionsB),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SYSTEM_INSTRUCTIONS),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new(contextA),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_MODEL_CONTEXT),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new(contextB),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_MODEL_CONTEXT),
		}.Build(),
	}}.Build(), nil
}

// Release frees the registration admission.
func (r *registration) Release() {}

// Release frees the handler admission.
func (o *operation) Release() {}

// Run transforms instructions or request-local history after recording the invocation when tracing is enabled.
func (o *operation) Run(context.Context) (*extensionpb.HandleResponse, error) {
	if err := o.service.trace(o.request); err != nil {
		return nil, err
	}
	if response, selected := o.scenarioFailure(); selected {
		return response, nil
	}
	if input := o.request.GetSystemInstructions(); input != nil {
		return o.transformInstructions(input), nil
	}
	return o.transformContext(o.request.GetModelContext()), nil
}

// transformInstructions appends marker A to current instructions or project instructions to the original.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active replacement payload.
func (o *operation) transformInstructions(input *extensionpb.SystemInstructionsInvocation) *extensionpb.HandleResponse {
	replacement := input.GetCurrent() + paragraphSeparator + instructionsMarkerA
	if o.request.GetHandlerId() == instructionsB {
		replacement = input.GetOriginal() + paragraphSeparator + strings.TrimSpace(projectInstructions)
	}
	return extensionpb.HandleResponse_builder{SystemInstructions: extensionpb.SystemInstructionsAction_builder{
		Replace: new(replacement),
	}.Build()}.Build()
}

// transformContext appends marker A to current history or marker B to the original history.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active replacement payload.
func (o *operation) transformContext(input *extensionpb.ModelContextInvocation) *extensionpb.HandleResponse {
	entries := append([]*extensionpb.ModelContextEntry(nil), input.GetCurrent().GetEntries()...)
	marker := contextMarkerA
	if o.request.GetHandlerId() == contextB {
		entries = append([]*extensionpb.ModelContextEntry(nil), input.GetOriginal().GetEntries()...)
		if o.service.mode == dropReplayMode {
			entries = withoutReplayReferences(entries)
		}
		marker = contextMarkerB
		count := o.service.contextInvocations.Add(1)
		if o.service.mode == countedMode {
			marker += "-" + strconv.FormatInt(count, 10)
		}
	}
	entries = append(entries, extensionpb.ModelContextEntry_builder{
		User: extensionpb.SessionTreeUserMessage_builder{Content: []*extensionpb.SessionTreeUserContent{
			extensionpb.SessionTreeUserContent_builder{Text: new(marker)}.Build(),
		}}.Build(),
	}.Build())
	return extensionpb.HandleResponse_builder{ModelContext: extensionpb.ModelContextAction_builder{
		Replace: extensionpb.ModelContextHistory_builder{Entries: entries}.Build(),
	}.Build()}.Build()
}
