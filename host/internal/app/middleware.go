package app

import (
	"fmt"

	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// bindWireMiddleware supplies the same ordinary wire owner to registration and logical execution.
func bindWireMiddleware(
	registration *startup.Service,
	execution *modelexecution.Service,
	runtimes *extensionruntime.Service,
	contexts *extensioncontext.Service,
) error {
	owner := middleware.New(runtimes, contexts)
	if err := registration.BindMiddleware(owner); err != nil {
		return fmt.Errorf("bind ordinary middleware registration: %w", err)
	}
	if err := execution.BindWireMiddleware(owner); err != nil {
		return fmt.Errorf("bind ordinary wire execution: %w", err)
	}
	return nil
}
