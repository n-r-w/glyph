package programmatic

import (
	"context"
	"errors"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/internal/operation"
)

// runNavigation preserves actual navigation commits independently of the terminal failure state.
func (p *commandPrepared) runNavigation(
	ctx context.Context,
	reporter operation.Reporter[controller.OperationProgress],
) operation.Outcome[controller.Response] {
	response, err := p.service.navigateSessionTree(
		ctx,
		p.command,
		programmaticNavigationCallback(reporter),
		programmaticNavigationRetryCallback(reporter),
	)
	if err != nil {
		if navigation, present := response.TreeNavigation.Get(); present && navigation.Committed.IsSome() {
			if isOperationCancellation(ctx, err) {
				return operation.CompletedWithSource(response, err)
			}
			return operation.FailedWithResult(failureCode(err), err, response)
		}
		if isOperationCancellation(ctx, err) {
			return newCanceledCommandOutcome(err)
		}
		return operation.Failed[controller.Response](failureCode(err), err)
	}
	if rejection, present := response.Rejection.Get(); present {
		return operation.Failed[controller.Response](failureCodeForRejection(rejection.Code), rejection.Cause)
	}
	if isCanceledNavigation(response) && errors.Is(ctx.Err(), context.Canceled) {
		return operation.Canceled[controller.Response]()
	}
	return operation.Completed(response)
}

// isCanceledNavigation reports a domain-canceled navigation result without treating other completed results as
// cancellation.
func isCanceledNavigation(response controller.Response) bool {
	result, present := response.TreeNavigation.Get()
	return present && result.Status == controller.TreeNavigationStatusCanceled
}
