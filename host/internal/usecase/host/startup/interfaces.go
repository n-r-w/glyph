package startup

import (
	"context"

	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=startup

// Reporter receives isolated startup failures and the completed load report.
type Reporter interface {
	ReportIssue(ctx context.Context, issue toolservice.Issue) error
	ReportSummary(ctx context.Context, report toolservice.LoadReport) error
}
