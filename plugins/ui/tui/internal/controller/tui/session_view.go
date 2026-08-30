package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/lo"
	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

const (
	// absentSessionValue marks optional session metadata that has no value.
	absentSessionValue = "<absent>"
	// sessionLineSeparator separates session information lines.
	sessionLineSeparator = "\n"
	// sessionIDLabel prefixes the session identifier.
	sessionIDLabel = "Session ID: "
	// sessionNameLabel prefixes the optional session name.
	sessionNameLabel = "Name: "
	// sessionWorkingDirectoryLabel prefixes the working directory.
	sessionWorkingDirectoryLabel = "Working directory: "
	// sessionStoragePathLabel prefixes the optional storage path.
	sessionStoragePathLabel = "Storage path: "
	// sessionCreatedLabel prefixes the creation time.
	sessionCreatedLabel = "Created: "
	// sessionUpdatedLabel prefixes the last update time.
	sessionUpdatedLabel = "Updated: "
)

const (
	// sessionMessagesFormat renders stored message counts.
	sessionMessagesFormat = "Messages: %d user, %d model, %d tool results, %d total"
	// sessionToolCallsFormat renders the tool-call count.
	sessionToolCallsFormat = "Tool calls: %d"
	// sessionUsageUnavailableText reports absent complete token accounting.
	sessionUsageUnavailableText = "Tokens: unavailable"
	// sessionUsageFormat renders complete token accounting.
	sessionUsageFormat = "Tokens: %d input, %d output, %d cache read, %d cache write, %d total"
	// sessionReasoningUsageFormat renders the reasoning subset.
	sessionReasoningUsageFormat = "Reasoning tokens: %d, included in output"
)

const (
	// sessionEstimatedCostFormat renders complete aggregate cost.
	sessionEstimatedCostFormat = "Estimated cost: $%.6f"
	// sessionEstimatedCostUnavailableText reports absent aggregate cost.
	sessionEstimatedCostUnavailableText = "Estimated cost: unavailable"
	// sessionCostBreakdownFormat renders one provider-model cost.
	sessionCostBreakdownFormat = "%s: $%.6f"
	// sessionCostUnavailableSuffix reports absent provider-model cost.
	sessionCostUnavailableSuffix = ": unavailable"
	// sessionProviderModelSeparator separates provider and model identifiers.
	sessionProviderModelSeparator = "/"
)

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
		TreeCommand:     mo.None[presentationdomain.TreeCommand](),
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
		return strings.Join(lines, sessionLineSeparator)
	}
	lines = append(lines,
		fmt.Sprintf(
			sessionMessagesFormat,
			values.UserMessages, values.ModelResponses, values.ToolResults, values.TotalMessages,
		),
		fmt.Sprintf(sessionToolCallsFormat, values.ToolCalls),
	)
	lines = appendSessionTokenLines(lines, values.TokenUsage)
	lines = appendSessionCostLines(lines, values)
	return strings.Join(lines, sessionLineSeparator)
}

// appendSessionTokenLines renders token availability without affecting count lines.
func appendSessionTokenLines(
	lines []string,
	usage mo.Option[presentationdomain.TokenUsage],
) []string {
	tokens, available := usage.Get()
	if !available {
		return append(lines, sessionUsageUnavailableText)
	}
	return append(lines,
		fmt.Sprintf(
			sessionUsageFormat,
			tokens.InputTokens, tokens.OutputTokens, tokens.CacheReadTokens,
			tokens.CacheWriteTokens, tokens.TotalTokens,
		),
		fmt.Sprintf(sessionReasoningUsageFormat, tokens.ReasoningTokens),
	)
}

// appendSessionCostLines renders aggregate and ordered provider-model cost availability.
func appendSessionCostLines(
	lines []string,
	statistics presentationdomain.SessionStatistics,
) []string {
	if cost, available := statistics.EstimatedCost.Get(); available {
		lines = append(lines, fmt.Sprintf(sessionEstimatedCostFormat, cost.Total))
	} else {
		lines = append(lines, sessionEstimatedCostUnavailableText)
	}
	costLines := lo.Map(statistics.CostBreakdown, func(group presentationdomain.ProviderModelCost, _ int) string {
		label := group.ProviderID + sessionProviderModelSeparator + group.ModelID
		if cost, available := group.EstimatedCost.Get(); available {
			return fmt.Sprintf(sessionCostBreakdownFormat, label, cost.Total)
		}
		return label + sessionCostUnavailableSuffix
	})
	return append(lines, costLines...)
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
		sessionIDLabel + info.ID,
		sessionNameLabel + name,
		sessionWorkingDirectoryLabel + info.WorkingDirectory,
		sessionStoragePathLabel + storagePath,
		sessionCreatedLabel + info.CreatedAt.UTC().Format(time.RFC3339Nano),
		sessionUpdatedLabel + info.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, sessionLineSeparator)
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
		TreeEvent:            mo.None[presentationdomain.TreeEvent](),
	}
}
