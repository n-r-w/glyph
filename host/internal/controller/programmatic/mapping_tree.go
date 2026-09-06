package programmatic

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// mapSessionTreeCompleted maps one complete tree query result.
func mapSessionTreeCompleted(wire *programmaticv1.HostCompleted, tree SessionTree) error {
	mapped, err := mapSessionTree(tree)
	if err != nil {
		return err
	}
	result := new(programmaticv1.SessionTreeResult)
	result.SetTree(mapped)
	wire.SetSessionTree(result)
	return nil
}

// mapTreeNavigationProgress maps committed state to typed operation progress.
func mapTreeNavigationProgress(
	progress TreeNavigationProgress,
) (*programmaticv1.SessionTreeNavigationProgress, error) {
	tree, err := mapSessionTree(progress.Tree)
	if err != nil {
		return nil, err
	}
	activeBranch, err := mapSessionEntries(progress.ActiveBranch)
	if err != nil {
		return nil, fmt.Errorf("map navigation progress active branch: %w", err)
	}
	result := new(programmaticv1.SessionTreeNavigationProgress)
	result.SetTree(tree)
	result.SetActiveBranch(activeBranch)
	return result, nil
}

// mapTreeNavigationCompleted maps committed metadata or a canceled result.
func mapTreeNavigationCompleted(wire *programmaticv1.HostCompleted, navigation TreeNavigationResult) error {
	result := new(programmaticv1.SessionTreeNavigationResult)
	switch navigation.Status {
	case TreeNavigationStatusCommitted:
		committed, present := navigation.Committed.Get()
		if !present {
			return errors.New("map tree navigation: committed state is absent")
		}
		result.SetStatus(programmaticv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED)
		if destinationID, destinationPresent := committed.DestinationID.Get(); destinationPresent {
			result.SetDestinationId(destinationID)
		}
		if activeLeafID, activeLeafPresent := committed.ActiveLeafID.Get(); activeLeafPresent {
			result.SetActiveLeafId(activeLeafID)
		}
		if createdSummary, summaryPresent := committed.CreatedSummary.Get(); summaryPresent {
			mappedSummary, err := EncodeSessionTreeEntry(createdSummary)
			if err != nil {
				return err
			}
			result.SetCreatedSummary(mappedSummary)
		}
		if nextInput, nextInputPresent := committed.NextInput.Get(); nextInputPresent {
			result.SetNextInput(nextInput)
		}
	case TreeNavigationStatusCanceled:
		if navigation.Committed.IsSome() {
			return errors.New("map tree navigation: canceled result contains committed state")
		}
		result.SetStatus(programmaticv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED)
	case TreeNavigationStatusUnspecified:
		return errors.New("map tree navigation: status is unspecified")
	default:
		return fmt.Errorf("map tree navigation: unknown status %d", navigation.Status)
	}
	result.SetIssues(mapOperationIssues(navigation.Issues))
	wire.SetSessionTreeNavigation(result)
	return nil
}

// mapOperationIssues maps safe ordered navigation issues to Programmatic Control.
func mapOperationIssues(issues []OperationIssue) []*programmaticv1.OperationIssue {
	return lo.Map(issues, func(value OperationIssue, _ int) *programmaticv1.OperationIssue {
		issue := new(programmaticv1.OperationIssue)
		issue.SetCode(programmaticv1.OperationIssueCode(value.Code))
		issue.SetExtensionId(value.ExtensionID)
		issue.SetHandlerId(value.HandlerID)
		issue.SetMessage(value.Message)
		return issue
	})
}

// mapSessionTree maps every tree entry in persistence order.
func mapSessionTree(tree SessionTree) (*programmaticv1.SessionTree, error) {
	entries, err := lo.MapErr(
		tree.Entries,
		func(entry SessionTreeEntry, index int) (*programmaticv1.SessionTreeEntry, error) {
			mapped, mapErr := EncodeSessionTreeEntry(entry)
			if mapErr != nil {
				return nil, fmt.Errorf("map session tree entry %d: %w", index, mapErr)
			}
			return mapped, nil
		},
	)
	if err != nil {
		return nil, err
	}
	wire := new(programmaticv1.SessionTree)
	wire.SetEntries(entries)
	if activeLeafID, present := tree.ActiveLeafID.Get(); present {
		wire.SetActiveLeafId(activeLeafID)
	}
	return wire, nil
}

// EncodeSessionTreeEntry maps one closed tree entry payload.
//
//nolint:gocyclo // The switch maps every closed tree entry kind.
func EncodeSessionTreeEntry(entry SessionTreeEntry) (*programmaticv1.SessionTreeEntry, error) {
	wire := new(programmaticv1.SessionTreeEntry)
	wire.SetId(entry.ID)
	if parentID, present := entry.ParentID.Get(); present {
		wire.SetParentId(parentID)
	}
	wire.SetCreatedTime(timestamppb.New(entry.CreatedAt))
	wire.SetLabel(entry.Label)
	switch entry.Kind {
	case SessionTreeEntryUser:
		user, present := entry.User.Get()
		if !present {
			return nil, errors.New("user payload is absent")
		}
		mapped, err := mapUserMessage(user)
		if err != nil {
			return nil, err
		}
		wire.SetUser(mapped)
	case SessionTreeEntryModel:
		response, present := entry.Model.Get()
		if !present {
			return nil, errors.New("model payload is absent")
		}
		mapped, err := mapModelResponse(response)
		if err != nil {
			return nil, err
		}
		wire.SetModel(mapped)
	case SessionTreeEntryToolResult:
		result, present := entry.ToolResult.Get()
		if !present {
			return nil, errors.New("tool-result payload is absent")
		}
		mapped, err := mapToolResult(result)
		if err != nil {
			return nil, err
		}
		wire.SetToolResult(mapped)
	case SessionTreeEntryExtension:
		extension, present := entry.Extension.Get()
		if !present {
			return nil, errors.New("extension payload is absent")
		}
		mapped := new(programmaticv1.ExtensionEntry)
		mapped.SetExtensionId(extension.ExtensionID)
		mapped.SetEntryType(extension.EntryType)
		wire.SetExtension(mapped)
	case SessionTreeEntryExtensionMessage:
		message, present := entry.ExtensionMessage.Get()
		if !present {
			return nil, errors.New("extension message payload is absent")
		}
		wire.SetExtensionMessage(mapExtensionMessage(message))
	case SessionTreeEntryBranchSummary:
		summary, present := entry.BranchSummary.Get()
		if !present {
			return nil, errors.New("branch-summary payload is absent")
		}
		mapped, err := mapBranchSummary(summary)
		if err != nil {
			return nil, err
		}
		wire.SetBranchSummary(mapped)
	case SessionTreeEntryUnspecified:
		return nil, errors.New("tree entry kind is unspecified")
	default:
		return nil, fmt.Errorf("unknown tree entry kind %d", entry.Kind)
	}
	return wire, nil
}

// mapExtensionMessage maps exact message content and client visibility.
func mapExtensionMessage(message ExtensionMessage) *programmaticv1.ExtensionMessage {
	visibility := programmaticv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE
	if message.Visibility == session.ClientVisibilityHidden {
		visibility = programmaticv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN
	}
	return programmaticv1.ExtensionMessage_builder{
		ExtensionId: new(message.ExtensionID), EntryType: new(message.EntryType),
		Text: new(message.Text), Visibility: new(visibility),
	}.Build()
}

// mapBranchSummary maps one persisted summary and its optional accounting.
func mapBranchSummary(summary BranchSummary) (*programmaticv1.BranchSummary, error) {
	if err := summary.Source.Validate(); err != nil {
		return nil, err
	}
	wire := new(programmaticv1.BranchSummary)
	wire.SetSummary(summary.Summary)
	wire.SetFirstEntryId(summary.FirstEntryID)
	wire.SetLastEntryId(summary.LastEntryID)
	// Only a model source enters reasoning and token conversion.
	source := new(programmaticv1.BranchSummarySource)
	if extensionID, present := summary.Source.ExtensionID.Get(); present {
		source.SetExtensionId(extensionID)
	} else if modelSource, modelPresent := summary.Source.Model.Get(); modelPresent {
		reasoning, err := mapReasoningChoice(modelSource.Selection.ReasoningChoice)
		if err != nil {
			return nil, err
		}
		// Keep actual model identity and its usage in the same wire alternative.
		mappedModel := new(programmaticv1.BranchSummaryModelSource)
		mappedModel.SetProviderId(string(modelSource.Selection.Provider))
		mappedModel.SetModelId(string(modelSource.Selection.Model))
		mappedModel.SetReasoningChoice(reasoning)
		if usage, reported := modelSource.Usage.Get(); reported {
			mapped := new(programmaticv1.TokenUsage)
			setCommonUsage(mapped, usage.InputTokens, usage.OutputTokens, usage.CacheWriteTokens,
				usage.ReasoningTokens, usage.TotalTokens)
			mapped.SetCacheReadTokens(usage.CacheReadTokens)
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
