package startup

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=startup

// Reporter receives isolated startup failures and the completed load report.
type Reporter interface {
	ReportIssue(ctx context.Context, issue Issue) error
	ReportSummary(ctx context.Context, report LoadReport) error
}
