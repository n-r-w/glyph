//go:build !integration

package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

type ServiceSuite struct {
	suite.Suite
}

func TestServiceSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(ServiceSuite))
}

// emptyControllerResponse creates one fully initialized response for controller protocol tests.
func emptyControllerResponse(correlationID string, kind ResponseKind) Response {
	return Response{
		CorrelationID: correlationID, Kind: kind, State: mo.None[RunStateResult](), Messages: nil,
		Models: mo.None[ModelsResult](), Selection: mo.None[model.Selection](), SessionInfo: mo.None[session.Info](),
		Sessions: nil, SessionEntries: nil, SessionStatistics: mo.None[session.Statistics](),
		SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
		Rejection: mo.None[Rejection](), Replacement: mo.None[SessionReplacement](),
	}
}
