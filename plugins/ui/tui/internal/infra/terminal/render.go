package terminal

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/samber/lo"
	"github.com/samber/mo"

	presentation "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

const (
	// tuiTitle is the standard TUI heading.
	tuiTitle = "Glyph"
	// statusLabel prefixes the current Host status.
	statusLabel = "Status: "
	// statusSeparator separates status fields.
	statusSeparator = " | "
	// requestLabel prefixes the main editor.
	requestLabel = "Request: "
	// terminalSizeFormat renders terminal dimensions.
	terminalSizeFormat = "Terminal: %dx%d"
)

const (
	// selectionKeysText lists model-selection keys.
	selectionKeysText = "Keys: Enter submit | Ctrl+L models | Ctrl+P next model | Shift+Ctrl+P previous model"
	// reasoningSelectionKeyText lists the conditional reasoning-selection key.
	reasoningSelectionKeyText = " | Shift+Tab reasoning"
	// commonKeysText lists keys that are always available in the editor.
	commonKeysText = " | Ctrl+T reasoning display | Ctrl+C stop | Ctrl+R retry authentication | Ctrl+Q quit"
)

const (
	// modelsSelectorTitle labels configured model selection.
	modelsSelectorTitle = "Models:"
	// sessionsSelectorTitle labels stored session selection.
	sessionsSelectorTitle = "Sessions:"
	// sessionRowFormat renders one stored session row.
	sessionRowFormat = "%s | %s | %d messages"
	// sessionStatusLabel prefixes a rejected resume status.
	sessionStatusLabel = "Session status: "
	// selectorHelpText lists shared selector controls.
	selectorHelpText = "Selector: Up/Down navigate | Enter confirm | Escape cancel"
)

const (
	// authorizationLabel prefixes a pending authorization URL.
	authorizationLabel = "Authorization: "
	// reasoningCollapsedText represents hidden reasoning content.
	reasoningCollapsedText = "Reasoning: [collapsed]"
	// toolCallFinalText identifies a finalized tool call.
	toolCallFinalText = "final"
	// toolCallProvisionalText identifies a streaming tool call.
	toolCallProvisionalText = "provisional"
	// toolCallPrefix identifies a tool call line.
	toolCallPrefix = "[tool:call] "
)

const (
	// informationLinePrefix identifies information lines.
	informationLinePrefix = "[info]"
	// errorLinePrefix identifies error lines.
	errorLinePrefix = "[error]"
	// warningLinePrefix identifies warning lines.
	warningLinePrefix = "[warning]"
	// userLinePrefix identifies user lines.
	userLinePrefix = "user:"
	// modelLinePrefix identifies model lines.
	modelLinePrefix = "assistant:"
	// refusalLinePrefix identifies refusal lines.
	refusalLinePrefix = "[refusal]"
	// reasoningLinePrefix identifies reasoning lines.
	reasoningLinePrefix = "reasoning:"
	// branchSummaryLinePrefix identifies abandoned-branch context.
	branchSummaryLinePrefix = "[branch]"
	// branchSummaryCollapsedText describes hidden branch-summary content.
	branchSummaryCollapsedText = "Branch summary (ctrl+o to expand)"
	// branchSummaryExpandedTitle labels visible branch-summary content.
	branchSummaryExpandedTitle = "Branch Summary"
	// branchSummaryCollapsedFormat renders a collapsed branch-summary item.
	branchSummaryCollapsedFormat = "%s\n\n%s"
	// branchSummaryExpandedFormat renders an expanded branch-summary item.
	branchSummaryExpandedFormat = "%s\n\n%s\n\n%s"
)

const (
	// toolStatusLinePrefix identifies tool status lines.
	toolStatusLinePrefix = "[tool:status]"
	// toolStdoutLinePrefix identifies tool standard output lines.
	toolStdoutLinePrefix = "[tool:stdout]"
	// toolStderrLinePrefix identifies tool error output lines.
	toolStderrLinePrefix = "[tool:stderr]"
	// toolDoneLinePrefix identifies successful tool completion lines.
	toolDoneLinePrefix = "[tool:done]"
	// toolErrorLinePrefix identifies failed tool completion lines.
	toolErrorLinePrefix = "[tool:error]"
)

const (
	// modelUnavailableText identifies an unavailable model selection.
	modelUnavailableText = "model unavailable"
	// modelSelectionFormat renders one confirmed model selection.
	modelSelectionFormat = "%s / %s / %s"
)

const (
	// unspecifiedReasoningText identifies an unavailable reasoning choice.
	unspecifiedReasoningText = "unspecified"
	// reasoningOffText identifies disabled reasoning.
	reasoningOffText = "off"
	// reasoningOnText identifies provider-default reasoning.
	reasoningOnText = "on"
	// reasoningMinimalText identifies minimal reasoning.
	reasoningMinimalText = "minimal"
	// reasoningLowText identifies low reasoning.
	reasoningLowText = "low"
	// reasoningMediumText identifies medium reasoning.
	reasoningMediumText = "medium"
	// reasoningHighText identifies high reasoning.
	reasoningHighText = "high"
	// reasoningXHighText identifies extra-high reasoning.
	reasoningXHighText = "xhigh"
	// reasoningMaxText identifies maximum reasoning.
	reasoningMaxText = "max"
)

const (
	// unavailableStatusText identifies unavailable Host state.
	unavailableStatusText = "Unavailable"
	// checkingStatusText identifies authentication-state checking.
	checkingStatusText = "Checking"
	// authenticatingStatusText identifies active authentication.
	authenticatingStatusText = "Authenticating"
	// authenticationFailedStatusText identifies failed authentication.
	authenticationFailedStatusText = "Authentication failed"
	// idleStatusText identifies an idle Host.
	idleStatusText = "Idle"
	// runningStatusText identifies a running agent.
	runningStatusText = "Running"
)

// View renders the current presentation as plain terminal text.
func (model Model) View() tea.View {
	selector := model.visibleSelectorLines()
	body := model.visibleBodyLines(len(selector))
	lines := make([]string, 0, fixedViewLineCount+len(body)+len(selector))
	status := statusLabel + availabilityText(
		model.snapshot.Body.Availability,
	) + statusSeparator + selectionText(
		model.snapshot.Body.ModelSelection,
	)
	if model.snapshot.TreeStatus != "" {
		status += statusSeparator + model.snapshot.TreeStatus
	}
	lines = append(lines, tuiTitle, status)
	lines = append(lines, body...)
	lines = append(lines, selector...)
	selectionKeys := selectionKeysText
	if model.reasoningSelectionVisible() {
		selectionKeys += reasoningSelectionKeyText
	}
	lines = append(
		lines,
		requestLabel+string(
			model.snapshot.Input[:model.snapshot.Cursor],
		)+"|"+string(
			model.snapshot.Input[model.snapshot.Cursor:],
		),
		fmt.Sprintf(terminalSizeFormat, model.width, model.height),
		selectionKeys+commonKeysText,
	)

	view := tea.NewView(strings.Join(lines, "\n"))
	view.AltScreen = true

	return view
}

// reasoningSelectionVisible hides the shortcut when the selected model has no effective alternative.
func (model Model) reasoningSelectionVisible() bool {
	return model.snapshot.ReasoningSelectionVisible
}

// visibleSelectorLines renders a bounded window around the highlighted model.
func (model Model) visibleSelectorLines() []string {
	if model.snapshot.TreeMode != presentation.TreeClosed {
		return model.treeSelectorLines()
	}
	rowCount := len(model.snapshot.Body.Models)
	title := modelsSelectorTitle
	if model.snapshot.SessionSelector {
		title = sessionsSelectorTitle
		rowCount = len(model.snapshot.Body.Sessions)
	}
	if !model.snapshot.SelectorOpen || rowCount == 0 {
		return nil
	}
	statusLineCount := 0
	if model.snapshot.SessionSelector && model.snapshot.ResumeStatus != "" {
		statusLineCount = 1
	}
	capacity := min(maxVisibleSelectorRows, rowCount)
	if model.height > 0 {
		capacity = min(capacity, max(1, model.height-fixedViewLineCount-selectorFixedLineCount-statusLineCount))
	}
	start := model.snapshot.SelectorRow - capacity/selectorCenterDivisor
	start = max(0, min(start, rowCount-capacity))
	lines := make([]string, 0, selectorFixedLineCount+statusLineCount+capacity)
	lines = append(lines, title)
	for index := start; index < start+capacity; index++ {
		prefix := inactiveSelectorPrefix
		if index == model.snapshot.SelectorRow {
			prefix = activeSelectorPrefix
		}
		if model.snapshot.SessionSelector {
			summary := model.snapshot.Body.Sessions[index]
			label := summary.Info.ID
			if summary.Info.NamePresent {
				label = summary.Info.Name
			} else if summary.TextPresent {
				label = summary.FirstUserText
			}
			row := fmt.Sprintf(
				sessionRowFormat, label, summary.Info.UpdatedAt.Format(time.RFC3339), summary.TotalMessages,
			)
			lines = append(lines, prefix+ellipsize(row, max(1, model.width-len(prefix))))
			continue
		}
		configured := model.snapshot.Body.Models[index]
		lines = append(lines, prefix+configured.ProviderID+" / "+configured.ModelID)
	}
	if statusLineCount > 0 {
		lines = append(lines, ellipsize(sessionStatusLabel+model.snapshot.ResumeStatus, max(1, model.width)))
	}
	return append(lines, selectorHelpText)
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

	estimatedLines := len(model.snapshot.Body.Startup) + len(model.snapshot.Body.Transcript) +
		len(model.snapshot.Body.ActiveModel) + len(model.snapshot.Body.ActiveToolCalls) + 1
	if capacity >= 0 && estimatedLines > capacity {
		estimatedLines = capacity
	}
	// Walk body sections from newest to oldest so rendering stops when the viewport is full.
	// appendNewestWrapped stores visual lines in reverse order to avoid repeated prepends.
	lines := make([]string, 0, estimatedLines)
	if authorizationURL, ok := model.snapshot.Body.AuthorizationURL.Get(); ok {
		lines = appendNewestWrapped(lines, authorizationLabel+authorizationURL, model.width, capacity)
	}

	if hasBodyCapacity(lines, capacity) {
		calls := lo.Values(model.snapshot.Body.ActiveToolCalls)
		slices.SortFunc(calls, func(left, right presentation.ToolCallState) int {
			return cmp.Compare(left.Position, right.Position)
		})
		for index := len(calls) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
			lines = appendNewestWrapped(lines, renderToolCall(calls[index]), model.width, capacity)
		}
	}

	if hasBodyCapacity(lines, capacity) {
		positions := lo.Keys(model.snapshot.Body.ActiveModel)
		slices.Sort(positions)
		for index := len(positions) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
			content := model.snapshot.Body.ActiveModel[positions[index]]
			lines = appendNewestWrapped(
				lines,
				renderActiveModelLine(content, model.snapshot.ReasoningExpanded),
				model.width,
				capacity,
			)
		}
	}

	// Completed transcript and startup lines are older than all active content.
	for index := len(model.snapshot.Body.Transcript) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
		line := model.snapshot.Body.Transcript[index]
		if line.Kind == presentation.LineReasoning && !model.snapshot.ReasoningExpanded {
			lines = appendNewestWrapped(lines, reasoningCollapsedText, model.width, capacity)
			continue
		}
		if line.Kind == presentation.LineBranchSummary {
			lines = appendNewestWrapped(
				lines,
				renderBranchSummary(line, model.snapshot.BranchSummariesExpanded),
				model.width,
				capacity,
			)
			continue
		}
		lines = appendNewestWrapped(lines, renderLine(line), model.width, capacity)
	}
	for index := len(model.snapshot.Body.Startup) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
		lines = appendNewestWrapped(lines, renderLine(model.snapshot.Body.Startup[index]), model.width, capacity)
	}

	// Restore chronological display order after the newest-first traversal.
	slices.Reverse(lines)
	return lines
}

// hasBodyCapacity reports whether another visual line fits, including an unbounded viewport.
func hasBodyCapacity(lines []string, capacity int) bool {
	return capacity < 0 || len(lines) < capacity
}

// appendNewestWrapped retains the newest wrapped lines within the remaining viewport budget.
func appendNewestWrapped(lines []string, line string, width, capacity int) []string {
	wrapped := wrappedBodyLines(line, width)
	for index := len(wrapped) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
		lines = append(lines, wrapped[index])
	}
	return lines
}

// renderActiveModelLine distinguishes text, refusal, and locally collapsed reasoning blocks.
func renderActiveModelLine(content presentation.ActiveModelContent, reasoningExpanded bool) string {
	kind := presentation.LineModel
	contentKind, ok := content.Kind.Get()
	if !ok {
		contentKind = presentation.ModelContentUnspecified
	}
	switch contentKind {
	case presentation.ModelContentRefusal:
		kind = presentation.LineRefusal
	case presentation.ModelContentReasoning:
		kind = presentation.LineReasoning
	case presentation.ModelContentText, presentation.ModelContentUnspecified:
	}
	if kind == presentation.LineReasoning && !reasoningExpanded {
		return reasoningCollapsedText
	}
	return renderLine(presentation.Line{
		Kind:     kind,
		Text:     content.Text,
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentation.Content](),
	})
}

// wrappedBodyLines converts one logical body line into readable terminal-width visual lines.
func wrappedBodyLines(line string, width int) []string {
	if width <= 0 {
		return strings.Split(line, "\n")
	}

	return strings.Split(ansi.Wrap(line, width, ""), "\n")
}

// renderToolCall displays completed argument values and provisional string prefixes.
func renderToolCall(call presentation.ToolCallState) string {
	status := toolCallFinalText
	if call.Provisional {
		status = toolCallProvisionalText
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
	line := toolCallPrefix + call.Name + " (" + status + ")"
	if len(parts) > 0 {
		line += " " + strings.Join(parts, " ")
	}
	return line
}

// linePrefix assigns a stable prefix to non-tool presentation lines.
func linePrefix(kind presentation.LineKind) string {
	switch kind {
	case presentation.LineInformation, presentation.LineUnspecified:
		return informationLinePrefix
	case presentation.LineError:
		return errorLinePrefix
	case presentation.LineWarning:
		return warningLinePrefix
	case presentation.LineUser:
		return userLinePrefix
	case presentation.LineModel:
		return modelLinePrefix
	case presentation.LineRefusal:
		return refusalLinePrefix
	case presentation.LineReasoning:
		return reasoningLinePrefix
	case presentation.LineBranchSummary:
		return branchSummaryLinePrefix
	case presentation.LineToolStatus, presentation.LineToolStdout,
		presentation.LineToolStderr, presentation.LineToolDone,
		presentation.LineToolError:
		return toolLinePrefix(kind)
	default:
		return ""
	}
}

// toolLinePrefix assigns a stable prefix to tool presentation lines.
func toolLinePrefix(kind presentation.LineKind) string {
	switch kind {
	case presentation.LineToolStatus:
		return toolStatusLinePrefix
	case presentation.LineToolStdout:
		return toolStdoutLinePrefix
	case presentation.LineToolStderr:
		return toolStderrLinePrefix
	case presentation.LineToolDone:
		return toolDoneLinePrefix
	case presentation.LineToolError:
		return toolErrorLinePrefix
	case presentation.LineUnspecified, presentation.LineInformation,
		presentation.LineError, presentation.LineWarning, presentation.LineUser,
		presentation.LineModel, presentation.LineRefusal, presentation.LineReasoning,
		presentation.LineBranchSummary:
		return ""
	}
	return ""
}

// renderBranchSummary renders one local collapsed or expanded summary item.
func renderBranchSummary(line presentation.Line, expanded bool) string {
	if !expanded {
		return fmt.Sprintf(branchSummaryCollapsedFormat, branchSummaryLinePrefix, branchSummaryCollapsedText)
	}
	text, _ := line.Text.Get()
	return fmt.Sprintf(branchSummaryExpandedFormat, branchSummaryLinePrefix, branchSummaryExpandedTitle, text)
}

// renderLine assigns one stable terminal prefix to each presentation line kind.
func renderLine(line presentation.Line) string {
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
func selectionText(selectionOption mo.Option[presentation.ModelSelection]) string {
	selection, ok := selectionOption.Get()
	if !ok || selection.ProviderID == "" || selection.ModelID == "" {
		return modelUnavailableText
	}
	return fmt.Sprintf(
		modelSelectionFormat,
		selection.ProviderID,
		selection.ModelID,
		reasoningText(selection.ReasoningChoice),
	)
}

// reasoningText maps the closed reasoning set to its configured spelling.
func reasoningText(level presentation.ReasoningChoice) string {
	switch level {
	case presentation.ReasoningChoiceOff:
		return reasoningOffText
	case presentation.ReasoningChoiceOn:
		return reasoningOnText
	case presentation.ReasoningChoiceMinimal:
		return reasoningMinimalText
	case presentation.ReasoningChoiceLow:
		return reasoningLowText
	case presentation.ReasoningChoiceMedium:
		return reasoningMediumText
	case presentation.ReasoningChoiceHigh:
		return reasoningHighText
	case presentation.ReasoningChoiceXHigh:
		return reasoningXHighText
	case presentation.ReasoningChoiceMax:
		return reasoningMaxText
	case presentation.ReasoningChoiceUnspecified:
		return unspecifiedReasoningText
	default:
		return unspecifiedReasoningText
	}
}

// availabilityText maps Host availability to concise terminal status text.
func availabilityText(availabilityOption mo.Option[presentation.Availability]) string {
	availability, ok := availabilityOption.Get()
	if !ok {
		return unavailableStatusText
	}
	switch availability {
	case presentation.AvailabilityChecking:
		return checkingStatusText
	case presentation.AvailabilityAuthenticating:
		return authenticatingStatusText
	case presentation.AvailabilityAuthenticationFailed:
		return authenticationFailedStatusText
	case presentation.AvailabilityIdle:
		return idleStatusText
	case presentation.AvailabilityRunning:
		return runningStatusText
	case presentation.AvailabilityUnspecified:
		return unavailableStatusText
	default:
		return unavailableStatusText
	}
}
