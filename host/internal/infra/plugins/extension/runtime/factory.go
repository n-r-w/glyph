package runtime

import (
	"context"
	"fmt"
	"os/exec"

	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// Factory starts SDK-backed extension runtimes.
type Factory struct{}

var _ toolservice.RuntimeFactory = (*Factory)(nil)

// NewFactory creates an extension runtime factory.
func NewFactory() *Factory { return &Factory{} }

// Start launches one candidate with the Host working directory and environment.
func (f *Factory) Start(ctx context.Context, candidate toolservice.Candidate) (toolservice.ExtensionRuntime, error) {
	//nolint:gosec // The catalog contains trusted local extension executables.
	command := exec.CommandContext(context.WithoutCancel(ctx), candidate.Path)
	runtime, err := Start(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start extension %q: %w", candidate.ID, err)
	}
	return runtime, nil
}
