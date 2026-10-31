package app

import (
	"fmt"

	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/providers"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// bindMiddleware supplies the same ordinary middleware owner to registration and logical execution and returns it
// for run input processing.
func bindMiddleware(
	registration *startup.Service,
	execution *modelexecution.Service,
	runtimes *extensionruntime.Service,
	contexts *extensioncontext.Service,
	models *providers.Catalog,
) (*middleware.Service, error) {
	owner := middleware.New(runtimes, contexts, models)
	if err := registration.BindMiddleware(owner); err != nil {
		return nil, fmt.Errorf("bind ordinary middleware registration: %w", err)
	}
	if err := execution.BindWireMiddleware(owner); err != nil {
		return nil, fmt.Errorf("bind ordinary wire execution: %w", err)
	}
	if err := execution.BindContextMiddleware(owner); err != nil {
		return nil, fmt.Errorf("bind ordinary context execution: %w", err)
	}
	return owner, nil
}
