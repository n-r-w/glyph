// Package extension maps extension-initiated requests to session-bound Host operations.
package extension

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// contextReadAction owns a selected action that produces no progress.
type contextReadAction func(context.Context) (*extensionpb.HostCompleted, error)

// contextReadAction implements prepared request execution.
var _ contextAction = contextReadAction(nil)

// Run executes the selected non-progress action.
func (action contextReadAction) Run(
	ctx context.Context,
	_ *extensionsdk.HostProgressReporter,
) (*extensionpb.HostCompleted, error) {
	return action(ctx)
}

// contextProgressAction owns a selected action that can report progress.
type contextProgressAction func(context.Context, *extensionsdk.HostProgressReporter) (*extensionpb.HostCompleted, error)

// contextProgressAction implements prepared execution with progress access.
var _ contextAction = contextProgressAction(nil)

// Run executes the selected progress-producing action.
func (action contextProgressAction) Run(
	ctx context.Context,
	reporter *extensionsdk.HostProgressReporter,
) (*extensionpb.HostCompleted, error) {
	return action(ctx, reporter)
}

// prepareSelectionAction binds selection admission and its release owner.
func (o *contextOperation) prepareSelectionAction(command SelectionCommand) (contextAction, error) {
	if o.service.selection == nil {
		return nil, extensionsdk.Reject(internalFailureCode, errors.New("model selection is not bound"))
	}
	command.ExtensionID = o.service.extensionID
	command.RuntimeID = o.service.runtimeID
	command.Context = o.reference
	prepared, err := o.service.selection.PrepareExtensionSelection(command)
	if err != nil {
		return nil, mapSelectionRejection(err)
	}
	o.selectionRelease = mo.Some(prepared.Release)
	return contextReadAction(
		func(ctx context.Context) (*extensionpb.HostCompleted, error) { return o.runSelection(ctx, prepared) },
	), nil
}

// contextAdmission identifies the required reservation for a mapped request.
type contextAdmission uint8

const (
	// contextAdmissionRegular uses the ordinary runtime reservation.
	contextAdmissionRegular contextAdmission = iota
	// contextAdmissionManual also reserves the manual operation gate.
	contextAdmissionManual
	// contextAdmissionCandidate requires an admitted parent lifetime.
	contextAdmissionCandidate
)

// prepareContextAction binds selected payload data after common admission.
type prepareContextAction func(*contextOperation) (contextAction, error)

// runManualCompaction reports manual compaction progress and returns its public completion.
func (o *contextOperation) runManualCompaction(
	ctx context.Context,
	reporter *extensionsdk.HostProgressReporter,
	instructions mo.Option[string],
) (*extensionpb.HostCompleted, error) {
	result := new(extensionpb.HostCompleted)
	progress := new(extensionpb.HostProgress)
	progress.SetCompaction(extensionpb.CompactionProgress_builder{Stage: new(compactionRunningStage)}.Build())
	if err := reporter.Report(ctx, progress); err != nil {
		return nil, extensionsdk.Fail(internalFailureCode, fmt.Errorf("report compaction progress: %w", err))
	}
	compacted, compactionErr := o.service.compaction.CompactExtension(
		ctx, o.service.extensionID, o.service.runtimeID, o.reference, instructions,
	)
	mapped, err := mapCompactionCompletion(ctx, compacted, compactionErr)
	if mapped == nil {
		return nil, err
	}
	result.SetCompact(mapped)

	return result, err
}

// runSelection returns the public selection completion.
func (o *contextOperation) runSelection(
	ctx context.Context,
	selection PreparedSelection,
) (*extensionpb.HostCompleted, error) {
	result := new(extensionpb.HostCompleted)
	selectionResult := selection.Run(ctx)
	mapped, err := mapSelectionResult(selectionResult)
	if mapped == nil {
		return nil, err
	}
	result.SetSelection(mapped)
	return result, err
}

// runAppendMessage returns the visible message receipt and delivery issues.
func (o *contextOperation) runAppendMessage(
	ctx context.Context,
	message appendMessageRequest,
) (*extensionpb.HostCompleted, error) {
	result := new(extensionpb.HostCompleted)
	appended, err := o.service.contexts.AppendExtensionMessage(
		ctx, o.service.extensionID, o.service.runtimeID, o.reference,
		message.entryType, message.text, message.visibility,
	)
	if err != nil {
		return nil, mapContextFailure("append extension message", err)
	}
	mapped, err := mapSessionEntry(appended.Entry)
	if err != nil {
		return nil, extensionsdk.Fail(internalFailureCode, err)
	}
	issues := make([]*extensionpb.AppendExtensionMessageIssue, 0, len(appended.Issues))
	for issueIndex := range appended.Issues {
		issue := &appended.Issues[issueIndex]
		if issue.Code != deliveryFailedIssueCode {
			return nil, extensionsdk.Fail(
				internalFailureCode,
				fmt.Errorf("unknown append message issue %q", issue.Code),
			)
		}
		issues = append(issues, extensionpb.AppendExtensionMessageIssue_builder{
			Code: new(
				extensionpb.AppendExtensionMessageIssueCode_APPEND_EXTENSION_MESSAGE_ISSUE_CODE_DELIVERY_FAILED,
			),
			Message: new(issue.Message), ExtensionId: new(issue.ExtensionID),
		}.Build())
	}
	result.SetAppendExtensionMessage(extensionpb.AppendExtensionMessageResult_builder{
		Entry: mapped, Issues: issues,
	}.Build())

	return result, nil
}

// runAppendValue returns the hidden entry receipt.
func (o *contextOperation) runAppendValue(
	ctx context.Context,
	value appendRequest,
) (*extensionpb.HostCompleted, error) {
	result := new(extensionpb.HostCompleted)
	entry, err := o.service.contexts.AppendExtension(
		ctx, o.service.extensionID, o.service.runtimeID, o.reference,
		value.entryType, value.data,
	)
	if err != nil {
		return nil, mapContextFailure("append extension entry", err)
	}
	mapped, err := mapSessionEntry(entry)
	if err != nil {
		return nil, extensionsdk.Fail(internalFailureCode, err)
	}
	result.SetAppendExtension(extensionpb.AppendExtensionResult_builder{Entry: mapped}.Build())

	return result, nil
}

// runSessionState returns the active branch snapshot.
func (o *contextOperation) runSessionState(ctx context.Context) (*extensionpb.HostCompleted, error) {
	result := new(extensionpb.HostCompleted)
	snapshot, err := o.service.contexts.ReadSessionState(
		ctx, o.service.extensionID, o.service.runtimeID, o.reference,
	)
	if err != nil {
		return nil, mapContextFailure("read extension session state", err)
	}
	mapped, err := mapSessionState(snapshot)
	if err != nil {
		return nil, extensionsdk.Fail(internalFailureCode, err)
	}
	result.SetGetSessionState(mapped)

	return result, nil
}

// runConfiguredModel returns the configured model completion.
func (o *contextOperation) runConfiguredModel(
	ctx context.Context,
	reporter *extensionsdk.HostProgressReporter,
	configured configuredRequest,
) (*extensionpb.HostCompleted, error) {
	result := new(extensionpb.HostCompleted)
	response, err := o.service.models.Request(
		ctx, o.service.extensionID, o.service.runtimeID, o.reference,
		configured.selection, configured.instructions, configured.history,
		func(progress ConfiguredRetryProgress) error {
			value := extensionpb.ConfiguredModelRetryProgress_builder{
				CompletedAttempts: new(progress.CompletedAttempts), AttemptLimit: new(progress.AttemptLimit),
				DelayMilliseconds: new(progress.Delay.Milliseconds()), Error: new(progress.Error),
			}.Build()
			frame := new(extensionpb.HostProgress)
			frame.SetConfiguredModelRetry(value)
			return reporter.Report(ctx, frame)
		},
	)
	if err != nil {
		return nil, mapModelFailure("request configured model", err)
	}
	mapped, err := MapModelResponse(response)
	if err != nil {
		return nil, extensionsdk.Fail(internalFailureCode, err)
	}
	result.SetConfiguredModel(mapped)

	return result, nil
}

// runModelCatalog returns the model catalog.
func (o *contextOperation) runModelCatalog(ctx context.Context) (*extensionpb.HostCompleted, error) {
	result := new(extensionpb.HostCompleted)
	catalog, err := o.service.models.ReadModels(ctx, o.service.extensionID, o.service.runtimeID, o.reference)
	if err != nil {
		return nil, mapModelFailure("read model catalog", err)
	}
	result.SetGetModels(mapModelCatalog(catalog))

	return result, nil
}

// runProviderCatalog returns the provider catalog.
func (o *contextOperation) runProviderCatalog(ctx context.Context) (*extensionpb.HostCompleted, error) {
	result := new(extensionpb.HostCompleted)
	providers, err := o.service.models.ReadProviders(ctx, o.service.extensionID, o.service.runtimeID, o.reference)
	if err != nil {
		return nil, mapModelFailure("read provider catalog", err)
	}
	result.SetGetProviders(mapProviderCatalog(providers))
	return result, nil
}
