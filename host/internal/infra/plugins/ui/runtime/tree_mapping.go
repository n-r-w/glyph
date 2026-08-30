package runtime

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapTreeFrame maps tree query, navigation, and failure frames.
func mapTreeFrame(frame domainui.Frame) (*uipb.OpenRequest, bool, error) {
	request := new(uipb.OpenRequest)
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
		result := new(uipb.SessionTreeResult)
		result.SetTree(mapped)
		request.SetSessionTree(result)
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
	case domainui.FrameSessionTreeFailed:
		failure, present := frame.TreeFailure.Get()
		if !present {
			return nil, true, errors.New("map UI tree failure: result is required")
		}
		code, err := mapTreeFailureCode(failure.Code)
		if err != nil {
			return nil, true, err
		}
		mapped := new(uipb.SessionTreeFailed)
		mapped.SetCode(code)
		mapped.SetMessage(failure.Message)
		request.SetSessionTreeFailed(mapped)
		return request, true, nil
	case domainui.FrameInitialization, domainui.FrameLifecycle, domainui.FrameAuthorization,
		domainui.FrameInformation, domainui.FrameError, domainui.FrameModelSelectionChanged,
		domainui.FrameSessionList, domainui.FrameSessionChanged, domainui.FrameSessionInformation:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// mapTreeNavigation maps committed state or cancellation without speculative fields.
func mapTreeNavigation(navigation domainui.TreeNavigationResult) (*uipb.SessionTreeNavigationResult, error) {
	wire := new(uipb.SessionTreeNavigationResult)
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
		wire.SetStatus(uipb.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED)
		wire.SetTree(tree)
		wire.SetActiveBranch(branch)
		if nextInput, nextInputPresent := committed.NextInput.Get(); nextInputPresent {
			wire.SetNextInput(nextInput)
		}
	case domainui.TreeNavigationStatusCanceled:
		if navigation.Committed.IsSome() {
			return nil, errors.New("map UI tree navigation: canceled result contains committed state")
		}
		wire.SetStatus(uipb.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED)
	case domainui.TreeNavigationStatusUnspecified:
		return nil, errors.New("map UI tree navigation: status is unspecified")
	default:
		return nil, fmt.Errorf("map UI tree navigation: unknown status %d", navigation.Status)
	}
	return wire, nil
}

// mapSessionTree maps every tree entry in persistence order.
func mapSessionTree(tree domainui.SessionTree) (*uipb.SessionTree, error) {
	entries := make([]*uipb.SessionTreeEntry, 0, len(tree.Entries))
	for index := range tree.Entries {
		entry, err := mapSessionTreeEntry(tree.Entries[index])
		if err != nil {
			return nil, fmt.Errorf("map UI tree entry %d: %w", index, err)
		}
		entries = append(entries, entry)
	}
	wire := new(uipb.SessionTree)
	wire.SetEntries(entries)
	if activeLeafID, present := tree.ActiveLeafID.Get(); present {
		wire.SetActiveLeafId(activeLeafID)
	}
	return wire, nil
}

// mapSessionTreeEntry maps one closed public tree payload.
func mapSessionTreeEntry(entry domainui.SessionTreeEntry) (*uipb.SessionTreeEntry, error) {
	wire := new(uipb.SessionTreeEntry)
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
		mapped := new(uipb.ExtensionEntry)
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
func mapBranchSummary(summary domainui.BranchSummary) (*uipb.BranchSummary, error) {
	reasoning, err := mapModelReasoningChoice(summary.ReasoningChoice)
	if err != nil {
		return nil, err
	}
	wire := new(uipb.BranchSummary)
	wire.SetSummary(summary.Summary)
	wire.SetFirstEntryId(summary.FirstEntryID)
	wire.SetLastEntryId(summary.LastEntryID)
	wire.SetProviderId(string(summary.Provider))
	wire.SetModelId(string(summary.Model))
	wire.SetReasoningChoice(reasoning)
	if usage, present := summary.Usage.Get(); present {
		mapped := new(uipb.TokenUsage)
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
func mapModelReasoningChoice(choice model.ReasoningChoice) (uipb.ReasoningChoice, error) {
	switch choice {
	case model.ReasoningChoiceOff:
		return uipb.ReasoningChoice_REASONING_CHOICE_OFF, nil
	case model.ReasoningChoiceOn:
		return uipb.ReasoningChoice_REASONING_CHOICE_ON, nil
	case model.ReasoningChoiceMinimal:
		return uipb.ReasoningChoice_REASONING_CHOICE_MINIMAL, nil
	case model.ReasoningChoiceLow:
		return uipb.ReasoningChoice_REASONING_CHOICE_LOW, nil
	case model.ReasoningChoiceMedium:
		return uipb.ReasoningChoice_REASONING_CHOICE_MEDIUM, nil
	case model.ReasoningChoiceHigh:
		return uipb.ReasoningChoice_REASONING_CHOICE_HIGH, nil
	case model.ReasoningChoiceXHigh:
		return uipb.ReasoningChoice_REASONING_CHOICE_XHIGH, nil
	case model.ReasoningChoiceMax:
		return uipb.ReasoningChoice_REASONING_CHOICE_MAX, nil
	default:
		return 0, fmt.Errorf("unknown summary reasoning choice %q", choice)
	}
}

// mapTreeFailureCode maps current closed UI navigation failures.
func mapTreeFailureCode(code domainui.TreeFailureCode) (uipb.SessionTreeFailureCode, error) {
	switch code {
	case domainui.TreeFailureInvalidArgument:
		return uipb.SessionTreeFailureCode_SESSION_TREE_FAILURE_CODE_INVALID_ARGUMENT, nil
	case domainui.TreeFailureNotFound:
		return uipb.SessionTreeFailureCode_SESSION_TREE_FAILURE_CODE_NOT_FOUND, nil
	case domainui.TreeFailureBusy:
		return uipb.SessionTreeFailureCode_SESSION_TREE_FAILURE_CODE_BUSY, nil
	case domainui.TreeFailurePersistenceUnavailable:
		return uipb.SessionTreeFailureCode_SESSION_TREE_FAILURE_CODE_PERSISTENCE_UNAVAILABLE, nil
	case domainui.TreeFailureInternal:
		return uipb.SessionTreeFailureCode_SESSION_TREE_FAILURE_CODE_INTERNAL, nil
	case domainui.TreeFailureUnspecified:
		return 0, errors.New("tree failure code is unspecified")
	default:
		return 0, fmt.Errorf("unknown tree failure code %d", code)
	}
}
