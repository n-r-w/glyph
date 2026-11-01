package modelexecution

import (
	"fmt"
	"slices"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// toolErrorCategoryFormat is the model-visible text that names the category of an error tool result.
const toolErrorCategoryFormat = "Glyph error category: %s"

// annotateErrorCategories returns history in which every error result with a category starts with a text block that
// names the category; the stored contents of the input stay unchanged.
func annotateErrorCategories(history []agent.HistoryEntry) []agent.HistoryEntry {
	annotated := slices.Clone(history)
	for index := range annotated {
		result, present := annotated[index].ToolResult.Get()
		category, categorized := result.ErrorCategory.Get()
		if !present || !categorized {
			continue
		}
		result.Contents = slices.Concat(
			tool.TextContents(fmt.Sprintf(toolErrorCategoryFormat, category.Name())), result.Contents,
		)
		annotated[index].ToolResult = mo.Some(result)
	}
	return annotated
}
