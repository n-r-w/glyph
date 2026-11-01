package extensionwire

import (
	"fmt"

	"github.com/samber/mo"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// decodeToolErrorCategory maps the optional public category; a category outside the closed set, including the
// unspecified value, fails with an error.
func decodeToolErrorCategory(result *pb.SessionTreeToolResult) (mo.Option[compaction.ToolErrorCategory], error) {
	if !result.HasErrorCategory() {
		return mo.None[compaction.ToolErrorCategory](), nil
	}
	wire := result.GetErrorCategory()
	switch wire {
	case pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNAVAILABLE_TOOL:
		return mo.Some(compaction.ToolErrorUnavailableTool), nil
	case pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS:
		return mo.Some(compaction.ToolErrorInvalidArguments), nil
	case pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE:
		return mo.Some(compaction.ToolErrorFailure), nil
	case pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_POLICY_REJECTION:
		return mo.Some(compaction.ToolErrorPolicyRejection), nil
	case pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNSPECIFIED:
	}
	return mo.None[compaction.ToolErrorCategory](), fmt.Errorf("tool error category %s is not supported", wire)
}

// encodeToolErrorCategory maps the optional tool error category to the public contract; a category outside the closed
// set fails with an error.
func encodeToolErrorCategory(category mo.Option[compaction.ToolErrorCategory]) (*pb.ToolErrorCategory, error) {
	value, present := category.Get()
	if !present {
		return nil, nil //nolint:nilnil // An absent category has no public value, and that is not a failure.
	}
	switch value {
	case compaction.ToolErrorUnavailableTool:
		return new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNAVAILABLE_TOOL), nil
	case compaction.ToolErrorInvalidArguments:
		return new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS), nil
	case compaction.ToolErrorFailure:
		return new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE), nil
	case compaction.ToolErrorPolicyRejection:
		return new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_POLICY_REJECTION), nil
	default:
		return nil, fmt.Errorf("tool error category %q is not supported", value)
	}
}
