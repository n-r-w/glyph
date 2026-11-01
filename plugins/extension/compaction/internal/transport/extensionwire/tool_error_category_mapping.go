package extensionwire

import (
	"github.com/samber/mo"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// newToolErrorCategories pairs policy error categories with their public enum values.
func newToolErrorCategories() map[compaction.ToolErrorCategory]pb.ToolErrorCategory {
	return map[compaction.ToolErrorCategory]pb.ToolErrorCategory{
		compaction.ToolErrorUnavailableTool:  pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNAVAILABLE_TOOL,
		compaction.ToolErrorInvalidArguments: pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS,
		compaction.ToolErrorFailure:          pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE,
	}
}

// decodeToolErrorCategory maps the optional public category, including an unrecognized value.
func decodeToolErrorCategory(result *pb.SessionTreeToolResult) mo.Option[compaction.ToolErrorCategory] {
	if !result.HasErrorCategory() {
		return mo.None[compaction.ToolErrorCategory]()
	}
	for category, wire := range newToolErrorCategories() {
		if result.GetErrorCategory() == wire {
			return mo.Some(category)
		}
	}
	return mo.Some(compaction.ToolErrorInvalid)
}

// encodeToolErrorCategory maps the optional policy category to the public contract; an unrecognized category becomes
// the unspecified value, which Host rejects.
func encodeToolErrorCategory(category mo.Option[compaction.ToolErrorCategory]) *pb.ToolErrorCategory {
	value, present := category.Get()
	if !present {
		return nil
	}
	return new(newToolErrorCategories()[value])
}
