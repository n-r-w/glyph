package app

import (
	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	extensiontransport "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// bindExtensionContexts constructs context ownership and runtime-specific dispatch before registration.
func bindExtensionContexts(
	factory *extensiontransport.Factory,
	runtimes *extensionruntime.Service,
	toolService *tools.Service,
	sessions sessionComposition,
) *extensioncontext.Service {
	contexts := extensioncontext.New(runtimes, sessions.active)
	toolService.BindContextIssuer(contexts)
	sessions.tree.BindContextIssuer(contexts)
	factory.BindHostServiceFactory(func(extensionID, runtimeID string) extensionsdk.HostService {
		return extensioncontroller.New(contexts, runtimes, extensionID, runtimeID)
	})
	return contexts
}
