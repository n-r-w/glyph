package output

import (
	"context"
	"log/slog"

	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
)

// ReportRuntimeFailure records runtime diagnostics without sending a Programmatic connection event.
func (*Service) ReportRuntimeFailure(ctx context.Context, failure extension.RuntimeFailure) error {
	message, runtimeErr := failure.Message()
	slog.ErrorContext(ctx, message, "plugin_id", failure.PluginID, "error", runtimeErr)
	return nil
}

var _ extensionruntime.FailureReporter = (*Service)(nil)
