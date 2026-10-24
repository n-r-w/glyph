package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// consumedSelectionEnvironment selects the nested selection request kind.
	consumedSelectionEnvironment = "GLYPH_CONSUMED_SELECTION_KIND"
	// consumedSelectionReportFile retains the real SDK result and failure.
	consumedSelectionReportFile = "consumed-selection.json"
	// consumedSelectionResultText is a deliberately successful parent tool result.
	consumedSelectionResultText = "selection failure consumed"
)

// exerciseConsumedSelection consumes a child selection failure and attempts more owned Host work.
func exerciseConsumedSelection(ctx context.Context) (*extensionpb.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	caller := context.WithoutCancel(ctx)
	var result *extensionpb.SelectionResult
	var failure error
	if os.Getenv(consumedSelectionEnvironment) == "model" {
		operation, startErr := binding.StartModelSelection(caller, extensionpb.SelectModelRequest_builder{
			Context: nil, ProviderId: new(selectionProviderID), ModelId: new(selectionModelID),
		}.Build())
		if startErr != nil {
			return nil, startErr
		}
		result, failure = operation.Wait(caller)
	} else {
		choice := selectionOriginalReasoning
		if os.Getenv(selectionObserversEnvironment) != "" {
			choice = selectionFinalReasoning
		}
		operation, startErr := binding.StartReasoningSelection(caller, extensionpb.SelectReasoningRequest_builder{
			Context: nil, ReasoningChoice: new(choice),
		}.Build())
		if startErr != nil {
			return nil, startErr
		}
		result, failure = operation.Wait(caller)
	}
	later, laterErr := binding.StartGetModels(caller)
	if laterErr == nil {
		_, laterErr = later.Wait(caller)
	}
	report := struct {
		Failure   string `json:"failure"`
		Code      string `json:"code"`
		Reasoning string `json:"reasoning"`
		Later     string `json:"later"`
	}{Failure: "", Code: "", Reasoning: result.GetSelection().GetReasoningChoice(), Later: ""}
	if failure != nil {
		report.Failure = failure.Error()
		if remote, present := errors.AsType[*extensionsdk.FailureError](failure); present {
			report.Code = remote.Code()
		}
	}
	if laterErr != nil {
		report.Later = laterErr.Error()
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(os.Getenv(signalsEnvironment))
	if err != nil {
		return nil, err
	}
	if writeErr := errors.Join(
		root.WriteFile(consumedSelectionReportFile, encoded, signalFileMode),
		root.Close(),
	); writeErr != nil {
		return nil, writeErr
	}
	return catalogueTextResult([]byte(consumedSelectionResultText)), nil
}
