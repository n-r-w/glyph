package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// traceEnvironment names a file that receives one line per invocation when set.
	traceEnvironment = "GLYPH_CONTEXT_TRACE"
	// failMode fails the first instructions-a invocation before provider dispatch.
	failMode = "context-fail"
	// failSecondMode fails the second instructions-a invocation before provider dispatch.
	failSecondMode = "context-fail-second"
	// invalidMode returns an invalid history action on the first context-a invocation.
	invalidMode = "context-invalid"
	// badReferenceMode returns an unknown replay reference on the first context-a invocation.
	badReferenceMode = "context-bad-reference"
	// unknownReplayReference is a replay reference that Host never issued.
	unknownReplayReference = "unknown"
	// tracePermission is the file mode for owner-only reading and writing.
	tracePermission = 0o600
)

// trace appends the handler identifier and public payload of one invocation to the trace file.
func (s *service) trace(request *extensionpb.HandleRequest) (returnErr error) {
	path := os.Getenv(traceEnvironment)
	if path == "" {
		return nil
	}
	var payload proto.Message = request.GetSystemInstructions()
	if request.GetModelContext() != nil {
		payload = request.GetModelContext()
	}
	encoded, err := protojson.Marshal(payload)
	if err != nil {
		return err
	}
	s.traceMutex.Lock()
	defer s.traceMutex.Unlock()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	file, err := root.OpenFile(filepath.Base(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, tracePermission)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	_, err = fmt.Fprintf(file, "%s\t%s\n", request.GetHandlerId(), encoded)
	return err
}

// scenarioFailure returns the single failure selected by the executable name.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only the active action or ordinary failure.
func (o *operation) scenarioFailure() (*extensionpb.HandleResponse, bool) {
	id := o.request.GetHandlerId()
	instructions := int64(0)
	if id == instructionsA {
		instructions = o.service.instructionsInvocations.Add(1)
	}
	switch {
	case o.service.mode == failMode && instructions == 1, o.service.mode == failSecondMode && instructions == 2:
		return extensionpb.HandleResponse_builder{
			Error: extensionpb.HandlerError_builder{Message: new(failureText)}.Build(),
		}.Build(), true
	case o.service.mode == invalidMode && id == contextA && o.service.failed.CompareAndSwap(false, true):
		return extensionpb.HandleResponse_builder{
			ModelContext: extensionpb.ModelContextAction_builder{}.Build(),
		}.Build(), true
	case o.service.mode == badReferenceMode && id == contextA && o.service.failed.CompareAndSwap(false, true):
		return extensionpb.HandleResponse_builder{ModelContext: extensionpb.ModelContextAction_builder{
			Replace: extensionpb.ModelContextHistory_builder{Entries: append(
				o.request.GetModelContext().GetCurrent().GetEntries(), unknownReplayEntry(),
			)}.Build(),
		}.Build()}.Build(), true
	default:
		return nil, false
	}
}

// unknownReplayEntry builds a model response whose reasoning block carries an unissued replay reference.
//
//nolint:exhaustruct_v5 // The protobuf oneof builder sets only the model payload.
func unknownReplayEntry() *extensionpb.ModelContextEntry {
	return extensionpb.ModelContextEntry_builder{
		Model: extensionpb.ReplayableModelResponse_builder{Content: []*extensionpb.ReplayableModelContent{
			extensionpb.ReplayableModelContent_builder{
				Content: extensionpb.SessionTreeModelContent_builder{
					Kind:     new(extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_REASONING),
					Text:     new("reasoning"),
					ToolCall: nil,
				}.Build(),
				ReplayReference: new(unknownReplayReference),
			}.Build(),
		}}.Build(),
	}.Build()
}

// withoutReplayReferences copies history entries and removes every replay reference.
func withoutReplayReferences(entries []*extensionpb.ModelContextEntry) []*extensionpb.ModelContextEntry {
	result := make([]*extensionpb.ModelContextEntry, len(entries))
	for index, entry := range entries {
		cloned := proto.CloneOf(entry)
		for _, content := range cloned.GetModel().GetContent() {
			content.ClearReplayReference()
		}
		result[index] = cloned
	}
	return result
}
