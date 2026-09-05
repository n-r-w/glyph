//go:build integration

package app

import (
	"fmt"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// summaryControlExtensionMode supplies non-model output with an unavailable unused model.
	summaryControlExtensionMode = "control-extension"
	// summaryControlMissingMode supplies non-model output without a built-in selection.
	summaryControlMissingMode = "control-missing"
	// summaryControlModelMode replaces output with usage from another configured model.
	summaryControlModelMode = "control-model"
	// summaryControlClearMode removes a supplied result and restores built-in dispatch.
	summaryControlClearMode = "control-clear"
	// summaryControlInvalidMode returns a result without its required producer.
	summaryControlInvalidMode = "control-invalid"
	// summaryControlCancelMode cancels from a result handler before persistence.
	summaryControlCancelMode = "control-cancel"
	// summaryControlProducer identifies the explicit source, not the forwarding handler.
	summaryControlProducer = "cooperating-producer"
	// summaryRequestCause is the nested ordinary request-handler failure.
	summaryRequestCause = "load summary rules: open rules.json: permission denied"
	// summaryResultCause is the nested ordinary result-handler failure.
	summaryResultCause = "refine summary: read glossary.json: invalid JSON"
	// summaryObserverCause is the nested post-commit observer failure.
	summaryObserverCause = "save navigation receipt: write receipt.json: disk full"
)

// summaryControlRegistration registers failing handlers followed by state-sensitive continuations.
func summaryControlRegistration(mode string) *extensionpb.RegisterResponse {
	handlers := []*extensionpb.HandlerDescriptor{
		extensionpb.HandlerDescriptor_builder{
			Id:   new("request-failure"),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new("supply"),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new("result-failure"),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_RESULT),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new("refine"),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_RESULT),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new("observer-failure"),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new("observer-after"),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE),
		}.Build(),
	}
	if mode == summaryControlClearMode {
		handlers = append(handlers, extensionpb.HandlerDescriptor_builder{
			Id: new("clear"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST),
		}.Build())
	}
	return extensionpb.RegisterResponse_builder{Tools: nil, Handlers: handlers}.Build()
}

// summaryControlFailure returns an ordinary handler failure through the public error alternative.
func summaryControlFailure(message string) *extensionpb.HandleResponse {
	wire := new(extensionpb.HandleResponse)
	wire.SetError(extensionpb.HandlerError_builder{Message: new(message)}.Build())
	return wire
}

// runSummaryControlHandler exercises errors, state preservation, explicit source, and model replacement.
func (operation *handlerFixtureHandleOperation) runSummaryControlHandler() (*extensionpb.HandleResponse, error) {
	request := operation.request
	switch request.GetHandlerId() {
	case "request-failure":
		return summaryControlFailure(summaryRequestCause), nil
	case "result-failure":
		return summaryControlFailure(summaryResultCause), nil
	case "observer-failure":
		return summaryControlFailure(summaryObserverCause), nil
	case "clear":
		wire := new(extensionpb.HandleResponse)
		wire.SetSessionBeforeTreeRequest(extensionpb.SessionBeforeTreeRequestAction_builder{
			Cancel: new(false), RequestAction: new(extensionpb.RequestAction_REQUEST_ACTION_PRESERVE), Request: nil,
			ResultAction: new(extensionpb.ResultAction_RESULT_ACTION_CLEAR), Result: nil,
		}.Build())
		return wire, nil
	case "supply":
		invocation := request.GetSessionBeforeTreeRequest()
		if invocation.GetCurrentResult() != nil || invocation.GetCurrentRequest().GetTargetEntryId() != "user" {
			return nil, fmt.Errorf("request error changed incoming state")
		}
		replacement := invocation.GetCurrentRequest()
		if operation.fixture.mode == summaryControlMissingMode {
			replacement.ClearSummaryModel()
		} else {
			replacement.SetSummaryModel(extensionpb.ModelSelection_builder{
				ProviderId: new("openai-codex"), ModelId: new("unused"), ReasoningChoice: new("off"),
			}.Build())
		}
		wire := new(extensionpb.HandleResponse)
		wire.SetSessionBeforeTreeRequest(extensionpb.SessionBeforeTreeRequestAction_builder{
			Cancel: new(
				false,
			),
			RequestAction: new(extensionpb.RequestAction_REQUEST_ACTION_REPLACE),
			Request:       replacement,
			ResultAction:  new(extensionpb.ResultAction_RESULT_ACTION_REPLACE),
			Result: extensionpb.BranchSummaryResult_builder{
				Summary: new(
					"supplied",
				),
				Source: extensionpb.BranchSummarySource_builder{
					ExtensionId: new(summaryControlProducer),
					Model:       nil,
				}.Build(),
			}.Build(),
		}.Build())
		return wire, nil
	case "refine":
		invocation := request.GetSessionBeforeTreeResult()
		if invocation.GetCurrentResult().GetSummary() != "supplied" ||
			invocation.GetCurrentResult().GetSource().GetExtensionId() != summaryControlProducer {
			return nil, fmt.Errorf("result error changed incoming state")
		}
		if operation.fixture.mode == summaryControlCancelMode {
			wire := new(extensionpb.HandleResponse)
			wire.SetSessionBeforeTreeResult(extensionpb.SessionBeforeTreeResultAction_builder{
				Cancel: new(true), ResultAction: nil, Result: nil,
			}.Build())
			return wire, nil
		}
		result := invocation.GetCurrentResult()
		result.SetSummary("refined")
		if operation.fixture.mode == summaryControlInvalidMode {
			result.ClearSource()
		}
		if operation.fixture.mode == summaryControlModelMode {
			source := new(extensionpb.BranchSummarySource)
			source.SetModel(extensionpb.BranchSummaryModelSource_builder{
				Selection: extensionpb.ModelSelection_builder{
					ProviderId:      new("local"),
					ModelId:         new("priced"),
					ReasoningChoice: new("off"),
				}.Build(),
				Usage: extensionpb.TokenUsage_builder{
					InputTokens: new(int64(1000000)), OutputTokens: new(int64(0)), CacheReadTokens: new(int64(0)),
					CacheWriteTokens: new(int64(0)), ReasoningTokens: new(int64(0)), TotalTokens: new(int64(1000000)),
				}.Build(),
			}.Build())
			result.SetSource(source)
		}
		wire := new(extensionpb.HandleResponse)
		wire.SetSessionBeforeTreeResult(extensionpb.SessionBeforeTreeResultAction_builder{
			Cancel: new(false), ResultAction: new(extensionpb.ResultAction_RESULT_ACTION_REPLACE), Result: result,
		}.Build())
		return wire, nil
	case "observer-after":
		invocation := request.GetSessionTree()
		if invocation.GetCreatedSummary().GetSummary() != "refined" ||
			invocation.GetCommittedActiveLeafId() != invocation.GetCreatedSummary().GetEntryId() {
			return nil, fmt.Errorf("observer did not receive the committed summary")
		}
		wire := new(extensionpb.HandleResponse)
		wire.SetSessionTree(new(extensionpb.SessionTreeAction))
		return wire, nil
	default:
		return nil, fmt.Errorf("unknown summary control handler %q", request.GetHandlerId())
	}
}
