package uiv1

import (
	"errors"
	"fmt"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// validateSessionSummary validates one session list item.
func validateSessionSummary(summary *uiv1.SessionSummary) error {
	if summary == nil || summary.GetInfo() == nil || !summary.HasTotalMessages() {
		return errors.New("Host session summary fields are required")
	}
	return validateSessionInfo(summary.GetInfo())
}

// validateSessionStatistics validates nested optional costs and repeated cost groups.
func validateSessionStatistics(statistics *uiv1.SessionStatistics) error {
	if statistics == nil {
		return errors.New("Host session statistics are required")
	}
	if cost := statistics.GetEstimatedCost(); cost != nil {
		if err := validateEstimatedCost(cost); err != nil {
			return err
		}
	}
	for index, group := range statistics.GetCostBreakdown() {
		if group == nil || !group.HasProviderId() || !group.HasModelId() {
			return fmt.Errorf("Host session cost group %d identity is required", index)
		}
		if cost := group.GetEstimatedCost(); cost != nil {
			if err := validateEstimatedCost(cost); err != nil {
				return fmt.Errorf("Host session cost group %d: %w", index, err)
			}
		}
	}
	return nil
}

// validateEstimatedCost requires every normalized cost bucket.
func validateEstimatedCost(cost *uiv1.EstimatedCost) error {
	if cost == nil || !cost.HasInput() || !cost.HasOutput() || !cost.HasCacheRead() ||
		!cost.HasCacheWrite() || !cost.HasTotal() {
		return errors.New("Host estimated cost fields are required")
	}
	return nil
}

// validateSessionEntry validates one restored transcript entry and its payload.
func validateSessionEntry(entry *uiv1.SessionEntry) error {
	if entry == nil || !entry.HasId() || entry.GetId() == "" || entry.GetCreatedTime() == nil {
		return errors.New("Host session entry fields are required")
	}
	if err := entry.GetCreatedTime().CheckValid(); err != nil {
		return fmt.Errorf("Host session entry time: %w", err)
	}
	switch entry.WhichEntry() {
	case uiv1.SessionEntry_User_case:
		return validateUserMessage(entry.GetUser())
	case uiv1.SessionEntry_Model_case:
		return validateModelResponse(entry.GetModel())
	case uiv1.SessionEntry_ToolResult_case:
		return validateToolResult(entry.GetToolResult())
	case uiv1.SessionEntry_BranchSummary_case:
		return validateBranchSummary(entry.GetBranchSummary())
	case uiv1.SessionEntry_Entry_not_set_case:
		return errors.New("Host session entry payload is required")
	default:
		return errors.New("Host session entry payload is unknown")
	}
}

// validateSessionTreeEntry validates one tree entry and its selected payload.
func validateSessionTreeEntry(entry *uiv1.SessionTreeEntry) error {
	if entry == nil || !entry.HasId() || entry.GetId() == "" || entry.GetCreatedTime() == nil {
		return errors.New("Host session tree entry fields are required")
	}
	if err := entry.GetCreatedTime().CheckValid(); err != nil {
		return fmt.Errorf("Host session tree entry time: %w", err)
	}
	switch entry.WhichEntry() {
	case uiv1.SessionTreeEntry_User_case:
		return validateUserMessage(entry.GetUser())
	case uiv1.SessionTreeEntry_Model_case:
		return validateModelResponse(entry.GetModel())
	case uiv1.SessionTreeEntry_ToolResult_case:
		return validateToolResult(entry.GetToolResult())
	case uiv1.SessionTreeEntry_Extension_case:
		extension := entry.GetExtension()
		if extension == nil || !extension.HasExtensionId() || !extension.HasEntryType() {
			return errors.New("Host extension tree entry fields are required")
		}
		return nil
	case uiv1.SessionTreeEntry_BranchSummary_case:
		return validateBranchSummary(entry.GetBranchSummary())
	case uiv1.SessionTreeEntry_Entry_not_set_case:
		return errors.New("Host session tree entry payload is required")
	default:
		return errors.New("Host session tree entry payload is unknown")
	}
}

// validateUserMessage validates every selected user content item.
func validateUserMessage(message *uiv1.UserMessage) error {
	if message == nil {
		return errors.New("Host user message is required")
	}
	for index, content := range message.GetContent() {
		if content == nil {
			return fmt.Errorf("Host user content %d is required", index)
		}
		switch content.WhichContent() {
		case uiv1.UserContent_Text_case:
			continue
		case uiv1.UserContent_Image_case:
			image := content.GetImage()
			if image == nil || !image.HasMediaType() || image.GetMediaType() == "" || !image.HasData() {
				return fmt.Errorf("Host user image %d fields are required", index)
			}
		case uiv1.UserContent_Content_not_set_case:
			return fmt.Errorf("Host user content %d payload is required", index)
		default:
			return fmt.Errorf("Host user content %d payload is unknown", index)
		}
	}
	return nil
}

// validateToolResult validates restored tool result content.
func validateToolResult(result *uiv1.ToolResult) error {
	if result == nil {
		return errors.New("Host tool result is required")
	}
	return validateToolResultContents(result.GetContents(), true)
}

// validateBranchSummary validates the text required by presentation mapping.
func validateBranchSummary(summary *uiv1.BranchSummary) error {
	if summary == nil || !summary.HasSummary() {
		return errors.New("Host branch summary text is required")
	}
	return nil
}
