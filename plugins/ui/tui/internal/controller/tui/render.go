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
	"github.com/samber/lo"
	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
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
		model.state.Availability,
	) + statusSeparator + selectionText(
		model.state.ModelSelection,
	)
	if model.treeStatus != "" {
		status += statusSeparator + model.treeStatus
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
		requestLabel+string(model.input[:model.cursor])+"|"+string(model.input[model.cursor:]),
		fmt.Sprintf(terminalSizeFormat, model.width, model.height),
		selectionKeys+commonKeysText,
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
	if model.treeMode != treeInteractionClosed {
		return model.treeSelectorLines()
	}
	rowCount := len(model.state.Models)
	title := modelsSelectorTitle
	if model.sessionSelector {
		title = sessionsSelectorTitle
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
		prefix := inactiveSelectorPrefix
		if index == model.selectorRow {
			prefix = activeSelectorPrefix
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
				sessionRowFormat, label, summary.Info.UpdatedAt.Format(time.RFC3339), summary.TotalMessages,
			)
			lines = append(lines, prefix+ellipsize(row, max(1, model.width-len(prefix))))
			continue
		}
		configured := model.state.Models[index]
		lines = append(lines, prefix+configured.ProviderID+" / "+configured.ModelID)
	}
	if statusLineCount > 0 {
		lines = append(lines, ellipsize(sessionStatusLabel+model.resumeStatus, max(1, model.width)))
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

	estimatedLines := len(model.state.Startup) + len(model.state.Transcript) +
		len(model.state.ActiveModel) + len(model.state.ActiveToolCalls) + 1
	if capacity >= 0 && estimatedLines > capacity {
		estimatedLines = capacity
	}
	// Walk body sections from newest to oldest so rendering stops when the viewport is full.
	// appendNewestWrapped stores visual lines in reverse order to avoid repeated prepends.
	lines := make([]string, 0, estimatedLines)
	if authorizationURL, ok := model.state.AuthorizationURL.Get(); ok {
		lines = appendNewestWrapped(lines, authorizationLabel+authorizationURL, model.width, capacity)
	}

	if hasBodyCapacity(lines, capacity) {
		calls := lo.Values(model.state.ActiveToolCalls)
		slices.SortFunc(calls, func(left, right presentationdomain.ToolCallState) int {
			return cmp.Compare(left.Position, right.Position)
		})
		for index := len(calls) - 1; index >= 0 && hasBodyCapacity(lines, capacity); index-- {
			lines = appendNewestWrapped(lines, renderToolCall(calls[index]), model.width, capacity)
		}
	}

	if hasBodyCapacity(lines, capacity) {
		positions := lo.Keys(model.state.ActiveModel)
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
			lines = appendNewestWrapped(lines, reasoningCollapsedText, model.width, capacity)
			continue
		}
		if line.Kind == presentationdomain.LineBranchSummary {
			lines = appendNewestWrapped(
				lines,
				renderBranchSummary(line, model.branchSummariesExpanded),
				model.width,
				capacity,
			)
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
		return reasoningCollapsedText
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
func linePrefix(kind presentationdomain.LineKind) string {
	switch kind {
	case presentationdomain.LineInformation, presentationdomain.LineUnspecified:
		return informationLinePrefix
	case presentationdomain.LineError:
		return errorLinePrefix
	case presentationdomain.LineWarning:
		return warningLinePrefix
	case presentationdomain.LineUser:
		return userLinePrefix
	case presentationdomain.LineModel:
		return modelLinePrefix
	case presentationdomain.LineRefusal:
		return refusalLinePrefix
	case presentationdomain.LineReasoning:
		return reasoningLinePrefix
	case presentationdomain.LineBranchSummary:
		return branchSummaryLinePrefix
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
		return toolStatusLinePrefix
	case presentationdomain.LineToolStdout:
		return toolStdoutLinePrefix
	case presentationdomain.LineToolStderr:
		return toolStderrLinePrefix
	case presentationdomain.LineToolDone:
		return toolDoneLinePrefix
	case presentationdomain.LineToolError:
		return toolErrorLinePrefix
	case presentationdomain.LineUnspecified, presentationdomain.LineInformation,
		presentationdomain.LineError, presentationdomain.LineWarning, presentationdomain.LineUser,
		presentationdomain.LineModel, presentationdomain.LineRefusal, presentationdomain.LineReasoning,
		presentationdomain.LineBranchSummary:
		return ""
	}
	return ""
}

// renderBranchSummary renders one local collapsed or expanded summary item.
func renderBranchSummary(line presentationdomain.Line, expanded bool) string {
	if !expanded {
		return fmt.Sprintf(branchSummaryCollapsedFormat, branchSummaryLinePrefix, branchSummaryCollapsedText)
	}
	text, _ := line.Text.Get()
	return fmt.Sprintf(branchSummaryExpandedFormat, branchSummaryLinePrefix, branchSummaryExpandedTitle, text)
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
func reasoningText(level presentationdomain.ReasoningChoice) string {
	switch level {
	case presentationdomain.ReasoningChoiceOff:
		return reasoningOffText
	case presentationdomain.ReasoningChoiceOn:
		return reasoningOnText
	case presentationdomain.ReasoningChoiceMinimal:
		return reasoningMinimalText
	case presentationdomain.ReasoningChoiceLow:
		return reasoningLowText
	case presentationdomain.ReasoningChoiceMedium:
		return reasoningMediumText
	case presentationdomain.ReasoningChoiceHigh:
		return reasoningHighText
	case presentationdomain.ReasoningChoiceXHigh:
		return reasoningXHighText
	case presentationdomain.ReasoningChoiceMax:
		return reasoningMaxText
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
		return checkingStatusText
	case presentationdomain.AvailabilityAuthenticating:
		return authenticatingStatusText
	case presentationdomain.AvailabilityAuthenticationFailed:
		return authenticationFailedStatusText
	case presentationdomain.AvailabilityIdle:
		return idleStatusText
	case presentationdomain.AvailabilityRunning:
		return runningStatusText
	case presentationdomain.AvailabilityUnspecified:
		return unavailableStatusText
	default:
		return unavailableStatusText
	}
}
