package runtime

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapTreeFrame maps tree query, navigation, and label completion frames.
func mapTreeFrame(frame domainui.Frame) (*uiv1.HostCompleted, bool, error) {
	request := new(uiv1.HostCompleted)
	switch frame.Kind {
	case domainui.FrameSessionTree:
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
	case domainui.FrameEntryLabelSet:
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
	case domainui.FrameSessionTreeNavigation:
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
	case domainui.FrameInitialization, domainui.FrameLifecycle, domainui.FrameAuthorization,
		domainui.FrameInformation, domainui.FrameError, domainui.FrameModelSelectionChanged,
		domainui.FrameSessionList, domainui.FrameSessionChanged, domainui.FrameSessionInformation,
		domainui.FrameSessionForked, domainui.FrameSessionCloned, domainui.FrameSubmitCompleted,
		domainui.FrameAuthenticationCompleted:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// mapTreeNavigation maps committed state or cancellation without speculative fields.
func mapTreeNavigation(navigation domainui.TreeNavigationResult) (*uiv1.SessionTreeNavigationResult, error) {
	wire := new(uiv1.SessionTreeNavigationResult)
	switch navigation.Status {
	case domainui.TreeNavigationStatusCommitted:
		committed, present := navigation.Committed.Get()
		if !present {
			return nil, errors.New("map UI tree navigation: committed state is absent")
		}
		tree, err := mapSessionTree(committed.Tree)
		if err != nil {
			return nil, err
		}
		branch, err := mapRestoredSessionEntries(committed.ActiveBranch)
		if err != nil {
			return nil, fmt.Errorf("map UI tree navigation active branch: %w", err)
		}
		wire.SetStatus(uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED)
		wire.SetTree(tree)
		wire.SetActiveBranch(branch)
		if nextInput, nextInputPresent := committed.NextInput.Get(); nextInputPresent {
			wire.SetNextInput(nextInput)
		}
	case domainui.TreeNavigationStatusCanceled:
		if navigation.Committed.IsSome() {
			return nil, errors.New("map UI tree navigation: canceled result contains committed state")
		}
		wire.SetStatus(uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED)
	case domainui.TreeNavigationStatusUnspecified:
		return nil, errors.New("map UI tree navigation: status is unspecified")
	default:
		return nil, fmt.Errorf("map UI tree navigation: unknown status %d", navigation.Status)
	}
	wire.SetIssues(mapOperationIssues(navigation.Issues))
	return wire, nil
}

// mapOperationIssues maps safe ordered navigation issues to the UI contract.
func mapOperationIssues(issues []domainui.OperationIssue) []*uiv1.OperationIssue {
	return lo.Map(issues, func(value domainui.OperationIssue, _ int) *uiv1.OperationIssue {
		issue := new(uiv1.OperationIssue)
		issue.SetCode(uiv1.OperationIssueCode(value.Code))
		issue.SetExtensionId(value.ExtensionID)
		issue.SetHandlerId(value.HandlerID)
		issue.SetMessage(value.Message)
		return issue
	})
}

// mapSessionTree maps every tree entry in persistence order.
func mapSessionTree(tree domainui.SessionTree) (*uiv1.SessionTree, error) {
	entries, err := lo.MapErr(
		tree.Entries,
		func(entry domainui.SessionTreeEntry, index int) (*uiv1.SessionTreeEntry, error) {
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
func mapSessionTreeEntry(entry domainui.SessionTreeEntry) (*uiv1.SessionTreeEntry, error) {
	wire := new(uiv1.SessionTreeEntry)
	wire.SetId(entry.ID)
	if parentID, present := entry.ParentID.Get(); present {
		wire.SetParentId(parentID)
	}
	wire.SetCreatedTime(timestamppb.New(entry.CreatedAt))
	wire.SetLabel(entry.Label)
	switch entry.Kind {
	case domainui.SessionTreeEntryUser, domainui.SessionTreeEntryModel, domainui.SessionTreeEntryToolResult:
		public := domainui.SessionEntry{
			ID: entry.ID, CreatedAt: entry.CreatedAt, Kind: domainui.SessionEntryKind(entry.Kind),
			User: entry.User, Model: entry.Model, ToolResult: entry.ToolResult,
			BranchSummary: mo.None[domainui.BranchSummary](),
		}
		mapped, err := mapRestoredSessionEntries([]domainui.SessionEntry{public})
		if err != nil {
			return nil, err
		}
		switch entry.Kind {
		case domainui.SessionTreeEntryUser:
			wire.SetUser(mapped[0].GetUser())
		case domainui.SessionTreeEntryModel:
			wire.SetModel(mapped[0].GetModel())
		case domainui.SessionTreeEntryToolResult:
			wire.SetToolResult(mapped[0].GetToolResult())
		case domainui.SessionTreeEntryUnspecified, domainui.SessionTreeEntryExtension,
			domainui.SessionTreeEntryBranchSummary:
			return nil, errors.New("tree entry kind cannot use transcript mapping")
		default:
			return nil, fmt.Errorf("unknown transcript tree entry kind %d", entry.Kind)
		}
	case domainui.SessionTreeEntryExtension:
		extension, present := entry.Extension.Get()
		if !present {
			return nil, errors.New("extension metadata is absent")
		}
		mapped := new(uiv1.ExtensionEntry)
		mapped.SetExtensionId(extension.ExtensionID)
		mapped.SetEntryType(extension.EntryType)
		wire.SetExtension(mapped)
	case domainui.SessionTreeEntryBranchSummary:
		summary, present := entry.BranchSummary.Get()
		if !present {
			return nil, errors.New("branch summary is absent")
		}
		mapped, err := mapBranchSummary(summary)
		if err != nil {
			return nil, err
		}
		wire.SetBranchSummary(mapped)
	case domainui.SessionTreeEntryUnspecified:
		return nil, errors.New("tree entry kind is unspecified")
	default:
		return nil, fmt.Errorf("unknown tree entry kind %d", entry.Kind)
	}
	return wire, nil
}

// mapBranchSummary maps one persisted summary and optional accounting.
func mapBranchSummary(summary domainui.BranchSummary) (*uiv1.BranchSummary, error) {
	reasoning, err := mapModelReasoningChoice(summary.ReasoningChoice)
	if err != nil {
		return nil, err
	}
	wire := new(uiv1.BranchSummary)
	wire.SetSummary(summary.Summary)
	wire.SetFirstEntryId(summary.FirstEntryID)
	wire.SetLastEntryId(summary.LastEntryID)
	wire.SetProviderId(string(summary.Provider))
	wire.SetModelId(string(summary.Model))
	wire.SetReasoningChoice(reasoning)
	if usage, present := summary.Usage.Get(); present {
		mapped := new(uiv1.TokenUsage)
		mapped.SetInputTokens(usage.InputTokens)
		mapped.SetOutputTokens(usage.OutputTokens)
		mapped.SetCacheReadTokens(usage.CacheReadTokens)
		mapped.SetCacheWriteTokens(usage.CacheWriteTokens)
		mapped.SetReasoningTokens(usage.ReasoningTokens)
		mapped.SetTotalTokens(usage.TotalTokens)
		wire.SetUsage(mapped)
	}
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
