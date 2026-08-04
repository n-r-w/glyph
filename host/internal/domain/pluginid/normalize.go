// Package pluginid owns normalized identities shared by Host plugin catalogs and settings.
package pluginid

import (
	"strings"
	"unicode"
)

// Normalize converts a catalog or configured plugin identifier into its comparison form.
func Normalize(value string) string {
	var result strings.Builder
	separatorPending := false
	for _, character := range strings.ToLower(value) {
		if unicode.IsSpace(character) || character == '_' || character == '-' {
			separatorPending = result.Len() > 0
			continue
		}
		if separatorPending {
			result.WriteByte('-')
			separatorPending = false
		}
		result.WriteRune(character)
	}
	return result.String()
}
