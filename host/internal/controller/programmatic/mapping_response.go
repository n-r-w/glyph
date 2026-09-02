package programmatic

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// mapResponse maps one completed internal result to its typed public payload.
//
//nolint:gocyclo // The switch exhaustively maps the closed completed payload union.
func mapResponse(response Response) (*programmaticv1.HostCompleted, error) {
	wire := new(programmaticv1.HostCompleted)
	if handled, err := mapSessionResponse(wire, response); handled {
		return wire, err
	}
	switch response.Kind {
	case ResponseUserRequestCompleted:
		wire.SetUserRequest(new(programmaticv1.UserRequestCompleted))
	case ResponseCancelCompleted:
		state, present := response.CancelTargetState.Get()
		if !present {
			return nil, errors.New("map cancellation: target state is absent")
		}
		completed := new(operationv1.CancelCompleted)
		switch state {
		case operation.TerminalStateCompleted:
			completed.SetTargetState(operationv1.TerminalState_TERMINAL_STATE_COMPLETED)
		case operation.TerminalStateCanceled:
			completed.SetTargetState(operationv1.TerminalState_TERMINAL_STATE_CANCELED)
		case operation.TerminalStateFailed:
			completed.SetTargetState(operationv1.TerminalState_TERMINAL_STATE_FAILED)
		default:
			return nil, fmt.Errorf("map cancellation: unknown target state %d", state)
		}
		wire.SetCancel(completed)
	case ResponseRunState:
		if err := mapRunStateCompleted(wire, response.State); err != nil {
			return nil, err
		}
	case ResponseMessages:
		entries, err := mapHistoryEntries(response.Messages)
		if err != nil {
			return nil, err
		}
		result := new(programmaticv1.MessagesResult)
		result.SetEntries(entries)
		wire.SetMessages(result)
	case ResponseModels:
		if err := mapModelsCompleted(wire, response.Models); err != nil {
			return nil, err
		}
	case ResponseModelSelection:
		if err := mapModelSelectionCompleted(wire, response.Selection); err != nil {
			return nil, err
		}
	case ResponseSessionInfo, ResponseSessions, ResponseSessionEntries, ResponseSessionStats,
		ResponseSessionTree, ResponseSessionTreeNavigation, ResponseForkSession, ResponseCloneSession,
		ResponseSetEntryLabel:
		return nil, errors.New("map completed response: handled response was not mapped")
	case ResponseRejected:
		return nil, errors.New("map completed response: rejection reached Host mapping")
	case ResponseUnspecified:
		return nil, errors.New("map completed response: unspecified response kind")
	default:
		return nil, fmt.Errorf("map completed response: unknown response kind %d", response.Kind)
	}
	return wire, nil
}

// mapSessionResponse isolates session payload mapping from the core response dispatch.
//
//nolint:gocyclo // The switch maps every closed session response kind explicitly.
func mapSessionResponse(wire *programmaticv1.HostCompleted, response Response) (bool, error) {
	switch response.Kind {
	case ResponseSessionInfo:
		info, present := response.SessionInfo.Get()
		if !present {
			return true, errors.New("map session information: result is absent")
		}
		result := new(programmaticv1.SessionInfoResult)
		result.SetInfo(mapSessionInfo(info))
		wire.SetSessionInfo(result)
		return true, nil
	case ResponseSessions:
		result := new(programmaticv1.SessionsResult)
		result.SetSessions(
			lo.Map(response.Sessions, func(summary session.Summary, _ int) *programmaticv1.SessionSummary {
				return mapSessionSummary(summary)
			}),
		)
		wire.SetSessions(result)
		return true, nil
	case ResponseSessionEntries:
		entries, err := mapSessionEntries(response.SessionEntries)
		if err != nil {
			return true, err
		}
		result := new(programmaticv1.SessionEntriesResult)
		result.SetEntries(entries)
		wire.SetSessionEntries(result)
		return true, nil
	case ResponseSessionStats:
		statistics, present := response.SessionStatistics.Get()
		if !present {
			return true, errors.New("map session statistics: result is absent")
		}
		result := new(programmaticv1.SessionStatsResult)
		result.SetStatistics(mapSessionStatistics(statistics))
		wire.SetSessionStats(result)
		return true, nil
	case ResponseSessionTree:
		tree, present := response.SessionTree.Get()
		if !present {
			return true, errors.New("map session tree: result is absent")
		}
		return true, mapSessionTreeCompleted(wire, tree)
	case ResponseSessionTreeNavigation:
		navigation, present := response.TreeNavigation.Get()
		if !present {
			return true, errors.New("map tree navigation: result is absent")
		}
		return true, mapTreeNavigationCompleted(wire, navigation)
	case ResponseForkSession:
		return true, mapForkSessionCompleted(wire, response)
	case ResponseCloneSession:
		return true, mapCloneSessionCompleted(wire, response)
	case ResponseSetEntryLabel:
		tree, present := response.SessionTree.Get()
		if !present {
			return true, errors.New("map entry label: committed tree is absent")
		}
		mapped, err := mapSessionTree(tree)
		if err != nil {
			return true, err
		}
		result := new(programmaticv1.SetEntryLabelResult)
		result.SetTree(mapped)
		wire.SetSetEntryLabel(result)
		return true, nil
	case ResponseRejected:
		return true, errors.New("map session response: rejection reached completed mapping")
	case ResponseUnspecified, ResponseUserRequestCompleted, ResponseCancelCompleted,
		ResponseRunState, ResponseMessages, ResponseModels, ResponseModelSelection:
		return false, nil
	default:
		return false, nil
	}
}

// mapForkSessionCompleted maps one durable fork replacement and exact next input.
func mapForkSessionCompleted(wire *programmaticv1.HostCompleted, response Response) error {
	replacement, present := response.Replacement.Get()
	if !present {
		return errors.New("map fork session: replacement is absent")
	}
	nextInput, present := replacement.NextInput.Get()
	if !present {
		return errors.New("map fork session: next input is absent")
	}
	entries, err := mapSessionEntries(replacement.ActiveBranch)
	if err != nil {
		return fmt.Errorf("map fork session active branch: %w", err)
	}
	result := new(programmaticv1.ForkSessionResult)
	result.SetInfo(mapSessionInfo(replacement.Info))
	result.SetActiveBranch(entries)
	result.SetNextInput(nextInput)
	wire.SetForkSession(result)
	return nil
}

// mapCloneSessionCompleted maps one durable active-branch clone replacement.
func mapCloneSessionCompleted(wire *programmaticv1.HostCompleted, response Response) error {
	replacement, present := response.Replacement.Get()
	if !present {
		return errors.New("map clone session: replacement is absent")
	}
	entries, err := mapSessionEntries(replacement.ActiveBranch)
	if err != nil {
		return fmt.Errorf("map clone session active branch: %w", err)
	}
	result := new(programmaticv1.CloneSessionResult)
	result.SetInfo(mapSessionInfo(replacement.Info))
	result.SetActiveBranch(entries)
	wire.SetCloneSession(result)
	return nil
}

// mapSessionInfo maps one active-session snapshot to its Programmatic representation.
func mapSessionInfo(info session.Info) *programmaticv1.SessionInfo {
	wire := new(programmaticv1.SessionInfo)
	wire.SetId(string(info.ID))
	if name, present := info.Name.Get(); present {
		wire.SetName(name)
	}
	wire.SetWorkingDirectory(info.WorkingDirectory)
	if path, present := info.StoragePath.Get(); present {
		wire.SetStoragePath(path)
	}
	wire.SetCreatedTime(timestamppb.New(info.CreatedAt))
	wire.SetUpdateTime(timestamppb.New(info.UpdatedAt))
	return wire
}

// mapSessionStatistics preserves message counts and optional complete token and cost values.
func mapSessionStatistics(statistics session.Statistics) *programmaticv1.SessionStatistics {
	wire := new(programmaticv1.SessionStatistics)
	wire.SetUserMessages(int64(statistics.UserMessages))
	wire.SetModelResponses(int64(statistics.ModelResponses))
	wire.SetToolCalls(int64(statistics.ToolCalls))
	wire.SetToolResults(int64(statistics.ToolResults))
	wire.SetTotalMessages(int64(statistics.TotalMessages))
	if usage, present := statistics.TokenUsage.Get(); present {
		tokens := new(programmaticv1.TokenUsage)
		setCommonUsage(
			tokens,
			usage.InputTokens,
			usage.OutputTokens,
			usage.CacheWriteTokens,
			usage.ReasoningTokens,
			usage.TotalTokens,
		)
		tokens.SetCacheReadTokens(usage.CacheReadTokens)
		wire.SetTokens(tokens)
	}
	if cost, present := statistics.EstimatedCost.Get(); present {
		wire.SetEstimatedCost(mapEstimatedCost(cost))
	}
	breakdown := lo.Map(
		statistics.CostBreakdown,
		func(group session.ProviderModelCost, _ int) *programmaticv1.ProviderModelCost {
			mapped := new(programmaticv1.ProviderModelCost)
			mapped.SetProviderId(string(group.Provider))
			mapped.SetModelId(string(group.Model))
			if cost, present := group.EstimatedCost.Get(); present {
				mapped.SetEstimatedCost(mapEstimatedCost(cost))
			}
			return mapped
		},
	)
	wire.SetCostBreakdown(breakdown)
	return wire
}

// commonUsage receives token fields shared by model and session usage messages.
type commonUsage interface {
	SetInputTokens(int64)
	SetOutputTokens(int64)
	SetCacheWriteTokens(int64)
	SetReasoningTokens(int64)
	SetTotalTokens(int64)
}

// setCommonUsage maps token fields shared by model and session usage messages.
func setCommonUsage(target commonUsage, input, output, cacheWrite, reasoning, total int64) {
	target.SetInputTokens(input)
	target.SetOutputTokens(output)
	target.SetCacheWriteTokens(cacheWrite)
	target.SetReasoningTokens(reasoning)
	target.SetTotalTokens(total)
}

// mapEstimatedCost preserves all calculated disjoint cost buckets and their stored total.
func mapEstimatedCost(cost session.EstimatedCost) *programmaticv1.EstimatedCost {
	mapped := new(programmaticv1.EstimatedCost)
	mapped.SetInput(cost.Input)
	mapped.SetOutput(cost.Output)
	mapped.SetCacheRead(cost.CacheRead)
	mapped.SetCacheWrite(cost.CacheWrite)
	mapped.SetTotal(cost.Total)
	return mapped
}

// mapSessionSummary preserves optional display text and lifecycle information in the public contract.
func mapSessionSummary(summary session.Summary) *programmaticv1.SessionSummary {
	wire := new(programmaticv1.SessionSummary)
	wire.SetInfo(mapSessionInfo(summary.Info))
	if text, present := summary.FirstUserText.Get(); present {
		wire.SetFirstUserText(text)
	}
	wire.SetTotalMessages(int64(summary.TotalMessages))
	return wire
}

// mapRunStateCompleted maps one run-state response after response-kind dispatch.
func mapRunStateCompleted(
	wire *programmaticv1.HostCompleted,
	response mo.Option[RunStateResult],
) error {
	stateResult, ok := response.Get()
	if !ok {
		return errors.New("map completed response: missing run state")
	}
	state, err := mapRunState(stateResult.State)
	if err != nil {
		return err
	}
	result := new(programmaticv1.RunStateResult)
	result.SetState(state)
	if activeOperationID, present := stateResult.ActiveOperationID.Get(); present {
		result.SetActiveOperationId(activeOperationID)
	}
	wire.SetRunState(result)
	return nil
}

// mapModelsCompleted maps the catalog and confirmed selection after response-kind dispatch.
func mapModelsCompleted(
	wire *programmaticv1.HostCompleted,
	response mo.Option[ModelsResult],
) error {
	modelsResult, ok := response.Get()
	if !ok {
		return errors.New("map completed response: missing models result")
	}
	models, err := mapConfiguredModels(modelsResult.Models)
	if err != nil {
		return err
	}
	activeSelection, ok := modelsResult.ActiveSelection.Get()
	if !ok {
		return errors.New("map models response: missing active selection")
	}
	selection, err := mapModelSelection(activeSelection)
	if err != nil {
		return err
	}
	result := new(programmaticv1.ModelsResult)
	result.SetModels(models)
	result.SetActiveSelection(selection)
	wire.SetModels(result)
	return nil
}

// mapModelSelectionCompleted maps one confirmed selection after response-kind dispatch.
func mapModelSelectionCompleted(
	wire *programmaticv1.HostCompleted,
	selection mo.Option[model.Selection],
) error {
	selectionValue, ok := selection.Get()
	if !ok {
		return errors.New("map completed response: missing model selection")
	}
	mapped, err := mapModelSelection(selectionValue)
	if err != nil {
		return err
	}
	result := new(programmaticv1.ModelSelectionResult)
	result.SetSelection(mapped)
	wire.SetModelSelection(result)
	return nil
}
