package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// absentSessionValue marks optional session metadata that has no value.
const absentSessionValue = "<absent>"

// emitCommand serializes one UI command without blocking the update loop.
func (model Model) emitCommand(command presentationdomain.Command) (tea.Model, tea.Cmd) {
	if model.emitting {
		return model, nil
	}
	model.emitting = true

	return model, func() tea.Msg {
		return emissionResultMsg{
			command: command,
			err:     model.emit(command),
		}
	}
}

// emitSessionCommand preserves the editor until the Host confirms or rejects the lifecycle operation.
func (model Model) emitSessionCommand(kind presentationdomain.CommandKind, id, name string) (tea.Model, tea.Cmd) {
	command := presentationdomain.Command{
		Kind:            kind,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.EmptyableToOption(id),
		SessionName:     mo.EmptyableToOption(name),
	}
	if kind == presentationdomain.CommandSetSessionName {
		command.SessionName = mo.Some(name)
	}
	// Keep the editor unchanged until a Host frame confirms the lifecycle operation.
	return model.emitCommand(command)
}

// formatSessionInformation renders metadata, message counts, and optional token and cost values.
func formatSessionInformation(
	info presentationdomain.SessionInfo,
	statistics mo.Option[presentationdomain.SessionStatistics],
) string {
	lines := []string{formatSessionInfo(info)}
	values, present := statistics.Get()
	if !present {
		return strings.Join(lines, "\n")
	}
	lines = append(lines,
		fmt.Sprintf(
			"Messages: %d user, %d model, %d tool results, %d total",
			values.UserMessages, values.ModelResponses, values.ToolResults, values.TotalMessages,
		),
		fmt.Sprintf("Tool calls: %d", values.ToolCalls),
	)
	lines = appendSessionTokenLines(lines, values.TokenUsage)
	lines = appendSessionCostLines(lines, values)
	return strings.Join(lines, "\n")
}

// appendSessionTokenLines renders token availability without affecting count lines.
func appendSessionTokenLines(
	lines []string,
	usage mo.Option[presentationdomain.TokenUsage],
) []string {
	tokens, available := usage.Get()
	if !available {
		return append(lines, "Tokens: unavailable")
	}
	return append(lines,
		fmt.Sprintf(
			"Tokens: %d input, %d output, %d cache read, %d cache write, %d total",
			tokens.InputTokens, tokens.OutputTokens, tokens.CacheReadTokens,
			tokens.CacheWriteTokens, tokens.TotalTokens,
		),
		fmt.Sprintf("Reasoning tokens: %d, included in output", tokens.ReasoningTokens),
	)
}

// appendSessionCostLines renders aggregate and ordered provider-model cost availability.
func appendSessionCostLines(
	lines []string,
	statistics presentationdomain.SessionStatistics,
) []string {
	if cost, available := statistics.EstimatedCost.Get(); available {
		lines = append(lines, fmt.Sprintf("Estimated cost: $%.6f", cost.Total))
	} else {
		lines = append(lines, "Estimated cost: unavailable")
	}
	for groupIndex := range statistics.CostBreakdown {
		group := &statistics.CostBreakdown[groupIndex]
		label := group.ProviderID + "/" + group.ModelID
		if cost, available := group.EstimatedCost.Get(); available {
			lines = append(lines, fmt.Sprintf("%s: $%.6f", label, cost.Total))
		} else {
			lines = append(lines, label+": unavailable")
		}
	}
	return lines
}

// formatSessionInfo renders lifecycle-only session metadata as safe presentation text.
func formatSessionInfo(info presentationdomain.SessionInfo) string {
	name := absentSessionValue
	if info.NamePresent {
		name = info.Name
	}
	storagePath := absentSessionValue
	if info.StoragePresent {
		storagePath = info.StoragePath
	}
	return strings.Join([]string{
		"Session ID: " + info.ID,
		"Name: " + name,
		"Working directory: " + info.WorkingDirectory,
		"Storage path: " + storagePath,
		"Created: " + info.CreatedAt.UTC().Format(time.RFC3339Nano),
		"Updated: " + info.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, "\n")
}

// sessionInformationEvent adapts formatted session metadata to a non-session-changing information event.
func sessionInformationEvent(text string) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInformation,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.Some(text),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	}
}
