package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestCatalogExposesOneStartupSelectedModel verifies immutable provider-neutral inspection.
func TestCatalogExposesOneStartupSelectedModel(t *testing.T) {
	t.Parallel()

	descriptor := model.Descriptor{
		Provider: "openai-codex", Model: "gpt-5.6-luna",
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: true,
			Grammar:          model.GrammarCapabilities{Lark: true, Regex: true},
		},
	}
	catalog := New(descriptor, nil)

	assert.Equal(t, []model.Descriptor{descriptor}, catalog.Models())
}
