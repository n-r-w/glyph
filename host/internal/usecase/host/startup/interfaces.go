package startup

import (
	"context"

	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=startup

// Reporter receives isolated startup failures and the completed load report.
type Reporter interface {
	ReportIssue(ctx context.Context, issue extensionservice.Issue) error
	ReportSummary(ctx context.Context, report extensionservice.LoadReport) error
}
