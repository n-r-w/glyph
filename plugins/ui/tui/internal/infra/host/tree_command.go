package host

import (
	"errors"
	"fmt"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentation "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// mapTreeCommand maps one presentation tree command to the public UI contract.
func mapTreeCommand(command presentation.Command) (*uiv1.UIRequest, bool, error) {
	if command.Kind < presentation.CommandGetSessionTree ||
		command.Kind > presentation.CommandSetEntryLabel {
		return nil, false, nil
	}
	treeCommand, present := command.TreeCommand.Get()
	if !present {
		return nil, true, errors.New("UI tree command payload is missing")
	}
	switch command.Kind {
	case presentation.CommandGetSessionTree:
		//nolint:exhaustruct_v5 // The protobuf builder sets only the active GetSessionTree field.
		return uiv1.UIRequest_builder{GetSessionTree: &uiv1.GetSessionTreeCommand{}}.Build(), true, nil
	case presentation.CommandNavigateSessionTree:
		response, err := mapNavigateTreeResponse(treeCommand)
		return response, true, err
	case presentation.CommandForkSession:
		response, err := mapForkTreeResponse(treeCommand)
		return response, true, err
	case presentation.CommandCloneSession:
		//nolint:exhaustruct_v5 // The protobuf builder sets only the active CloneSession field.
		return uiv1.UIRequest_builder{CloneSession: &uiv1.CloneSessionCommand{}}.Build(), true, nil
	case presentation.CommandSetEntryLabel:
		response, err := mapEntryLabelResponse(treeCommand)
		return response, true, err
	case presentation.CommandUnspecified, presentation.CommandSubmit, presentation.CommandStop,
		presentation.CommandRetryAuthentication, presentation.CommandQuit,
		presentation.CommandSelectModel, presentation.CommandSelectReasoningChoice,
		presentation.CommandCreateSession, presentation.CommandListSessions,
		presentation.CommandResumeSession, presentation.CommandSetSessionName,
		presentation.CommandGetSessionInfo:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// mapNavigateTreeResponse maps a validated navigation payload.
func mapNavigateTreeResponse(command presentation.TreeCommand) (*uiv1.UIRequest, error) {
	targetID, present := command.TargetEntryID.Get()
	if !present || targetID == "" {
		return nil, errors.New("UI tree navigation target is missing")
	}
	summaryMode, err := mapSummaryMode(command.SummaryMode)
	if err != nil {
		return nil, err
	}
	builder := uiv1.NavigateSessionTreeCommand_builder{
		TargetEntryId: new(targetID), SummaryMode: new(summaryMode), CustomFocus: nil,
	}
	if customFocus, customPresent := command.CustomFocus.Get(); customPresent {
		builder.CustomFocus = new(customFocus)
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active NavigateSessionTree field.
	return uiv1.UIRequest_builder{NavigateSessionTree: builder.Build()}.Build(), nil
}

// mapForkTreeResponse maps a validated fork target.
func mapForkTreeResponse(command presentation.TreeCommand) (*uiv1.UIRequest, error) {
	targetID, present := command.TargetEntryID.Get()
	if !present || targetID == "" {
		return nil, errors.New("UI fork target is missing")
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active ForkSession field.
	return uiv1.UIRequest_builder{
		ForkSession: uiv1.ForkSessionCommand_builder{TargetEntryId: new(targetID)}.Build(),
	}.Build(), nil
}

// mapEntryLabelResponse maps a label set or clear payload.
func mapEntryLabelResponse(command presentation.TreeCommand) (*uiv1.UIRequest, error) {
	targetID, targetPresent := command.TargetEntryID.Get()
	label, labelPresent := command.Label.Get()
	if !targetPresent || targetID == "" || !labelPresent {
		return nil, errors.New("UI entry label command is incomplete")
	}
	//nolint:exhaustruct_v5 // The protobuf builder sets only the active SetEntryLabel field.
	return uiv1.UIRequest_builder{
		SetEntryLabel: uiv1.SetEntryLabelCommand_builder{TargetEntryId: new(targetID), Label: new(label)}.Build(),
	}.Build(), nil
}

// mapSummaryMode maps one closed presentation summary mode.
func mapSummaryMode(mode presentation.SummaryMode) (uiv1.SummaryMode, error) {
	switch mode {
	case presentation.SummaryModeNoSummary:
		return uiv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY, nil
	case presentation.SummaryModeSummarize:
		return uiv1.SummaryMode_SUMMARY_MODE_SUMMARIZE, nil
	case presentation.SummaryModeCustomFocus:
		return uiv1.SummaryMode_SUMMARY_MODE_SUMMARIZE_WITH_CUSTOM_PROMPT, nil
	case presentation.SummaryModeUnspecified:
		return uiv1.SummaryMode_SUMMARY_MODE_UNSPECIFIED, errors.New("UI summary mode is unspecified")
	default:
		return uiv1.SummaryMode_SUMMARY_MODE_UNSPECIFIED, fmt.Errorf("unknown UI summary mode %d", mode)
	}
}
