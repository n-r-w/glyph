package programmatic

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/timestamppb"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// mapSessionTreeCommandResponse maps one complete tree query result.
func mapSessionTreeCommandResponse(wire *programmaticv1.CommandResponse, tree SessionTree) error {
	mapped, err := mapSessionTree(tree)
	if err != nil {
		return err
	}
	result := new(programmaticv1.SessionTreeResult)
	result.SetTree(mapped)
	wire.SetSessionTree(result)
	return nil
}

// mapTreeNavigationCommandResponse maps a committed or canceled navigation result.
func mapTreeNavigationCommandResponse(wire *programmaticv1.CommandResponse, navigation TreeNavigationResult) error {
	result := new(programmaticv1.SessionTreeNavigationResult)
	switch navigation.Status {
	case TreeNavigationStatusCommitted:
		committed, present := navigation.Committed.Get()
		if !present {
			return errors.New("map tree navigation: committed state is absent")
		}
		tree, err := mapSessionTree(committed.Tree)
		if err != nil {
			return err
		}
		branch, err := mapSessionEntries(committed.ActiveBranch)
		if err != nil {
			return fmt.Errorf("map tree navigation active branch: %w", err)
		}
		result.SetStatus(programmaticv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED)
		result.SetTree(tree)
		result.SetActiveBranch(branch)
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
			mapped, mapErr := mapSessionTreeEntry(entry)
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

// mapSessionTreeEntry maps one closed tree entry payload.
//
//nolint:gocyclo // The switch maps every closed tree entry kind.
func mapSessionTreeEntry(entry SessionTreeEntry) (*programmaticv1.SessionTreeEntry, error) {
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

// mapBranchSummary maps one persisted summary and its optional accounting.
func mapBranchSummary(summary BranchSummary) (*programmaticv1.BranchSummary, error) {
	reasoning, err := mapReasoningChoice(summary.ReasoningChoice)
	if err != nil {
		return nil, err
	}
	wire := new(programmaticv1.BranchSummary)
	wire.SetSummary(summary.Summary)
	wire.SetFirstEntryId(summary.FirstEntryID)
	wire.SetLastEntryId(summary.LastEntryID)
	wire.SetProviderId(string(summary.Provider))
	wire.SetModelId(string(summary.Model))
	wire.SetReasoningChoice(reasoning)
	if usage, present := summary.Usage.Get(); present {
		mapped := new(programmaticv1.TokenUsage)
		setCommonUsage(
			mapped, usage.InputTokens, usage.OutputTokens, usage.CacheWriteTokens,
			usage.ReasoningTokens, usage.TotalTokens,
		)
		mapped.SetCacheReadTokens(usage.CacheReadTokens)
		wire.SetUsage(mapped)
	}
	if cost, present := summary.EstimatedCost.Get(); present {
		wire.SetEstimatedCost(mapEstimatedCost(cost))
	}
	return wire, nil
}
