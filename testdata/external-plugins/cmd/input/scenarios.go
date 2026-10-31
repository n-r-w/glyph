package main

import (
	"context"
	"errors"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// keepImagesMode makes input-images preserve the current input.
	keepImagesMode = "input-keep-images"
	// addImageMode makes input-images append an image block to the current input.
	addImageMode = "input-add-image"
	// originalMode makes input-text append marker A and input-images replace from the original plus marker B.
	originalMode = "input-original"
	// failMode fails the first input-text invocation.
	failMode = "input-fail"
	// invalidMode returns an empty action from the first input-text invocation.
	invalidMode = "input-invalid"
	// blockMode blocks the first input-text invocation until Host cancels it.
	blockMode = "input-block"
	// exitMode terminates the extension process during the first input-text invocation.
	exitMode = "input-exit"
	// markerA is the text block that input-text appends in the original scenario.
	markerA = "glyph-input-A"
	// markerB is the text block that input-images appends to the original input in the original scenario.
	markerB = "glyph-input-B"
	// addedImageMediaType is the media type of the image that the add-image scenario appends.
	addedImageMediaType = "image/png"
	// signalsEnvironment names a directory that receives the blocked-invocation marker when set.
	signalsEnvironment = "GLYPH_INPUT_SIGNALS"
	// blockedSignal is the marker file written when the blocked invocation starts.
	blockedSignal = "input-blocked"
	// exitStatus is the process status of the exit scenario.
	exitStatus = 17
)

// text selects the input-text action of the scenario.
func (o *operation) text(
	ctx context.Context,
	input *extensionpb.UserInputInvocation,
) (*extensionpb.HandleResponse, error) {
	switch o.service.mode {
	case originalMode:
		return replace(appendBlock(input.GetCurrent(), textBlock(markerA))), nil
	case blockMode:
		if o.service.failed.CompareAndSwap(false, true) {
			return nil, block(ctx)
		}
	case exitMode:
		os.Exit(exitStatus)
	}
	if response, selected := o.scenarioFailure(); selected {
		return response, nil
	}
	return replace(transformText(input.GetCurrent())), nil
}

// images selects the input-images action of the scenario.
func (o *operation) images(input *extensionpb.UserInputInvocation) *extensionpb.HandleResponse {
	switch o.service.mode {
	case keepImagesMode:
		return preserve()
	case addImageMode:
		image := extensionpb.SessionTreeImage_builder{
			MediaType: new(addedImageMediaType), Data: []byte{0x89, 'P', 'N', 'G'},
		}.Build()
		return replace(appendBlock(
			input.GetCurrent(), extensionpb.SessionTreeUserContent_builder{Text: nil, Image: image}.Build(),
		))
	case originalMode:
		return replace(appendBlock(input.GetOriginal(), textBlock(markerB)))
	default:
		return replace(removeImages(input.GetCurrent()))
	}
}

// block reports the started invocation and waits until Host cancels it.
func block(ctx context.Context) error {
	if directory := os.Getenv(signalsEnvironment); directory != "" {
		if err := writeSignal(directory); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// writeSignal creates the blocked-invocation marker inside the signal directory.
func writeSignal(directory string) (returnErr error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	file, err := root.OpenFile(blockedSignal, os.O_CREATE|os.O_WRONLY, tracePermission)
	if err != nil {
		return err
	}
	return file.Close()
}

// scenarioFailure returns the single failure that the executable name selects.
func (o *operation) scenarioFailure() (*extensionpb.HandleResponse, bool) {
	if o.service.mode != failMode && o.service.mode != invalidMode || !o.service.failed.CompareAndSwap(false, true) {
		return nil, false
	}
	if o.service.mode == invalidMode {
		//nolint:exhaustruct_v5 // An empty action is the invalid result of this scenario.
		return extensionpb.HandleResponse_builder{
			UserInput: extensionpb.UserInputAction_builder{}.Build(),
		}.Build(), true
	}
	//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active error.
	return extensionpb.HandleResponse_builder{
		Error: extensionpb.HandlerError_builder{Message: new(failureText)}.Build(),
	}.Build(), true
}

// appendBlock returns a copy of the input with one more block.
func appendBlock(
	input *extensionpb.SessionTreeUserMessage,
	block *extensionpb.SessionTreeUserContent,
) *extensionpb.SessionTreeUserMessage {
	content := append(append([]*extensionpb.SessionTreeUserContent(nil), input.GetContent()...), block)
	return extensionpb.SessionTreeUserMessage_builder{Content: content}.Build()
}
