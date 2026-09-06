package app

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
)

// lifecycleIssueDeliveryFunc binds mode-specific client delivery at application assembly.
type lifecycleIssueDeliveryFunc func(context.Context, lifecycle.Issue) error

var _ lifecycle.IssueDelivery = lifecycleIssueDeliveryFunc(nil)

// DeliverExtensionIssue calls the bound client delivery function.
func (delivery lifecycleIssueDeliveryFunc) DeliverExtensionIssue(ctx context.Context, issue lifecycle.Issue) error {
	return delivery(ctx, issue)
}
