//go:build integration

package app

import (
	"fmt"
	"strings"
)

// newCompatibleWireSettings selects a local compatible endpoint while retaining the required Codex catalog entry.
func newCompatibleWireSettings(api, baseURL, extra string) string {
	base := codexSettings(extra)
	local := strings.SplitN(base, "providers:\n", 2)[1]
	local = strings.Replace(local, "  openai-codex:", "  local:", 1)
	local = strings.Replace(local, "type: openai-codex",
		fmt.Sprintf("type: openai-compatible\n    api: %s\n    baseURL: %s", api, baseURL), 1)
	return strings.Replace(base, "defaultProvider: openai-codex", "defaultProvider: local", 1) + local
}
