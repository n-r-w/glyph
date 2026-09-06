package runtime

import (
	"errors"
	"fmt"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapTreeFrame maps tree query, navigation, and label completion frames.
func mapTreeFrame(frame controllerui.Frame) (*uiv1.HostCompleted, bool, error) {
	request := new(uiv1.HostCompleted)
	switch frame.Kind {
	case controllerui.FrameSessionTree:
		tree, present := frame.SessionTree.Get()
		if !present {
			return nil, true, errors.New("map UI tree frame: tree is required")
		}
		mapped, err := mapSessionTree(tree)
		if err != nil {
			return nil, true, err
		}
		result := new(uiv1.SessionTreeResult)
		result.SetTree(mapped)
		request.SetSessionTree(result)
		return request, true, nil
	case controllerui.FrameEntryLabelSet:
		tree, present := frame.SessionTree.Get()
		if !present {
			return nil, true, errors.New("map UI entry label frame: tree is required")
		}
		mapped, err := mapSessionTree(tree)
		if err != nil {
			return nil, true, err
		}
		result := new(uiv1.EntryLabelSet)
		result.SetTree(mapped)
		request.SetEntryLabelSet(result)
		return request, true, nil
	case controllerui.FrameSessionTreeNavigation:
		navigation, present := frame.TreeNavigation.Get()
		if !present {
			return nil, true, errors.New("map UI tree navigation: result is required")
		}
		mapped, err := mapTreeNavigation(navigation)
		if err != nil {
			return nil, true, err
		}
		request.SetSessionTreeNavigation(mapped)
		return request, true, nil
	case controllerui.FrameLifecycle, controllerui.FrameAuthorization,
		controllerui.FrameModelSelectionChanged,
		controllerui.FrameSessionList, controllerui.FrameSessionChanged, controllerui.FrameSessionInformation,
		controllerui.FrameSessionTreeNavigationProgress,
		controllerui.FrameSessionForked, controllerui.FrameSessionCloned, controllerui.FrameSubmitCompleted,
		controllerui.FrameAuthenticationCompleted:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// mapTreeNavigationProgress maps committed state for operation progress.
func mapTreeNavigationProgress(
	progress controllerui.TreeNavigationProgress,
) (*uiv1.SessionTreeNavigationProgress, error) {
	tree, err := mapSessionTree(progress.Tree)
	if err != nil {
		return nil, err
	}
	branch, err := mapRestoredSessionEntries(progress.ActiveBranch)
	if err != nil {
		return nil, fmt.Errorf("map UI tree navigation progress active branch: %w", err)
	}
	result := new(uiv1.SessionTreeNavigationProgress)
	result.SetTree(tree)
	result.SetActiveBranch(branch)
	return result, nil
}

// mapTreeNavigation maps terminal metadata or cancellation without speculative fields.
func mapTreeNavigation(navigation controllerui.TreeNavigationResult) (*uiv1.SessionTreeNavigationResult, error) {
	wire := new(uiv1.SessionTreeNavigationResult)
	switch navigation.Status {
	case controllerui.TreeNavigationStatusCommitted:
		committed, present := navigation.Committed.Get()
		if !present {
			return nil, errors.New("map UI tree navigation: committed state is absent")
		}
		wire.SetStatus(uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED)
		if destinationID, destinationPresent := committed.DestinationID.Get(); destinationPresent {
			wire.SetDestinationId(destinationID)
		}
		if activeLeafID, activeLeafPresent := committed.ActiveLeafID.Get(); activeLeafPresent {
			wire.SetActiveLeafId(activeLeafID)
		}
		if createdSummary, summaryPresent := committed.CreatedSummary.Get(); summaryPresent {
			mappedSummary, err := mapSessionTreeEntry(createdSummary)
			if err != nil {
				return nil, err
			}
			wire.SetCreatedSummary(mappedSummary)
		}
		if nextInput, nextInputPresent := committed.NextInput.Get(); nextInputPresent {
			wire.SetNextInput(nextInput)
		}
	case controllerui.TreeNavigationStatusCanceled:
		if navigation.Committed.IsSome() {
			return nil, errors.New("map UI tree navigation: canceled result contains committed state")
		}
		wire.SetStatus(uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED)
	case controllerui.TreeNavigationStatusUnspecified:
		return nil, errors.New("map UI tree navigation: status is unspecified")
	default:
		return nil, fmt.Errorf("map UI tree navigation: unknown status %d", navigation.Status)
	}
	wire.SetIssues(mapOperationIssues(navigation.Issues))
	return wire, nil
}

// mapOperationIssues maps safe ordered navigation issues to the UI contract.
func mapOperationIssues(issues []controllerui.OperationIssue) []*uiv1.OperationIssue {
	return lo.Map(issues, func(value controllerui.OperationIssue, _ int) *uiv1.OperationIssue {
		issue := new(uiv1.OperationIssue)
		issue.SetCode(uiv1.OperationIssueCode(value.Code))
		issue.SetExtensionId(value.ExtensionID)
		issue.SetHandlerId(value.HandlerID)
		issue.SetMessage(value.Message)
		return issue
	})
}

// mapSessionTree maps every tree entry in persistence order.
func mapSessionTree(tree controllerui.SessionTree) (*uiv1.SessionTree, error) {
	entries, err := lo.MapErr(
		tree.Entries,
		func(entry controllerui.SessionTreeEntry, index int) (*uiv1.SessionTreeEntry, error) {
			mapped, mapErr := mapSessionTreeEntry(entry)
			if mapErr != nil {
				return nil, fmt.Errorf("map UI tree entry %d: %w", index, mapErr)
			}
			return mapped, nil
		},
	)
	if err != nil {
		return nil, err
	}
	wire := new(uiv1.SessionTree)
	wire.SetEntries(entries)
	if activeLeafID, present := tree.ActiveLeafID.Get(); present {
		wire.SetActiveLeafId(activeLeafID)
	}
	return wire, nil
}

// mapSessionTreeEntry maps one closed public tree payload.
//
//nolint:gocyclo // The closed tree union requires one explicit mapping for each payload.
func mapSessionTreeEntry(entry controllerui.SessionTreeEntry) (*uiv1.SessionTreeEntry, error) {
	wire := new(uiv1.SessionTreeEntry)
	wire.SetId(entry.ID)
	if parentID, present := entry.ParentID.Get(); present {
		wire.SetParentId(parentID)
	}
	wire.SetCreatedTime(timestamppb.New(entry.CreatedAt))
	wire.SetLabel(entry.Label)
	switch entry.Kind {
	case controllerui.SessionTreeEntryUser, controllerui.SessionTreeEntryModel, controllerui.SessionTreeEntryToolResult:
		public := controllerui.SessionEntry{
			ID:               entry.ID,
			CreatedAt:        entry.CreatedAt,
			Kind:             controllerui.SessionEntryKind(entry.Kind),
			User:             entry.User,
			Model:            entry.Model,
			ToolResult:       entry.ToolResult,
			BranchSummary:    mo.None[controllerui.BranchSummary](),
			ExtensionMessage: mo.None[controllerui.ExtensionMessage](),
		}
		mapped, err := mapRestoredSessionEntries([]controllerui.SessionEntry{public})
		if err != nil {
			return nil, err
		}
		switch entry.Kind {
		case controllerui.SessionTreeEntryUser:
			wire.SetUser(mapped[0].GetUser())
		case controllerui.SessionTreeEntryModel:
			wire.SetModel(mapped[0].GetModel())
		case controllerui.SessionTreeEntryToolResult:
			wire.SetToolResult(mapped[0].GetToolResult())
		case controllerui.SessionTreeEntryUnspecified, controllerui.SessionTreeEntryExtension,
			controllerui.SessionTreeEntryBranchSummary, controllerui.SessionTreeEntryExtensionMessage:
			return nil, errors.New("tree entry kind cannot use transcript mapping")
		default:
			return nil, fmt.Errorf("unknown transcript tree entry kind %d", entry.Kind)
		}
	case controllerui.SessionTreeEntryExtension:
		extension, present := entry.Extension.Get()
		if !present {
			return nil, errors.New("extension metadata is absent")
		}
		mapped := new(uiv1.ExtensionEntry)
		mapped.SetExtensionId(extension.ExtensionID)
		mapped.SetEntryType(extension.EntryType)
		wire.SetExtension(mapped)
	case controllerui.SessionTreeEntryExtensionMessage:
		message, present := entry.ExtensionMessage.Get()
		if !present {
			return nil, errors.New("extension message is absent")
		}
		wire.SetExtensionMessage(mapExtensionMessage(message))
	case controllerui.SessionTreeEntryBranchSummary:
		summary, present := entry.BranchSummary.Get()
		if !present {
			return nil, errors.New("branch summary is absent")
		}
		mapped, err := mapBranchSummary(summary)
		if err != nil {
			return nil, err
		}
		wire.SetBranchSummary(mapped)
	case controllerui.SessionTreeEntryUnspecified:
		return nil, errors.New("tree entry kind is unspecified")
	default:
		return nil, fmt.Errorf("unknown tree entry kind %d", entry.Kind)
	}
	return wire, nil
}

// mapExtensionMessage maps exact message content and client visibility.
func mapExtensionMessage(message controllerui.ExtensionMessage) *uiv1.ExtensionMessage {
	visibility := uiv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE
	if message.Visibility == session.ClientVisibilityHidden {
		visibility = uiv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN
	}
	return uiv1.ExtensionMessage_builder{
		ExtensionId: new(message.ExtensionID), EntryType: new(message.EntryType),
		Text: new(message.Text), Visibility: new(visibility),
	}.Build()
}

// mapBranchSummary maps one persisted summary and optional accounting.
func mapBranchSummary(summary controllerui.BranchSummary) (*uiv1.BranchSummary, error) {
	if err := summary.Source.Validate(); err != nil {
		return nil, err
	}
	wire := new(uiv1.BranchSummary)
	wire.SetSummary(summary.Summary)
	wire.SetFirstEntryId(summary.FirstEntryID)
	wire.SetLastEntryId(summary.LastEntryID)
	// Only a model source enters reasoning and token conversion.
	source := new(uiv1.BranchSummarySource)
	if extensionID, present := summary.Source.ExtensionID.Get(); present {
		source.SetExtensionId(extensionID)
	} else if modelSource, modelPresent := summary.Source.Model.Get(); modelPresent {
		reasoning, err := mapModelReasoningChoice(modelSource.Selection.ReasoningChoice)
		if err != nil {
			return nil, err
		}
		// Keep actual model identity and its usage in the same wire alternative.
		mappedModel := new(uiv1.BranchSummaryModelSource)
		mappedModel.SetProviderId(string(modelSource.Selection.Provider))
		mappedModel.SetModelId(string(modelSource.Selection.Model))
		mappedModel.SetReasoningChoice(reasoning)
		if usage, reported := modelSource.Usage.Get(); reported {
			mapped := new(uiv1.TokenUsage)
			mapped.SetInputTokens(usage.InputTokens)
			mapped.SetOutputTokens(usage.OutputTokens)
			mapped.SetCacheReadTokens(usage.CacheReadTokens)
			mapped.SetCacheWriteTokens(usage.CacheWriteTokens)
			mapped.SetReasoningTokens(usage.ReasoningTokens)
			mapped.SetTotalTokens(usage.TotalTokens)
			mappedModel.SetUsage(mapped)
		}
		source.SetModel(mappedModel)
	}
	wire.SetSource(source)
	if cost, present := summary.EstimatedCost.Get(); present {
		wire.SetEstimatedCost(mapEstimatedCost(cost))
	}
	return wire, nil
}

// mapModelReasoningChoice maps a stored model reasoning choice.
func mapModelReasoningChoice(choice model.ReasoningChoice) (uiv1.ReasoningChoice, error) {
	switch choice {
	case model.ReasoningChoiceOff:
		return uiv1.ReasoningChoice_REASONING_CHOICE_OFF, nil
	case model.ReasoningChoiceOn:
		return uiv1.ReasoningChoice_REASONING_CHOICE_ON, nil
	case model.ReasoningChoiceMinimal:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL, nil
	case model.ReasoningChoiceLow:
		return uiv1.ReasoningChoice_REASONING_CHOICE_LOW, nil
	case model.ReasoningChoiceMedium:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM, nil
	case model.ReasoningChoiceHigh:
		return uiv1.ReasoningChoice_REASONING_CHOICE_HIGH, nil
	case model.ReasoningChoiceXHigh:
		return uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH, nil
	case model.ReasoningChoiceMax:
		return uiv1.ReasoningChoice_REASONING_CHOICE_MAX, nil
	default:
		return 0, fmt.Errorf("unknown summary reasoning choice %q", choice)
	}
}
