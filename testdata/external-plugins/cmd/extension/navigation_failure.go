package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// Navigation mode components select participants and injected failures.
	navigationModeRequest     = "request"
	navigationModeObserver    = "observer"
	navigationModeResult      = "result"
	navigationModeConsumed    = "consumed"
	navigationModeUnavailable = "unavailable"
	navigationModeInvalid     = "invalid"
	navigationModeCancel      = "cancel"
	// navigationFailureEnvironment selects the failing navigation participant.
	navigationFailureEnvironment = "GLYPH_NAVIGATION_FAILURE"
	// navigationFailureText preserves the complete public failure diagnostic.
	navigationFailureText = "navigation participant full unique failure: original source Ω"
	// navigationLaterID identifies the participant that must not run after failure.
	navigationLaterID = "navigation-later"
	// navigationResultID identifies the summary result participant.
	navigationResultID = "navigation-result"
	// navigationFailureMarker separates the failing invocation from healthy subsequent navigation.
	navigationFailureMarker = "navigation-failed-once"
	// navigationHealthySignal proves the same extension participated after its ordinary failure.
	navigationHealthySignal = "navigation-healthy"
	// navigationReadySummary supplies a result without built-in generation.
	navigationReadySummary = "ready summary"
)

// navigationFailureDescriptors registers a later participant of the selected chain.
func navigationFailureDescriptors() []*extensionpb.HandlerDescriptor {
	mode := os.Getenv(navigationFailureEnvironment)
	if mode == "" {
		return nil
	}
	kind := extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST
	if strings.HasPrefix(mode, navigationModeObserver) {
		kind = extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE
	}
	if strings.HasPrefix(mode, navigationModeResult) {
		kind = extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_RESULT
	}
	handlers := []*extensionpb.HandlerDescriptor{}
	if strings.HasPrefix(mode, navigationModeResult) {
		handlers = append(
			handlers,
			extensionpb.HandlerDescriptor_builder{Id: new(navigationResultID), Kind: new(kind)}.Build(),
		)
	}
	return append(handlers, extensionpb.HandlerDescriptor_builder{Id: new(navigationLaterID), Kind: new(kind)}.Build())
}

// runNavigationFailure dispatches the selected failure and preserves ordinary fixture behavior elsewhere.
func (operation *handleOperation) runNavigationFailure(ctx context.Context) (*extensionpb.HandleResponse, error) {
	mode := os.Getenv(navigationFailureEnvironment)
	id := operation.request.GetHandlerId()
	if id == navigationLaterID {
		signal(operation.signals, navigationLaterID)
		return operation.runHandler(ctx)
	}
	_, markerErr := os.Stat(filepath.Join(operation.signals, navigationFailureMarker))
	if markerErr == nil {
		if id == navigationRequestHandlerID || id == navigationObserverID || id == navigationResultID {
			signal(operation.signals, navigationHealthySignal)
		}
		return operation.runHandler(ctx)
	}
	if !errors.Is(markerErr, os.ErrNotExist) {
		return nil, markerErr
	}
	if id == navigationRequestHandlerID && strings.HasPrefix(mode, navigationModeResult) {
		return operation.readyNavigationResult(), nil
	}
	selected := id == navigationRequestHandlerID && strings.HasPrefix(mode, navigationModeRequest) ||
		id == navigationObserverID && strings.HasPrefix(mode, navigationModeObserver) || id == navigationResultID
	if !selected {
		return operation.runHandler(ctx)
	}
	signal(operation.signals, navigationFailureMarker)
	return operation.failNavigationParticipant(ctx, mode)
}

// readyNavigationResult supplies a typed summary to reach the public result-handler chain.
func (operation *handleOperation) readyNavigationResult() *extensionpb.HandleResponse {
	source := new(extensionpb.BranchSummarySource)
	source.SetExtensionId(operation.request.GetContext().GetExtensionId())
	response := new(extensionpb.HandleResponse)
	response.SetSessionBeforeTreeRequest(extensionpb.SessionBeforeTreeRequestAction_builder{
		Cancel:        new(false),
		RequestAction: new(extensionpb.RequestAction_REQUEST_ACTION_PRESERVE),
		Request:       nil,
		ResultAction:  new(extensionpb.ResultAction_RESULT_ACTION_REPLACE),
		Result: extensionpb.BranchSummaryResult_builder{
			Summary: new(navigationReadySummary),
			Source:  source,
		}.Build(),
	}.Build())
	return response
}

// failNavigationParticipant returns the configured error or deliberately consumes an independently failed child.
func (operation *handleOperation) failNavigationParticipant(
	ctx context.Context,
	mode string,
) (*extensionpb.HandleResponse, error) {
	if strings.HasSuffix(mode, navigationModeConsumed) {
		caller := context.WithoutCancel(ctx)
		child, err := operation.context.StartReasoningSelection(caller, extensionpb.SelectReasoningRequest_builder{
			Context: nil, ReasoningChoice: new(selectionFinalReasoning),
		}.Build())
		if err != nil {
			return nil, err
		}
		_, _ = child.Wait(caller)
		return operation.runHandler(ctx)
	}
	if strings.HasSuffix(mode, navigationModeUnavailable) {
		os.Exit(selectionFailureExitCode)
	}
	if strings.HasSuffix(mode, navigationModeInvalid) {
		return new(extensionpb.HandleResponse), nil
	}
	if strings.HasSuffix(mode, navigationModeCancel) {
		return nil, context.Canceled
	}
	return nil, errors.New(navigationFailureText)
}
