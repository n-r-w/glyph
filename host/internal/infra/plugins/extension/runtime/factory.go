package runtime

import (
	"context"
	"fmt"
	"os/exec"

	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// Factory starts SDK-backed extension runtimes.
type Factory struct {
	// hostService constructs dispatch bound to one connected extension and process identity.
	hostService func(string, string) extensionsdk.HostService
}

var _ extensionruntime.RuntimeFactory = (*Factory)(nil)

// NewFactory creates an extension runtime factory.
func NewFactory() *Factory { return &Factory{hostService: nil} }

// BindHostServiceFactory installs runtime-specific dispatch construction before registration starts.
func (f *Factory) BindHostServiceFactory(hostService func(string, string) extensionsdk.HostService) {
	f.hostService = hostService
}

// Start launches one candidate with the Host working directory and environment.
func (f *Factory) Start(
	ctx context.Context,
	candidate extensionruntime.Candidate,
) (extensionruntime.ExtensionRuntime, error) {
	//nolint:gosec // The catalog contains trusted local extension executables.
	command := exec.CommandContext(context.WithoutCancel(ctx), candidate.Path)
	runtime, err := Start(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start extension %q: %w", candidate.ID, err)
	}
	if f.hostService != nil {
		runtime.connection.BindHostService(f.hostService(candidate.ID, candidate.InstanceID))
	}
	return runtime, nil
}
