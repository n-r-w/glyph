//go:build !integration

package runtime

import (
	"testing"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapInitializationPreservesWarningAndExtensionPath verifies public UI diagnostics mapping.
func TestMapInitializationPreservesWarningAndExtensionPath(t *testing.T) {
	t.Parallel()
	// Arrange the inline payload for mapInitialization to verify public UI diagnostics mapping.

	// Act by invoking mapInitialization to exercise public UI diagnostics mapping.
	mapped, err := mapInitialization(hostui.Initialization{
		SelectedUIID: "ui",
		StartupContent: []hostui.StartupContent{{
			Severity: hostui.ContentSeverityWarning,
			Text:     "excluded optional UI",
		}},
		Extensions: []hostui.ExtensionAvailability{{
			PluginID: "tools",
			Path:     "/plugins/tools",
			Tools:    []string{"read"},
		}},
		Availability: hostui.AvailabilityCheckingAuthentication,
		Models: []model.Descriptor{{
			Input:            nil,
			ContextWindow:    0,
			MaxTokens:        0,
			ToolCapabilities: model.ToolCapabilities{},
			Pricing:          mo.None[model.Pricing](),

			Provider:              "openrouter",
			Model:                 "sonnet",
			ReasoningCapabilities: testUIReasoningCapabilities(model.ReasoningChoiceOff, model.ReasoningChoiceXHigh),
		}},
		ModelSelection: mo.Some(model.Selection{
			Provider:        "openrouter",
			Model:           "sonnet",
			ReasoningChoice: model.ReasoningChoiceXHigh,
		}),
		SessionInfo: session.Info{},
	})

	// Assert public UI diagnostics mapping.
	require.NoError(t, err)
	require.Len(t, mapped.GetStartupContent(), 1)
	assert.Equal(t, uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING, mapped.GetStartupContent()[0].GetSeverity())
	require.Len(t, mapped.GetExtensions(), 1)
	assert.Equal(t, "/plugins/tools", mapped.GetExtensions()[0].GetPath())
	require.Len(t, mapped.GetModels(), 1)
	assert.Equal(t, "openrouter", mapped.GetModels()[0].GetProviderId())
	assert.Equal(t, []uiv1.ReasoningChoice{
		uiv1.ReasoningChoice_REASONING_CHOICE_OFF,
		uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH,
	}, mapped.GetModels()[0].GetReasoning().GetChoices())
	assert.Equal(t, uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH, mapped.GetModelSelection().GetReasoningChoice())
}
