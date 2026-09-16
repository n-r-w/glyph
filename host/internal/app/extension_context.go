package app

import (
	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	extensiontransport "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
	"github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// bindExtensionContexts constructs context ownership before model-dependent operations are exposed.
func bindExtensionContexts(
	runtimes *extensionruntime.Service,
	toolService *tools.Service,
	sessions sessionComposition,
) *extensioncontext.Service {
	contexts := extensioncontext.New(runtimes, sessions.active)
	toolService.BindContextIssuer(contexts)
	sessions.tree.BindContextIssuer(contexts)
	return contexts
}

// bindExtensionHostFactory exposes runtime operations only after every model dependency is bound.
func bindExtensionHostFactory(
	factory *extensiontransport.Factory,
	runtimes *extensionruntime.Service,
	contexts *extensioncontext.Service,
	selection *modelselection.Service,
) {
	factory.BindHostServiceFactory(func(extensionID, runtimeID string) extensionsdk.HostService {
		controller := extensioncontroller.New(contexts, runtimes, extensionID, runtimeID)
		controller.BindSelection(selection)
		return controller
	})
}
