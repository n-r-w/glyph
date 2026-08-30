package tui

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

const (
	// unspecifiedReasoningText identifies an unavailable reasoning choice.
	unspecifiedReasoningText = "unspecified"
	// unavailableStatusText identifies unavailable Host state.
	unavailableStatusText = "Unavailable"
)

// View renders the current presentation as plain terminal text.
func (model Model) View() tea.View {
	selector := model.visibleSelectorLines()
	body := model.visibleBodyLines(len(selector))
	lines := make([]string, 0, fixedViewLineCount+len(body)+len(selector))
	lines = append(
		lines,
		"Glyph",
		"Status: "+availabilityText(model.state.Availability)+" | "+selectionText(model.state.ModelSelection),
	)
	lines = append(lines, body...)
	lines = append(lines, selector...)
	selectionKeys := "Keys: Enter submit | Ctrl+L models | Ctrl+P next model | Shift+Ctrl+P previous model"
	if model.reasoningSelectionVisible() {
		selectionKeys += " | Shift+Tab reasoning"
	}
	lines = append(
		lines,
		"Request: "+string(model.input[:model.cursor])+"|"+string(model.input[model.cursor:]),
		fmt.Sprintf("Terminal: %dx%d", model.width, model.height),
		selectionKeys+" | Ctrl+T reasoning display | Ctrl+C stop | Ctrl+R retry authentication | Ctrl+Q quit",
	)

	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true

	return view
}

// reasoningSelectionVisible hides the shortcut when the selected model has no effective alternative.
func (model Model) reasoningSelectionVisible() bool {
	if len(model.state.Models) == 0 {
		return true
	}
	configured := model.state.Models[model.currentModelIndex()]
	return len(configured.Reasoning.Choices) > 1
}

// visibleSelectorLines renders a bounded window around the highlighted model.
func (model Model) visibleSelectorLines() []string {
	rowCount := len(model.state.Models)
	title := "Models:"
	if model.sessionSelector {
		title = "Sessions:"
		rowCount = len(model.state.Sessions)
	}
	if !model.selectorOpen || rowCount == 0 {
		return nil
	}
	statusLineCount := 0
	if model.sessionSelector && model.resumeStatus != "" {
		statusLineCount = 1
	}
	capacity := min(maxVisibleSelectorRows, rowCount)
	if model.height > 0 {
		capacity = min(capacity, max(1, model.height-fixedViewLineCount-selectorFixedLineCount-statusLineCount))
	}
	start := model.selectorRow - capacity/selectorCenterDivisor
	start = max(0, min(start, rowCount-capacity))
	lines := make([]string, 0, selectorFixedLineCount+statusLineCount+capacity)
	lines = append(lines, title)
	for index := start; index < start+capacity; index++ {
		prefix := "  "
		if index == model.selectorRow {
			prefix = "> "
		}
		if model.sessionSelector {
			summary := model.state.Sessions[index]
			label := summary.Info.ID
			if summary.Info.NamePresent {
				label = summary.Info.Name
			} else if summary.TextPresent {
				label = summary.FirstUserText
			}
			row := fmt.Sprintf(
				"%s | %s | %d messages", label, summary.Info.UpdatedAt.Format(time.RFC3339), summary.TotalMessages,
			)
			lines = append(lines, prefix+ellipsize(row, max(1, model.width-len(prefix))))
			continue
		}
		configured := model.state.Models[index]
		lines = append(lines, prefix+configured.ProviderID+" / "+configured.ModelID)
	}
	if statusLineCount > 0 {
		lines = append(lines, ellipsize("Session status: "+model.resumeStatus, max(1, model.width)))
	}
	return append(lines, "Selector: Up/Down navigate | Enter confirm | Escape cancel")
}

// ellipsize keeps selector rows single-line and rune-safe within the available terminal width.
func ellipsize(value string, width int) string {
	normalized := strings.Join(strings.Fields(value), " ")
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(normalized) <= width {
		return normalized
	}
	return ansi.Truncate(normalized, width, "…")
}

// visibleBodyLines renders newest content first and stops when the viewport is full.
//
//nolint:gocyclo // The flat branches preserve transcript order across visible content kinds.
func (model Model) visibleBodyLines(reservedLines int) []string {
	// A negative capacity preserves full output until Bubble Tea reports terminal dimensions.
	capacity := -1
	if model.height > 0 {
		capacity = model.height - fixedViewLineCount - reservedLines
		if capacity <= 0 {
			return nil
		}
	}

	estimatedLines := len(model.state.Startup) + len(model.state.Transcript) +
		len(model.state.ActiveModel) + len(model.state.ActiveToolCalls) + 1
	if capacity >= 0 && estimatedLines > capacity {
		estimatedLines = capacity
	}
	// Walk body sections from newest to oldest so rendering stops when the viewport is full.
	// appendNewestWrapped stores visual lines in reverse order to avoid repeated prepends.
	lines := make([]string, 0, estimatedLines)
	if authorizationURL, ok := model.state.AuthorizationURL.Get(); ok {
		lines = appendNewestWrapped(lines, "Authorization: "+authorizationURL, model.width, capacity)
	}

	if hasBodyCapacity(lines, capacity) {
		calls := make([]presentationdomain.ToolCallState, 0, len(model.state.ActiveToolCalls))
		for _, call := range model.state.ActiveToolCalls {
			calls = append(calls, call)
		}
		slices.SortFunc(calls, func(left, right presentationdomain.ToolCallState) int {
			return cmp.Compare(left.Position, right.Position)
		})
		for index := len(calls) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
			lines = appendNewestWrapped(lines, renderToolCall(calls[index]), model.width, capacity)
		}
	}

	if hasBodyCapacity(lines, capacity) {
		positions := make([]int, 0, len(model.state.ActiveModel))
		for position := range model.state.ActiveModel {
			positions = append(positions, position)
		}
		slices.Sort(positions)
		for index := len(positions) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
			content := model.state.ActiveModel[positions[index]]
			lines = appendNewestWrapped(
				lines,
				renderActiveModelLine(content, model.reasoningExpanded),
				model.width,
				capacity,
			)
		}
	}

	// Completed transcript and startup lines are older than all active content.
	for index := len(model.state.Transcript) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
		line := model.state.Transcript[index]
		if line.Kind == presentationdomain.LineReasoning && !model.reasoningExpanded {
			lines = appendNewestWrapped(lines, "Reasoning: [collapsed]", model.width, capacity)
			continue
		}
		lines = appendNewestWrapped(lines, renderLine(line), model.width, capacity)
	}
	for index := len(model.state.Startup) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
		lines = appendNewestWrapped(lines, renderLine(model.state.Startup[index]), model.width, capacity)
	}

	// Restore chronological display order after the newest-first traversal.
	slices.Reverse(lines)
	return lines
}

func hasBodyCapacity(lines []string, capacity int) bool {
	return capacity < 0 || len(lines) < capacity
}

func appendNewestWrapped(lines []string, line string, width, capacity int) []string {
	wrapped := wrappedBodyLines(line, width)
	for index := len(wrapped) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
		lines = append(lines, wrapped[index])
	}
	return lines
}

func renderActiveModelLine(content presentationdomain.ActiveModelContent, reasoningExpanded bool) string {
	kind := presentationdomain.LineModel
	contentKind, ok := content.Kind.Get()
	if !ok {
		contentKind = presentationdomain.ModelContentUnspecified
	}
	switch contentKind {
	case presentationdomain.ModelContentRefusal:
		kind = presentationdomain.LineRefusal
	case presentationdomain.ModelContentReasoning:
		kind = presentationdomain.LineReasoning
	case presentationdomain.ModelContentText, presentationdomain.ModelContentUnspecified:
	}
	if kind == presentationdomain.LineReasoning && !reasoningExpanded {
		return "Reasoning: [collapsed]"
	}
	return renderLine(presentationdomain.Line{
		Kind:     kind,
		Text:     content.Text,
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	})
}

// wrappedBodyLines converts one logical body line into readable terminal-width visual lines.
func wrappedBodyLines(line string, width int) []string {
	if width <= 0 {
		return strings.Split(line, "\n")
	}

	return strings.Split(ansi.Wrap(line, width, ""), "\n")
}

func renderToolCall(call presentationdomain.ToolCallState) string {
	status := "final"
	if call.Provisional {
		status = "provisional"
	}
	parts := make([]string, 0, len(call.Fields)+1)
	for _, field := range call.Fields {
		if value, ok := field.Value.Get(); ok {
			encoded, _ := json.Marshal(value)
			parts = append(parts, field.Name+"="+string(encoded))
		} else if prefix, prefixOK := field.Prefix.Get(); prefixOK {
			parts = append(parts, field.Name+"="+prefix)
		}
	}
	if !call.Provisional && call.Arguments != nil {
		arguments, _ := json.Marshal(call.Arguments)
		parts = []string{string(arguments)}
	}
	line := "[tool:call] " + call.Name + " (" + status + ")"
	if len(parts) > 0 {
		line += " " + strings.Join(parts, " ")
	}
	return line
}

// linePrefix assigns a stable prefix to non-tool presentation lines.
func linePrefix(kind presentationdomain.LineKind) string {
	switch kind {
	case presentationdomain.LineInformation, presentationdomain.LineUnspecified:
		return "[info]"
	case presentationdomain.LineError:
		return "[error]"
	case presentationdomain.LineWarning:
		return "[warning]"
	case presentationdomain.LineUser:
		return "user:"
	case presentationdomain.LineModel:
		return "assistant:"
	case presentationdomain.LineRefusal:
		return "[refusal]"
	case presentationdomain.LineReasoning:
		return "reasoning:"
	case presentationdomain.LineToolStatus, presentationdomain.LineToolStdout,
		presentationdomain.LineToolStderr, presentationdomain.LineToolDone,
		presentationdomain.LineToolError:
		return toolLinePrefix(kind)
	default:
		return ""
	}
}

// toolLinePrefix assigns a stable prefix to tool presentation lines.
func toolLinePrefix(kind presentationdomain.LineKind) string {
	switch kind {
	case presentationdomain.LineToolStatus:
		return "[tool:status]"
	case presentationdomain.LineToolStdout:
		return "[tool:stdout]"
	case presentationdomain.LineToolStderr:
		return "[tool:stderr]"
	case presentationdomain.LineToolDone:
		return "[tool:done]"
	case presentationdomain.LineToolError:
		return "[tool:error]"
	case presentationdomain.LineUnspecified, presentationdomain.LineInformation,
		presentationdomain.LineError, presentationdomain.LineWarning, presentationdomain.LineUser,
		presentationdomain.LineModel, presentationdomain.LineRefusal, presentationdomain.LineReasoning:
		return ""
	}
	return ""
}

// renderLine assigns one stable terminal prefix to each presentation line kind.
func renderLine(line presentationdomain.Line) string {
	prefix := linePrefix(line.Kind)

	parts := []string{prefix}
	if toolName, ok := line.ToolName.Get(); ok && toolName != "" {
		parts = append(parts, toolName)
	}
	if status, ok := line.Status.Get(); ok && status != "" {
		parts = append(parts, "("+status+")")
	}
	if text, ok := line.Text.Get(); ok && text != "" {
		parts = append(parts, text)
	}

	return strings.Join(parts, " ")
}

// selectionText renders only the Host-confirmed selection.
func selectionText(selectionOption mo.Option[presentationdomain.ModelSelection]) string {
	selection, ok := selectionOption.Get()
	if !ok || selection.ProviderID == "" || selection.ModelID == "" {
		return "model unavailable"
	}
	return fmt.Sprintf(
		"%s / %s / %s",
		selection.ProviderID,
		selection.ModelID,
		reasoningText(selection.ReasoningChoice),
	)
}

// reasoningText maps the closed reasoning set to its configured spelling.
func reasoningText(level presentationdomain.ReasoningChoice) string {
	switch level {
	case presentationdomain.ReasoningChoiceOff:
		return "off"
	case presentationdomain.ReasoningChoiceOn:
		return "on"
	case presentationdomain.ReasoningChoiceMinimal:
		return "minimal"
	case presentationdomain.ReasoningChoiceLow:
		return "low"
	case presentationdomain.ReasoningChoiceMedium:
		return "medium"
	case presentationdomain.ReasoningChoiceHigh:
		return "high"
	case presentationdomain.ReasoningChoiceXHigh:
		return "xhigh"
	case presentationdomain.ReasoningChoiceMax:
		return "max"
	case presentationdomain.ReasoningChoiceUnspecified:
		return unspecifiedReasoningText
	default:
		return unspecifiedReasoningText
	}
}

// availabilityText maps Host availability to concise terminal status text.
func availabilityText(availabilityOption mo.Option[presentationdomain.Availability]) string {
	availability, ok := availabilityOption.Get()
	if !ok {
		return unavailableStatusText
	}
	switch availability {
	case presentationdomain.AvailabilityChecking:
		return "Checking"
	case presentationdomain.AvailabilityAuthenticating:
		return "Authenticating"
	case presentationdomain.AvailabilityAuthenticationFailed:
		return "Authentication failed"
	case presentationdomain.AvailabilityIdle:
		return "Idle"
	case presentationdomain.AvailabilityRunning:
		return "Running"
	case presentationdomain.AvailabilityUnspecified:
		return unavailableStatusText
	default:
		return unavailableStatusText
	}
}
