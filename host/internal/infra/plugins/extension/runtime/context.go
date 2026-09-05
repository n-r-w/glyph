package runtime

import (
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// mapContext carries the issued binding without deriving identity from the request payload.
func mapContext(binding extension.Context) *extensionpb.ExtensionContext {
	return extensionpb.ExtensionContext_builder{
		ContextId: new(
			binding.ID,
		),
		ExtensionId:       new(binding.ExtensionID),
		RuntimeInstanceId: new(binding.RuntimeInstanceID),
		SessionId:         new(binding.SessionID),
		Cwd:               new(binding.WorkingDirectory),
	}.Build()
}
