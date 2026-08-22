package codex

import "github.com/n-r-w/glyph/host/internal/domain/model"

// ModelDescriptor resolves provider-owned capabilities for one configured model.
func ModelDescriptor(modelID model.ID) model.Descriptor {
	capabilities := model.ToolCapabilities{
		StrictJSONSchema: false, Grammar: model.GrammarCapabilities{Lark: false, Regex: false},
	}
	if modelID == "gpt-5.6-luna" {
		capabilities = model.ToolCapabilities{
			StrictJSONSchema: true,
			Grammar:          model.GrammarCapabilities{Lark: true, Regex: true},
		}
	}
	return model.Descriptor{Provider: ProviderID, Model: modelID, ToolCapabilities: capabilities}
}
