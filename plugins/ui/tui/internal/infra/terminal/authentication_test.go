//go:build !integration

package terminal

import (
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestAuthenticationSelectorRendersBothChoices checks the modal independently of configured models.
func TestAuthenticationSelectorRendersBothChoices(t *testing.T) {
	t.Parallel()
	// Arrange a sign-in selector with no configured model rows.
	model := newRenderModel()
	model.snapshot.SelectorOpen = true
	model.snapshot.AuthenticationSelector = true
	// Act by rendering each selected row.
	browser := model.visibleSelectorLines()
	model.snapshot.SelectorRow = 1
	device := model.visibleSelectorLines()
	// Assert the two choices and their focus marker remain visible without model-selector data.
	require.Len(t, browser, len(presentation.AuthenticationMethods())+2)
	require.Len(t, device, len(browser))
	assert.True(t, strings.HasPrefix(browser[1], activeSelectorPrefix))
	assert.True(t, strings.HasPrefix(browser[2], inactiveSelectorPrefix))
	assert.True(t, strings.HasPrefix(device[1], inactiveSelectorPrefix))
	assert.True(t, strings.HasPrefix(device[2], activeSelectorPrefix))
}

// TestDeviceAuthorizationShowsCodeAndClickableURL checks the values required to finish remote sign-in.
func TestDeviceAuthorizationShowsCodeAndClickableURL(t *testing.T) {
	t.Parallel()
	// Arrange a provider challenge without browser launch capability.
	const target = "https://example.test/device"
	model := newRenderModel()
	model.width, model.height = 80, 24
	model.snapshot.Body.AuthorizationURL = mo.Some(target)
	model.snapshot.Body.AuthorizationCode = mo.Some("ABCD-EFGH")
	// Act through the standard terminal body renderer.
	lines := model.visibleBodyLines(0)
	// Assert the code is visible and the URL has the complete browser target.
	assert.Contains(t, strings.Join(lines, "\n"), "ABCD-EFGH")
	var targets []string
	for _, line := range lines {
		_, links := hyperlinkCells(t, line)
		targets = append(targets, links...)
	}
	assert.Contains(t, targets, target)
}
