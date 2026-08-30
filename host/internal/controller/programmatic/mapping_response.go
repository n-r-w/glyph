package programmatic

import (
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

func mapResponse(response Response) (*programmaticv1.OpenResponse, error) {
	wire := new(programmaticv1.CommandResponse)
	if handled, err := mapSessionOrRejectionResponse(wire, response); handled {
		if err != nil {
			return nil, err
		}
		return wrapCommandResponse(response.CorrelationID, wire), nil
	}
	switch response.Kind {
	case ResponseUserRequestAccepted:
		wire.SetUserRequestAccepted(new(programmaticv1.UserRequestAccepted))
	case ResponseAbortCompleted:
		wire.SetAbortCompleted(new(programmaticv1.AbortCompleted))
	case ResponseRunState:
		if err := mapRunStateCommandResponse(wire, response.State); err != nil {
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
		if err := mapModelsCommandResponse(wire, response.Models); err != nil {
			return nil, err
		}
	case ResponseModelSelection:
		if err := mapModelSelectionCommandResponse(wire, response.Selection); err != nil {
			return nil, err
		}
	case ResponseSessionInfo, ResponseSessions, ResponseSessionEntries, ResponseSessionStats,
		ResponseSessionTree, ResponseSessionTreeNavigation, ResponseForkSession, ResponseCloneSession,
		ResponseSetEntryLabel, ResponseRejected:
		return nil, errors.New("map command response: handled response was not mapped")
	case ResponseUnspecified:
		return nil, errors.New("map command response: unspecified response kind")
	default:
		return nil, fmt.Errorf("map command response: unknown response kind %d", response.Kind)
	}
	return wrapCommandResponse(response.CorrelationID, wire), nil
}

// mapSessionOrRejectionResponse isolates lifecycle and rejection payload mapping from the core response dispatch.
//
//nolint:gocyclo // The switch maps every closed session response kind explicitly.
func mapSessionOrRejectionResponse(wire *programmaticv1.CommandResponse, response Response) (bool, error) {
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
		return true, mapSessionTreeCommandResponse(wire, tree)
	case ResponseSessionTreeNavigation:
		navigation, present := response.TreeNavigation.Get()
		if !present {
			return true, errors.New("map tree navigation: result is absent")
		}
		return true, mapTreeNavigationCommandResponse(wire, navigation)
	case ResponseForkSession:
		return true, mapForkSessionCommandResponse(wire, response)
	case ResponseCloneSession:
		return true, mapCloneSessionCommandResponse(wire, response)
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
		return true, mapRejectionCommandResponse(wire, response.Rejection)
	case ResponseUnspecified, ResponseUserRequestAccepted, ResponseAbortCompleted,
		ResponseRunState, ResponseMessages, ResponseModels, ResponseModelSelection:
		return false, nil
	default:
		return false, nil
	}
}

// mapForkSessionCommandResponse maps one durable fork replacement and exact next input.
func mapForkSessionCommandResponse(wire *programmaticv1.CommandResponse, response Response) error {
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

// mapCloneSessionCommandResponse maps one durable active-branch clone replacement.
func mapCloneSessionCommandResponse(wire *programmaticv1.CommandResponse, response Response) error {
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

// mapRunStateCommandResponse maps one run-state response after response-kind dispatch.
func mapRunStateCommandResponse(
	wire *programmaticv1.CommandResponse,
	response mo.Option[RunStateResult],
) error {
	stateResult, ok := response.Get()
	if !ok {
		return errors.New("map command response: missing run state")
	}
	state, err := mapRunState(stateResult.State)
	if err != nil {
		return err
	}
	result := new(programmaticv1.RunStateResult)
	result.SetState(state)
	if activeCorrelationID, present := stateResult.ActiveCorrelationID.Get(); present {
		result.SetActiveCorrelationId(activeCorrelationID)
	}
	wire.SetRunState(result)
	return nil
}

// mapModelsCommandResponse maps the catalog and confirmed selection after response-kind dispatch.
func mapModelsCommandResponse(
	wire *programmaticv1.CommandResponse,
	response mo.Option[ModelsResult],
) error {
	modelsResult, ok := response.Get()
	if !ok {
		return errors.New("map command response: missing models result")
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

// mapModelSelectionCommandResponse maps one confirmed selection after response-kind dispatch.
func mapModelSelectionCommandResponse(
	wire *programmaticv1.CommandResponse,
	selection mo.Option[model.Selection],
) error {
	selectionValue, ok := selection.Get()
	if !ok {
		return errors.New("map command response: missing model selection")
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

// mapRejectionCommandResponse maps one rejection after response-kind dispatch.
func mapRejectionCommandResponse(
	wire *programmaticv1.CommandResponse,
	response mo.Option[Rejection],
) error {
	rejection, ok := response.Get()
	if !ok {
		return errors.New("map command response: missing rejection")
	}
	command, err := mapCommandType(rejection.Command)
	if err != nil {
		return err
	}
	code, err := mapRejectionCode(rejection.Code)
	if err != nil {
		return err
	}
	rejected := new(programmaticv1.CommandRejected)
	rejected.SetCommand(command)
	rejected.SetCode(code)
	rejected.SetMessage(rejection.Message)
	wire.SetRejected(rejected)
	return nil
}

func wrapCommandResponse(
	correlationID string,
	response *programmaticv1.CommandResponse,
) *programmaticv1.OpenResponse {
	mapped := new(programmaticv1.OpenResponse)
	mapped.SetCorrelationId(correlationID)
	mapped.SetCommandResponse(response)
	return mapped
}
