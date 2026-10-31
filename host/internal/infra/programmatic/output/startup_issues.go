package output

import (
	"fmt"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// HoldStartupIssues keeps the extension-loading issues for the client that opens the stream.
func (s *Service) HoldStartupIssues(issues []startup.Issue) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.startupIssues = issues
}

// enqueueStartupIssues queues the held issues on a new writer, in report order; the caller holds the mutex.
func (s *Service) enqueueStartupIssues(writer *operation.Writer[*programmaticv1.OpenResponse]) {
	for _, issue := range s.startupIssues {
		// A refused enqueue keeps the issue text in the writer's undelivered sources, which end the session result.
		_ = writer.Enqueue(startupIssueResponse(issue), fmt.Errorf("deliver startup issue: %w", issue.Err))
	}
}

// startupIssueResponse maps one startup issue to its connection event.
func startupIssueResponse(issue startup.Issue) *programmaticv1.OpenResponse {
	connection := new(programmaticv1.HostConnectionEvent)
	connection.SetStartupIssue(programmaticv1.StartupIssue_builder{
		ExtensionIds: issue.PluginIDs, CatalogPath: new(issue.Path), Text: new(issue.Err.Error()),
	}.Build())
	response := new(programmaticv1.OpenResponse)
	response.SetConnectionEvent(connection)
	return response
}
