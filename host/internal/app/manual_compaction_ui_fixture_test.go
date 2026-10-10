//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"strconv"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	// manualUIBehavior selects public manual-compaction fixture calls.
	manualUIBehavior = "manual-compaction"
	// manualGuidanceEnvironment supplies the policy scenario to the fixture process.
	manualGuidanceEnvironment = "GLYPH_PUBLIC_MANUAL_GUIDANCE"
	// manualNoOwnerGuidance selects an empty-history no-owner call.
	manualNoOwnerGuidance = "no-owner"
)

// manualPublicReport contains the terminal outcome observed by a public client.
type manualPublicReport struct {
	// Committed reports a durable replacement receipt.
	Committed bool `json:"committed"`
	// Summary contains the replacement content.
	Summary string `json:"summary"`
	// Error contains the complete public terminal cause.
	Error string `json:"error"`
	// Canceled reports explicit policy cancellation.
	Canceled bool `json:"canceled"`
	// Originals reports preserved original-prefix user entries.
	Originals int `json:"originals"`
	// Markers reports durable replacement entries.
	Markers int `json:"markers"`
}

// runManualUIFixture exercises public UI submit and manual-compaction operations.
func runManualUIFixture(ctx context.Context, host *uisdk.Host) (returnErr error) {
	// manualFixtureManualUiSubmit prefixes UI conversation submission identifiers.
	const manualFixtureManualUiSubmit = "manual-ui-submit-"

	// manualFixtureManualUiStoredTree identifies UI retrieval of the stored session tree.
	const manualFixtureManualUiStoredTree = "manual-ui-stored-tree"

	// manualFixtureManualUiNext identifies the next UI conversation request.
	const manualFixtureManualUiNext = "manual-ui-next"

	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	guidance := os.Getenv(manualGuidanceEnvironment)
	if guidance != manualNoOwnerGuidance {
		for index, text := range []string{manualOriginalText, manualRetainedText} {
			request := new(uipb.UIRequest)
			request.SetSubmit(uipb.SubmitCommand_builder{Text: new(text)}.Build())
			operationID := manualFixtureManualUiSubmit + strconv.Itoa(index)
			started, err := host.Start(ctx, operationID, request)
			if err != nil {
				return err
			}
			if _, err := waitUIOperation(ctx, host, operationID, started, nil); err != nil {
				return err
			}
		}
	}
	request := new(uipb.UIRequest)
	request.SetCompact(uipb.CompactCommand_builder{Instructions: new(guidance)}.Build())
	cancelContext, cancelSignal := context.WithCancelCause(ctx)
	defer cancelSignal(nil)
	finishCancel, err := startManualUICancellation(cancelContext, host)
	if err != nil {
		return err
	}
	cancellationCollected := false
	defer func() {
		if !cancellationCollected {
			returnErr = errors.Join(returnErr, finishCancel())
		}
	}()
	if os.Getenv(manualFixtureGlyphManualCancelFailure) == manualFixtureEnabled {
		cancelSignal(errors.New("original UI cancellation request cause"))
	}
	started, err := host.Start(ctx, manualFixtureManualUiCompact, request)
	if err != nil {
		return err
	}
	completed, terminalErr := waitUIOperation(ctx, host, manualFixtureManualUiCompact, started, nil)
	report := manualPublicReport{Committed: false, Summary: "", Error: "", Canceled: false, Originals: 0, Markers: 0}
	if terminalErr != nil {
		report.Error = terminalErr.Error()
	} else {
		outcome := completed.GetCompaction()
		report.Committed = outcome.GetCommitted() != nil
		report.Summary = outcome.GetCommitted().GetCompaction().GetSummary()
		report.Error = outcome.GetError()
		report.Canceled = outcome.GetCanceled()
	}
	if report.Committed {
		next := new(uipb.UIRequest)
		next.SetSubmit(uipb.SubmitCommand_builder{Text: new(manualNextText)}.Build())
		operation, err := host.Start(ctx, manualFixtureManualUiNext, next)
		if err != nil {
			return err
		}
		if _, err := waitUIOperation(ctx, host, manualFixtureManualUiNext, operation, nil); err != nil {
			return err
		}
	}
	treeRequest := new(uipb.UIRequest)
	treeRequest.SetGetSessionTree(new(uipb.GetSessionTreeCommand))
	treeOperation, err := host.Start(ctx, manualFixtureManualUiStoredTree, treeRequest)
	if err != nil {
		return err
	}
	treeCompleted, err := waitUIOperation(ctx, host, manualFixtureManualUiStoredTree, treeOperation, nil)
	if err != nil {
		return err
	}
	for _, entry := range treeCompleted.GetSessionTree().GetTree().GetEntries() {
		if entry.GetCompaction() != nil {
			report.Markers++
		}
		for _, content := range entry.GetUser().GetContent() {
			if content.GetText() == manualOriginalText {
				report.Originals++
			}
		}
	}
	cancellationErr := finishCancel()
	cancellationCollected = true
	if cancellationErr != nil {
		report.Error = errors.Join(errors.New(report.Error), cancellationErr).Error()
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if err := os.WriteFile(os.Getenv(appUITraceEnvironment), encoded, 0o600); err != nil {
		return err
	}
	return host.Close(ctx)
}
