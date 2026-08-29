package sessions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// ServiceSuite verifies session repository path behavior.
type ServiceSuite struct {
	// Suite provides assertions and test lifecycle support.
	suite.Suite
}

// TestServiceSuite runs session repository path tests.
func TestServiceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ServiceSuite))
}

// TestProjectDirectoryName verifies the readable project directory name.
func (s *ServiceSuite) TestProjectDirectoryName() {
	// Arrange a readable absolute working directory.
	workingDirectory := "/Users/alice/src/glyph"

	// Act by encoding the project session directory name.
	name := ProjectDirectoryName(workingDirectory)

	// Assert slashes become dashes and the name has boundary markers.
	s.Equal("--Users-alice-src-glyph--", name)
}

// TestSessionFilename verifies the timestamp and session identifier filename format.
func (s *ServiceSuite) TestSessionFilename() {
	// Arrange a session header with a non-UTC creation time.
	header := session.Header{
		Version: 2,
		ID:      "session-id",
		CreatedAt: time.Date(
			2026, time.August, 30, 12, 34, 56, 123456789, time.FixedZone("test", 2*60*60),
		),
		WorkingDirectory: "/Users/alice/src/glyph",
	}

	// Act by formatting the session filename.
	name := SessionFilename(header)

	// Assert the timestamp is UTC and the opaque session identifier remains visible.
	s.Equal("20260830T103456.123456789Z-session-id.jsonl", name)
}
