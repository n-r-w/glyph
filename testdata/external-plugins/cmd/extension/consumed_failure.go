package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// consumedConfiguredMode selects a consumed configured-model extension failure.
	consumedConfiguredMode = "consumed-configured-failure"
	// consumedConfiguredReportFile records both child outcomes before the plugin returns success.
	consumedConfiguredReportFile = "consumed-configured-failure.json"
	// consumedConfiguredResultText is the plugin's attempted successful parent result.
	consumedConfiguredResultText = "consumed configured failure"
)

// requestConsumedConfiguredFailure catches a real retry-handler failure and attempts further Host work.
func requestConsumedConfiguredFailure(ctx context.Context, signals string) (*extensionpb.ToolResult, error) {
	binding, err := extensionsdk.ContextFrom(ctx)
	if err != nil {
		return nil, err
	}
	caller := context.WithoutCancel(ctx)
	operation, err := startConfiguredModel(caller, configuredInstructions, configuredDisabledReasoningChoice)
	if err != nil {
		return nil, err
	}
	_, childErr := operation.Wait(caller)
	if childErr == nil {
		return nil, errors.New("configured failure unexpectedly succeeded")
	}
	later, laterErr := binding.StartGetModels(caller)
	if laterErr == nil {
		_, laterErr = later.Wait(caller)
	}
	report := struct {
		Failure string `json:"failure"`
		Later   string `json:"later"`
	}{Failure: childErr.Error(), Later: ""}
	if laterErr != nil {
		report.Later = laterErr.Error()
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	if writeErr := os.WriteFile(
		filepath.Join(signals, consumedConfiguredReportFile),
		encoded,
		signalFileMode,
	); writeErr != nil {
		return nil, writeErr
	}
	return catalogueTextResult([]byte(consumedConfiguredResultText)), nil
}
