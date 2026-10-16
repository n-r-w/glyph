// Package app assembles the bundled compaction extension.
package app

import (
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/config"
	controller "github.com/n-r-w/glyph/plugins/extension/compaction/internal/controller/extension"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/infra/host"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/usecase/generation"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/usecase/policy"
	sdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// Serve runs the bundled compaction extension.
func Serve() error {
	settings, err := config.Load()
	if err != nil {
		return err
	}
	policyService := policy.New(&host.Service{}, settings.RetainedTokens)
	generator := generation.New(&host.Model{})
	sdk.Serve(controller.New(policyService, generator, settings.GeneratorEnabled))
	return nil
}
