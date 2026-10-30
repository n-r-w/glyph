package codex

import (
	"context"
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	openaifailure "github.com/n-r-w/glyph/host/internal/infra/providers/openai"
)

// completeWireDiagnostics retains cleanup and cancellation causes before redacting acquired authentication secrets.
func completeWireDiagnostics(
	ctx context.Context,
	response model.Response,
	requestErr error,
	closeErr error,
	diagnostics *openaifailure.WireDiagnostics,
) (model.Response, error) {
	if closeErr != nil {
		requestErr = errors.Join(requestErr, closeErr)
		response.Outcome = mo.Some(model.OutcomeFailed)
	}
	if requestErr != nil && ctx.Err() != nil {
		if !errors.Is(requestErr, ctx.Err()) {
			requestErr = errors.Join(ctx.Err(), requestErr)
		}
		response.Outcome = mo.Some(model.OutcomeAborted)
	}
	requestErr = diagnostics.RedactError(requestErr)
	if message, present := response.ErrorMessage.Get(); present {
		response.ErrorMessage = mo.Some(diagnostics.RedactText(message))
	}
	return response, requestErr
}
