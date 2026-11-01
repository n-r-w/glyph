package extension

import (
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// EncodeToolErrorCategory maps the optional category of a tool result to its public optional enum value and rejects a
// category outside the closed set.
func EncodeToolErrorCategory(category mo.Option[agent.ToolErrorCategory]) (*extensionpb.ToolErrorCategory, error) {
	value, present := category.Get()
	if !present {
		return nil, nil //nolint:nilnil // An absent category has no public value and is not an error.
	}
	switch value {
	case agent.ToolErrorUnavailableTool:
		return new(extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNAVAILABLE_TOOL), nil
	case agent.ToolErrorInvalidArguments:
		return new(extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS), nil
	case agent.ToolErrorFailure:
		return new(extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE), nil
	case agent.ToolErrorPolicyRejection:
		return new(extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_POLICY_REJECTION), nil
	default:
		return nil, fmt.Errorf("tool error category %d is outside the closed set", value)
	}
}

// decodeToolErrorCategory maps the optional public enum value of a tool result and rejects a value outside the
// closed set.
func decodeToolErrorCategory(result *extensionpb.SessionTreeToolResult) (mo.Option[agent.ToolErrorCategory], error) {
	if !result.HasErrorCategory() {
		return mo.None[agent.ToolErrorCategory](), nil
	}
	switch result.GetErrorCategory() {
	case extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNAVAILABLE_TOOL:
		return mo.Some(agent.ToolErrorUnavailableTool), nil
	case extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS:
		return mo.Some(agent.ToolErrorInvalidArguments), nil
	case extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE:
		return mo.Some(agent.ToolErrorFailure), nil
	case extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_POLICY_REJECTION:
		return mo.Some(agent.ToolErrorPolicyRejection), nil
	case extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNSPECIFIED:
	}
	return mo.None[agent.ToolErrorCategory](), fmt.Errorf(
		"tool error category %d is outside the closed set", result.GetErrorCategory(),
	)
}
