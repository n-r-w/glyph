package host

import (
	"github.com/samber/mo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// commandFixture creates one complete presentation command.
func commandFixture(kind presentationdomain.CommandKind, text mo.Option[string]) presentationdomain.Command {
	return presentationdomain.Command{
		Kind: kind, Text: text, ProviderID: mo.None[string](), ModelID: mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](), SessionID: mo.None[string](),
		SessionName: mo.None[string](), TreeCommand: mo.None[presentationdomain.TreeCommand](),
	}
}
