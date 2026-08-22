package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// TestBuildInitializationIncludesFailuresAvailabilityAndOneSummary verifies startup delivery content.
func TestBuildInitializationIncludesFailuresAvailabilityAndOneSummary(t *testing.T) {
	t.Parallel()

	initialization := BuildInitialization("selected", toolservice.LoadReport{
		Issues: []toolservice.Issue{{PluginIDs: []string{"broken"}, Path: "/broken", Err: errors.New("failed")}},
		Extensions: []toolservice.LoadedExtension{{
			ID: "tools", Path: "/plugins/tools",
			Tools: []tool.Descriptor{{
				Name: "read", Description: "read", InputSchemaJSON: []byte(`{}`),
				ConstrainedSampling: tool.ConstrainedSampling{
					Kind: 0, JSONSchemaStrictness: 0,
					Grammar: tool.GrammarVariants{Lark: "", Regex: ""}, GrammarInputProperty: "",
				},
			}},
		}},
	}, []SelectionIssue{{
		Candidate: domainui.Candidate{ID: "excluded", Path: "/excluded"}, Err: errors.New("incompatible"),
	}})

	assert.Equal(t, "selected", initialization.SelectedUIID)
	assert.Equal(t, domainui.AvailabilityCheckingAuthentication, initialization.Availability)
	require.Len(t, initialization.StartupContent, 3)
	assert.Equal(t, domainui.ContentSeverityError, initialization.StartupContent[0].Severity)
	assert.Contains(t, initialization.StartupContent[0].Text, "broken")
	assert.Contains(t, initialization.StartupContent[0].Text, "/broken")
	assert.Equal(t, domainui.ContentSeverityWarning, initialization.StartupContent[1].Severity)
	assert.Contains(t, initialization.StartupContent[1].Text, "excluded")
	assert.Contains(t, initialization.StartupContent[1].Text, "/excluded")
	assert.Equal(t, domainui.ContentSeverityInformation, initialization.StartupContent[2].Severity)
	assert.Contains(t, initialization.StartupContent[2].Text, "UI selected")
	assert.Contains(t, initialization.StartupContent[2].Text, "/plugins/tools")
	require.Len(t, initialization.Extensions, 1)
	assert.Equal(t, "tools", initialization.Extensions[0].PluginID)
	assert.Equal(t, "/plugins/tools", initialization.Extensions[0].Path)
	assert.Equal(t, []string{"read"}, initialization.Extensions[0].Tools)
}

// TestBuildInitializationTreatsEmptyExtensionsAsNormalInformation verifies empty catalogs are not errors.
func TestBuildInitializationTreatsEmptyExtensionsAsNormalInformation(t *testing.T) {
	t.Parallel()

	initialization := BuildInitialization("selected", toolservice.LoadReport{
		Issues: nil, Extensions: nil,
	}, nil)

	require.Len(t, initialization.StartupContent, 1)
	assert.Equal(t, domainui.ContentSeverityInformation, initialization.StartupContent[0].Severity)
	assert.Contains(t, initialization.StartupContent[0].Text, "extensions: none")
	assert.Empty(t, initialization.Extensions)
}
